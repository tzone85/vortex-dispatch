package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/preflight"
	"github.com/tzone85/vortex-dispatch/internal/state"
	"github.com/tzone85/vortex-dispatch/internal/tmux"
)

// --- F4: vxd doctor — automated pipeline diagnostics -------------------------
//
// CLAUDE.md carries a human "Debugging Checklist"; operators still diagnose
// by hand. `vxd doctor` runs the mechanical version: seven pure-ish checks,
// each returning findings with a fix hint. Every host interaction goes
// through doctorDeps func fields so each check is unit-testable with fakes.
// Exit code is 1 when any CRITICAL finding is reported.

// Severity levels for doctor findings.
const (
	sevCritical = "critical"
	sevWarning  = "warning"
	sevInfo     = "info"
)

// finding is one doctor diagnostic result.
type finding struct {
	Check    string `json:"check"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

// doctorReport is the --json envelope.
type doctorReport struct {
	Findings []finding `json:"findings"`
	Critical int       `json:"critical"`
	Warning  int       `json:"warning"`
}

// doctorDeps bundles every external seam the checks touch. Build via
// defaultDoctorDeps (production) or a test constructor (fakes).
type doctorDeps struct {
	Cfg            config.Config
	RepoDir        string
	StateDir       string
	LockPaths      []string
	WorktreeBase   string // absolute; worktrees outside this prefix are ignored
	Stories        []state.Story
	LastEvent      map[string]time.Time // storyID -> newest event timestamp
	StuckThreshold time.Duration

	ExecutablePath func() string                          // "" lets preflight resolve os.Executable
	ListWorktrees  func(repoDir string) ([]string, error) // git worktree list
	ListTmux       func() ([]string, error)               // tmux list-sessions
	ProcessAlive   func(pid int) bool                     // shared lockfile liveness probe
	RefExists      func(repoDir, ref string) bool         // git rev-parse --verify
	GitStatus      func(repoDir string) (string, error)   // git status --porcelain
	Now            func() time.Time
}

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run automated pipeline diagnostics",
		Long:  "Runs the mechanical version of the debugging checklist: binary PATH shadowing, model-ID validity, stuck stories, stale lock files, orphaned worktrees/tmux sessions, merge-base sanity, and a dirty base checkout.\nExits 1 when any CRITICAL finding is reported. Use --json for machine-readable output.",
		RunE:  runDoctor,
	}
	cmd.Flags().String("req", "", "Scope the stuck-story check to a requirement ID")
	cmd.Flags().Bool("json", false, "Output findings as JSON")
	cmd.SilenceUsage = true
	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	reqFilter, _ := cmd.Flags().GetString("req")
	jsonOut, _ := cmd.Flags().GetBool("json")

	s, err := loadStores(cmd)
	if err != nil {
		return err
	}
	defer s.Close()

	deps, err := defaultDoctorDeps(s, reqFilter)
	if err != nil {
		return err
	}

	findings := collectFindings(deps)

	out := cmd.OutOrStdout()
	if jsonOut {
		data, merr := renderDoctorJSON(findings)
		if merr != nil {
			return merr
		}
		fmt.Fprintln(out, string(data))
	} else {
		printDoctorReport(out, findings)
	}

	if eerr := doctorExitErrorIfCritical(findings); eerr != nil {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return eerr
	}
	return nil
}

// defaultDoctorDeps wires the production seams from loaded stores.
func defaultDoctorDeps(s stores, reqFilter string) (*doctorDeps, error) {
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	cfg := s.Config
	stateDir := expandHome(cfg.Workspace.StateDir)

	filter := state.StoryFilter{}
	if reqFilter != "" {
		filter.ReqID = reqFilter
	}
	stories, err := s.Proj.ListStories(filter)
	if err != nil {
		return nil, fmt.Errorf("list stories: %w", err)
	}

	// One pass over the event log builds the newest-event-per-story map;
	// per-story List calls would rescan the whole JSONL file per story.
	lastEvent := make(map[string]time.Time, len(stories))
	if allEvents, listErr := s.Events.List(state.EventFilter{}); listErr == nil {
		for _, evt := range allEvents {
			if evt.StoryID == "" {
				continue
			}
			if t, ok := lastEvent[evt.StoryID]; !ok || evt.Timestamp.After(t) {
				lastEvent[evt.StoryID] = evt.Timestamp
			}
		}
	}

	threshold := time.Duration(cfg.Monitor.StuckThresholdS) * time.Second
	if threshold <= 0 {
		threshold = 600 * time.Second // mirrors the monitor default
	}

	return &doctorDeps{
		Cfg:            cfg,
		RepoDir:        repoDir,
		StateDir:       stateDir,
		LockPaths:      []string{filepath.Join(stateDir, "vxd.lock")},
		WorktreeBase:   filepath.Join(stateDir, "worktrees"),
		Stories:        stories,
		LastEvent:      lastEvent,
		StuckThreshold: threshold,
		ExecutablePath: func() string { return "" },
		ListWorktrees:  vxdgit.ListWorktrees,
		ListTmux:       func() ([]string, error) { return tmux.ListSessions() },
		ProcessAlive:   engine.ProcessAlive,
		RefExists:      gitRefExists,
		GitStatus:      gitStatusPorcelain,
		Now:            time.Now,
	}, nil
}

// Compile-time guards: the production seams must keep matching the shapes
// the checks rely on (git.ListWorktrees signature, tmux session listing).
var (
	_ func(string) ([]string, error) = vxdgit.ListWorktrees
	_ func() ([]string, error)       = tmux.ListSessions
)

// collectFindings runs all seven checks in a stable order.
func collectFindings(d *doctorDeps) []finding {
	var out []finding
	out = append(out, doctorCheckBinaryPath(d)...)
	out = append(out, doctorCheckModels(d.Cfg)...)
	out = append(out, doctorCheckStuckStories(d)...)
	out = append(out, doctorCheckLocks(d)...)
	out = append(out, doctorCheckOrphans(d)...)
	out = append(out, doctorCheckMergeBase(d)...)
	out = append(out, doctorCheckDirtyRepo(d)...)
	return out
}

// --- check 1: binary PATH shadowing ------------------------------------------

// doctorCheckBinaryPath reuses the preflight CheckBinaryPath logic verbatim:
// running from outside ~/.local/bin means PATH order is wrong or a stale
// build shadows the canonical one ("new features appear absent").
func doctorCheckBinaryPath(d *doctorDeps) []finding {
	res := preflight.CheckBinaryPath(d.ExecutablePath())
	if res.Passed {
		return []finding{{Check: "binary_path", Severity: sevInfo, Message: res.Message}}
	}
	return []finding{{
		Check:    "binary_path",
		Severity: sevWarning,
		Message:  res.Message,
		Hint:     "rebuild with: go build -o ~/.local/bin/vxd ./cmd/vxd",
	}}
}

// --- check 2: model IDs -------------------------------------------------------

// doctorCheckModels validates every configured role binding against the
// static known-good alias list and flags dated snapshot IDs. No API calls:
// a bad model ID historically surfaced downstream as "agent produced no code
// changes" instead of a model error, so catching it statically is the point.
func doctorCheckModels(cfg config.Config) []finding {
	bindings := []struct {
		role string
		mc   config.ModelConfig
	}{
		{"tech_lead", cfg.Models.TechLead},
		{"senior", cfg.Models.Senior},
		{"intermediate", cfg.Models.Intermediate},
		{"junior", cfg.Models.Junior},
		{"qa", cfg.Models.QA},
		{"supervisor", cfg.Models.Supervisor},
		{"manager", cfg.Models.Manager},
		{"reviewer", cfg.Models.Reviewer},
	}

	var out []finding
	configured := 0
	for _, b := range bindings {
		if b.mc.Provider == "" && b.mc.Model == "" {
			continue // unset — falls back (e.g. reviewer -> senior)
		}
		configured++
		switch {
		case b.mc.Model == "":
			out = append(out, finding{
				Check: "model_ids", Severity: sevWarning,
				Message: fmt.Sprintf("models.%s has provider %q but an empty model", b.role, b.mc.Provider),
				Hint:    "set models." + b.role + ".model to an undated alias (see CLAUDE.md: Model ID Compatibility)",
			})
		case llm.LooksLikeDatedSnapshot(b.mc.Model):
			out = append(out, finding{
				Check: "model_ids", Severity: sevWarning,
				Message: fmt.Sprintf("models.%s uses dated snapshot %q — dated snapshots retire and then return HTTP 404", b.role, b.mc.Model),
				Hint:    "switch to the bare alias (e.g. claude-sonnet-4-6), which the subscription resolves to the current snapshot",
			})
		case !llm.IsKnownModelAlias(b.mc.Model):
			out = append(out, finding{
				Check: "model_ids", Severity: sevWarning,
				Message: fmt.Sprintf("models.%s uses %q which is not in the known-good alias list", b.role, b.mc.Model),
				Hint:    fmt.Sprintf("verify it resolves: claude --model %s -p OK (a bad ID surfaces as 'agent produced no code changes')", b.mc.Model),
			})
		}
	}
	if len(out) == 0 {
		msg := "all model bindings are known-good undated aliases"
		if configured == 0 {
			msg = "no model bindings configured (defaults apply)"
		}
		out = append(out, finding{Check: "model_ids", Severity: sevInfo, Message: msg})
	}
	return out
}

// --- check 3: stuck stories ---------------------------------------------------

// doctorCheckStuckStories flags in_progress stories whose newest event is
// older than the stuck threshold, reporting per-story age. Falls back to
// CreatedAt when a story has no events yet. CRITICAL: the pipeline cannot
// advance past a wedged story without operator intervention.
func doctorCheckStuckStories(d *doctorDeps) []finding {
	now := d.Now()
	var out []finding
	for _, st := range d.Stories {
		if st.Status != "in_progress" {
			continue
		}
		last, ok := d.LastEvent[st.ID]
		if !ok {
			last = st.CreatedAt
		}
		age := now.Sub(last)
		if d.StuckThreshold <= 0 || age <= d.StuckThreshold {
			continue
		}
		out = append(out, finding{
			Check:    "stuck_stories",
			Severity: sevCritical,
			Message: fmt.Sprintf("story %s (%s) is in_progress with no events for %s (threshold %s)",
				st.ID, truncate(st.Title, 48), age.Round(time.Second), d.StuckThreshold),
			Hint: fmt.Sprintf("inspect tmux session vxd-%s; recover with: vxd resume %s --force", st.ID, st.ReqID),
		})
	}
	if len(out) == 0 {
		out = append(out, finding{Check: "stuck_stories", Severity: sevInfo, Message: "no stuck in-progress stories"})
	}
	return out
}

// --- check 4: SLA/lock hygiene -------------------------------------------------

// doctorCheckLocks flags lock files whose owning PID is dead — the exact
// staleness signal engine.AcquireLock uses, via the shared liveness probe.
func doctorCheckLocks(d *doctorDeps) []finding {
	var out []finding
	for _, p := range d.LockPaths {
		info, err := engine.ReadLock(p)
		if err != nil {
			continue // absent or unreadable — nothing to report
		}
		if d.ProcessAlive(info.PID) {
			continue // live owner — healthy
		}
		out = append(out, finding{
			Check:    "locks",
			Severity: sevWarning,
			Message:  fmt.Sprintf("stale lock file %s (PID %d is dead, req %s since %s)", p, info.PID, info.ReqID, info.StartedAt),
			Hint:     fmt.Sprintf("remove it: rm %s  (or run: vxd resume --force)", p),
		})
	}
	if len(out) == 0 {
		out = append(out, finding{Check: "locks", Severity: sevInfo, Message: "no stale lock files"})
	}
	return out
}

// --- check 5: orphaned worktrees + tmux sessions --------------------------------

// doctorActiveStoryIDs returns the set of story IDs that legitimately own a
// worktree/session right now (anything not terminal and not pre-dispatch).
func doctorActiveStoryIDs(stories []state.Story) map[string]bool {
	active := make(map[string]bool, len(stories))
	for _, st := range stories {
		switch st.Status {
		case "merged", "archived", "draft", "estimated", "split":
			continue
		default:
			active[st.ID] = true
		}
	}
	return active
}

// doctorCheckOrphans finds git worktrees under the state worktree base and
// vxd-* tmux sessions that no active story claims.
func doctorCheckOrphans(d *doctorDeps) []finding {
	active := doctorActiveStoryIDs(d.Stories)
	var out []finding

	if trees, err := d.ListWorktrees(d.RepoDir); err == nil {
		for _, raw := range trees {
			w := filepath.Clean(raw)
			if d.WorktreeBase == "" || !strings.HasPrefix(w, d.WorktreeBase+string(os.PathSeparator)) {
				continue // main checkout or unrelated worktree
			}
			id := filepath.Base(w)
			if active[id] {
				continue
			}
			out = append(out, finding{
				Check:    "orphans",
				Severity: sevWarning,
				Message:  fmt.Sprintf("orphaned worktree %s (story %s is not active)", w, id),
				Hint:     fmt.Sprintf("git worktree remove --force %s  (or: vxd gc)", w),
			})
		}
	}

	if sessions, err := d.ListTmux(); err == nil {
		for _, sname := range sessions {
			if !strings.HasPrefix(sname, "vxd-") {
				continue
			}
			sid := strings.TrimPrefix(strings.TrimPrefix(sname, "vxd-"), "orphan-")
			if active[sid] {
				continue
			}
			out = append(out, finding{
				Check:    "orphans",
				Severity: sevWarning,
				Message:  fmt.Sprintf("orphaned tmux session %s (story %s is not active)", sname, sid),
				Hint:     fmt.Sprintf("tmux kill-session -t %s", sname),
			})
		}
	}

	if len(out) == 0 {
		out = append(out, finding{Check: "orphans", Severity: sevInfo, Message: "no orphaned worktrees or vxd-* tmux sessions"})
	}
	return out
}

// --- check 6: merge-base sanity --------------------------------------------------

// doctorCheckMergeBase detects which base ref the repo actually resolves,
// mirroring gitDiff()'s candidate order (origin/main, origin/master, main,
// master) so review diffs come from the same branch the operator expects.
func doctorCheckMergeBase(d *doctorDeps) []finding {
	candidates := []string{"origin/main", "origin/master", "main", "master"}
	detected := ""
	for _, c := range candidates {
		if d.RefExists(d.RepoDir, c) {
			detected = c
			break
		}
	}
	if detected == "" {
		return []finding{{
			Check:    "merge_base",
			Severity: sevWarning,
			Message:  fmt.Sprintf("no main/master ref found in %s — diff review falls back to the root commit", d.RepoDir),
			Hint:     "create the base branch, or point merge.base_branch at the branch you actually use",
		}}
	}

	base := strings.TrimSpace(d.Cfg.Merge.BaseBranch)
	normalized := strings.TrimPrefix(detected, "origin/")
	if base != "" && base != normalized {
		return []finding{{
			Check:    "merge_base",
			Severity: sevWarning,
			Message:  fmt.Sprintf("merge.base_branch=%q but the first existing ref is %q — reviews may diff against the wrong branch", base, detected),
			Hint:     fmt.Sprintf("set merge.base_branch: %s in vxd.yaml (mirrors the gitDiff candidate order)", normalized),
		}}
	}
	return []finding{{Check: "merge_base", Severity: sevInfo, Message: "merge-base target: " + detected}}
}

// --- check 7: dirty base checkout -------------------------------------------------

// doctorCheckDirtyRepo flags uncommitted changes in the repo root, which
// would block pullMainAfterMerge's fast-forward after merges complete.
func doctorCheckDirtyRepo(d *doctorDeps) []finding {
	status, err := d.GitStatus(d.RepoDir)
	if err != nil {
		return []finding{{Check: "dirty_repo", Severity: sevInfo, Message: "could not read git status: " + err.Error()}}
	}
	var lines []string
	for _, ln := range strings.Split(status, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) == 0 {
		return []finding{{Check: "dirty_repo", Severity: sevInfo, Message: "base checkout is clean"}}
	}
	preview := lines
	if len(preview) > 3 {
		preview = preview[:3]
	}
	return []finding{{
		Check:    "dirty_repo",
		Severity: sevWarning,
		Message:  fmt.Sprintf("repo root has %d uncommitted change(s): %s", len(lines), strings.Join(preview, "; ")),
		Hint:     "commit or stash — pullMainAfterMerge needs a clean tree to fast-forward after merges",
	}}
}

// --- output / exit code ------------------------------------------------------------

// renderDoctorJSON marshals the report envelope with severity tallies.
func renderDoctorJSON(findings []finding) ([]byte, error) {
	rep := doctorReport{Findings: findings}
	for _, f := range findings {
		switch f.Severity {
		case sevCritical:
			rep.Critical++
		case sevWarning:
			rep.Warning++
		}
	}
	return json.MarshalIndent(rep, "", "  ")
}

// doctorExitError is returned when any CRITICAL finding exists, so the
// process exit code is 1 for CI/script consumption.
type doctorExitError struct {
	Critical int
}

func (e doctorExitError) Error() string {
	return fmt.Sprintf("vxd doctor: %d CRITICAL finding(s) — see output above", e.Critical)
}

func doctorExitErrorIfCritical(findings []finding) error {
	n := 0
	for _, f := range findings {
		if f.Severity == sevCritical {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return doctorExitError{Critical: n}
}

// printDoctorReport renders the human-readable report.
func printDoctorReport(out io.Writer, findings []finding) {
	fmt.Fprintln(out, "VXD Doctor — pipeline diagnostics")
	fmt.Fprintln(out, strings.Repeat("-", 46))
	crit, warn := 0, 0
	for _, f := range findings {
		sym := "·"
		switch f.Severity {
		case sevCritical:
			sym = "✗"
			crit++
		case sevWarning:
			sym = "⚠"
			warn++
		}
		fmt.Fprintf(out, "%s [%s] %s: %s\n", sym, strings.ToUpper(f.Severity), f.Check, f.Message)
		if f.Hint != "" {
			fmt.Fprintf(out, "    hint: %s\n", f.Hint)
		}
	}
	fmt.Fprintf(out, "\nSummary: %d critical, %d warning\n", crit, warn)
}

// --- production git seams ------------------------------------------------------------

func gitRefExists(repoDir, ref string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

func gitStatusPorcelain(repoDir string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	return string(out), err
}
