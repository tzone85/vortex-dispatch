package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// mkDoctorDeps returns a deps value with deterministic fakes; individual
// tests override the seams they exercise.
func mkDoctorDeps(t *testing.T) *doctorDeps {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Merge.BaseBranch = "main"
	cfg.Monitor.StuckThresholdS = 600
	return &doctorDeps{
		Cfg:            cfg,
		RepoDir:        dir,
		StateDir:       dir,
		LockPaths:      nil,
		WorktreeBase:   filepath.Join(dir, "worktrees"),
		Stories:        nil,
		LastEvent:      map[string]time.Time{},
		StuckThreshold: 600 * time.Second,
		ExecutablePath: func() string { return filepath.Join(dir, ".local", "bin", "vxd") },
		ListWorktrees:  func(string) ([]string, error) { return nil, nil },
		ListTmux:       func() ([]string, error) { return nil, nil },
		ProcessAlive:   func(int) bool { return true },
		RefExists:      func(string, string) bool { return true },
		GitStatus:      func(string) (string, error) { return "", nil },
		Now:            func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) },
	}
}

// --- check: model IDs ----------------------------------------------------------------

func TestDoctorCheckModels_AllKnown_NoWarning(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Reviewer = config.ModelConfig{Provider: "codex", Model: "gpt-5.5"}
	for _, f := range doctorCheckModels(cfg) {
		if f.Severity == sevWarning || f.Severity == sevCritical {
			t.Errorf("unexpected problem finding for known-good config: %+v", f)
		}
	}
}

func TestDoctorCheckModels_UnknownAndDated_Flagged(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Junior = config.ModelConfig{Provider: "anthropic", Model: "claude-opus-4-20250514"} // dated
	cfg.Models.Manager = config.ModelConfig{Provider: "anthropic", Model: "claude-typo-9x"}       // unknown

	var dated, unknown bool
	for _, f := range doctorCheckModels(cfg) {
		if f.Severity != sevWarning {
			continue
		}
		if strings.Contains(f.Message, "dated snapshot") && strings.Contains(f.Message, "junior") {
			dated = true
		}
		if strings.Contains(f.Message, "not in the known-good alias list") && strings.Contains(f.Message, "manager") {
			unknown = true
		}
	}
	if !dated {
		t.Error("expected a dated-snapshot warning for models.junior")
	}
	if !unknown {
		t.Error("expected an unknown-alias warning for models.manager")
	}
}

func TestDoctorCheckModels_UnsetReviewerSkipped(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Models.Reviewer = config.ModelConfig{} // falls back to senior — must not warn
	for _, f := range doctorCheckModels(cfg) {
		if strings.Contains(f.Message, "reviewer") && f.Severity == sevWarning {
			t.Errorf("unset reviewer should be skipped, got: %+v", f)
		}
	}
}

// --- check: binary path ----------------------------------------------------------------

func TestDoctorCheckBinaryPath_ShadowedAndClean(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	shadowed := mkDoctorDeps(t)
	shadowed.ExecutablePath = func() string { return filepath.Join(dir, "go", "bin", "vxd") }
	fs := doctorCheckBinaryPath(shadowed)
	if len(fs) != 1 || fs[0].Severity != sevWarning {
		t.Fatalf("expected one warning for shadowed binary, got %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "stale build") {
		t.Errorf("expected 'stale build' in message: %s", fs[0].Message)
	}
	if !strings.Contains(fs[0].Hint, "go build -o ~/.local/bin/vxd") {
		t.Errorf("expected rebuild hint, got: %s", fs[0].Hint)
	}

	clean := mkDoctorDeps(t)
	clean.ExecutablePath = func() string { return filepath.Join(dir, ".local", "bin", "vxd") }
	fs = doctorCheckBinaryPath(clean)
	if len(fs) != 1 || fs[0].Severity != sevInfo {
		t.Fatalf("expected single info finding for canonical path, got %+v", fs)
	}
}

// --- check: stuck stories ----------------------------------------------------------------

func TestDoctorCheckStuckStories_AgeThresholdAndFresh(t *testing.T) {
	d := mkDoctorDeps(t)
	base := d.Now()
	d.Stories = []state.Story{
		{ID: "s-stale", ReqID: "r1", Title: "Stale story", Status: "in_progress", CreatedAt: base.Add(-3 * time.Hour)},
		{ID: "s-fresh", ReqID: "r1", Title: "Fresh story", Status: "in_progress", CreatedAt: base.Add(-3 * time.Hour)},
		{ID: "s-noevt", ReqID: "r1", Title: "No events", Status: "in_progress", CreatedAt: base.Add(-3 * time.Hour)},
		{ID: "s-done", ReqID: "r1", Title: "Merged", Status: "merged", CreatedAt: base.Add(-3 * time.Hour)},
	}
	d.LastEvent = map[string]time.Time{
		"s-stale": base.Add(-2 * time.Hour), // way past the 10m threshold
		"s-fresh": base.Add(-1 * time.Minute),
	}

	fs := doctorCheckStuckStories(d)
	var staleFound, noEvtFound bool
	for _, f := range fs {
		if f.Severity != sevCritical {
			continue
		}
		if strings.Contains(f.Message, "s-stale") {
			staleFound = true
			if !strings.Contains(f.Message, "2h") {
				t.Errorf("per-story age missing from message: %s", f.Message)
			}
		}
		if strings.Contains(f.Message, "s-noevt") {
			noEvtFound = true // CreatedAt fallback
		}
		if strings.Contains(f.Message, "s-fresh") || strings.Contains(f.Message, "s-done") {
			t.Errorf("fresh/terminal story wrongly flagged: %s", f.Message)
		}
	}
	if !staleFound {
		t.Error("expected s-stale flagged critical")
	}
	if !noEvtFound {
		t.Error("expected s-noevt flagged via CreatedAt fallback")
	}
}

// --- check: locks ---------------------------------------------------------------------------

func TestDoctorCheckLocks_StaleDeadPID(t *testing.T) {
	dir := t.TempDir()
	me := os.Getpid()
	dead := 1 << 30 // far above any real PID on supported platforms

	writeLock := func(name string, pid int) string {
		p := filepath.Join(dir, name)
		body := fmt.Sprintf(`{"pid":%d,"req_id":"r-9","started_at":"2026-06-30T10:00:00Z"}`, pid)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write lock: %v", err)
		}
		return p
	}
	staleP := writeLock("stale.lock", dead)
	liveP := writeLock("live.lock", me)

	d := mkDoctorDeps(t)
	d.LockPaths = []string{staleP, liveP, filepath.Join(dir, "missing.lock")}
	d.ProcessAlive = func(pid int) bool { return pid == me }

	fs := doctorCheckLocks(d)
	var staleFound bool
	for _, f := range fs {
		if f.Severity == sevWarning && strings.Contains(f.Message, staleP) {
			staleFound = true
			if !strings.Contains(f.Message, fmt.Sprintf("PID %d is dead", dead)) {
				t.Errorf("dead-PID detail missing: %s", f.Message)
			}
		}
		if strings.Contains(f.Message, liveP) && f.Severity == sevWarning {
			t.Errorf("live-owned lock flagged: %s", f.Message)
		}
	}
	if !staleFound {
		t.Error("expected stale lock warning")
	}
}

// --- check: orphans ----------------------------------------------------------------------------

func TestDoctorCheckOrphans_WorktreeAndTmux(t *testing.T) {
	dir := t.TempDir()
	wtBase := filepath.Join(dir, "worktrees")

	d := mkDoctorDeps(t)
	d.WorktreeBase = wtBase
	d.Stories = []state.Story{
		{ID: "s-live", Status: "in_progress"},
		{ID: "s-old", Status: "merged"},
	}
	d.ListWorktrees = func(string) ([]string, error) {
		return []string{dir, filepath.Join(wtBase, "s-live"), filepath.Join(wtBase, "s-old")}, nil
	}
	d.ListTmux = func() ([]string, error) {
		return []string{"vxd-s-live", "vxd-orphan-s-gone", "unrelated-session"}, nil
	}

	fs := doctorCheckOrphans(d)
	var wtOld, sessGone bool
	for _, f := range fs {
		if f.Severity != sevWarning {
			continue
		}
		if strings.Contains(f.Message, filepath.Join(wtBase, "s-old")) {
			wtOld = true
		}
		if strings.Contains(f.Message, "vxd-orphan-s-gone") {
			sessGone = true
		}
		if strings.Contains(f.Message, "s-live") || strings.Contains(f.Message, "unrelated-session") {
			t.Errorf("active/unrelated resource wrongly flagged: %s", f.Message)
		}
	}
	if !wtOld {
		t.Error("expected orphaned worktree warning for s-old")
	}
	if !sessGone {
		t.Error("expected orphaned tmux session warning for vxd-orphan-s-gone")
	}
}

// --- check: merge base ---------------------------------------------------------------------------

func TestDoctorCheckMergeBase_MasterFallbackAndMismatch(t *testing.T) {
	// Repo only has master: detected via candidate order, no config conflict.
	d := mkDoctorDeps(t)
	d.Cfg.Merge.BaseBranch = ""
	d.RefExists = func(_, ref string) bool { return ref == "master" }
	fs := doctorCheckMergeBase(d)
	if len(fs) != 1 || fs[0].Severity != sevInfo || !strings.Contains(fs[0].Message, "master") {
		t.Fatalf("expected info finding detecting master, got %+v", fs)
	}

	// Config says master but repo resolves origin/main first -> mismatch warning.
	d2 := mkDoctorDeps(t)
	d2.Cfg.Merge.BaseBranch = "master"
	d2.RefExists = func(_, ref string) bool { return ref == "origin/main" }
	fs = doctorCheckMergeBase(d2)
	if len(fs) != 1 || fs[0].Severity != sevWarning || !strings.Contains(fs[0].Message, "merge.base_branch") {
		t.Fatalf("expected base_branch mismatch warning, got %+v", fs)
	}

	// No base refs at all -> warning.
	d3 := mkDoctorDeps(t)
	d3.RefExists = func(string, string) bool { return false }
	fs = doctorCheckMergeBase(d3)
	if len(fs) != 1 || fs[0].Severity != sevWarning || !strings.Contains(fs[0].Message, "no main/master ref") {
		t.Fatalf("expected missing-ref warning, got %+v", fs)
	}
}

// --- check: dirty repo ------------------------------------------------------------------------------

func TestDoctorCheckDirtyRepo_CleanAndDirty(t *testing.T) {
	d := mkDoctorDeps(t)
	if fs := doctorCheckDirtyRepo(d); len(fs) != 1 || fs[0].Severity != sevInfo {
		t.Fatalf("expected clean info finding, got %+v", fs)
	}

	d2 := mkDoctorDeps(t)
	d2.GitStatus = func(string) (string, error) { return " M main.go\n?? notes.txt\n", nil }
	fs := doctorCheckDirtyRepo(d2)
	if len(fs) != 1 || fs[0].Severity != sevWarning {
		t.Fatalf("expected dirty warning, got %+v", fs)
	}
	if !strings.Contains(fs[0].Message, "2 uncommitted change") {
		t.Errorf("expected change count in message: %s", fs[0].Message)
	}
}

// --- JSON output ----------------------------------------------------------------------------------------

func TestDoctor_JSONOutput(t *testing.T) {
	d := mkDoctorDeps(t)
	base := d.Now()
	d.Stories = []state.Story{{ID: "s-x", ReqID: "r1", Title: "Wedged", Status: "in_progress", CreatedAt: base}}
	d.LastEvent = map[string]time.Time{"s-x": base.Add(-2 * time.Hour)}

	data, err := renderDoctorJSON(collectFindings(d))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rep doctorReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, string(data))
	}
	if rep.Critical < 1 {
		t.Errorf("expected >=1 critical in report, got %+v", rep)
	}
	seen := map[string]bool{}
	for _, f := range rep.Findings {
		if f.Check == "" || f.Severity == "" || f.Message == "" {
			t.Errorf("finding missing required fields: %+v", f)
		}
		seen[f.Check] = true
	}
	for _, want := range []string{"binary_path", "model_ids", "stuck_stories", "locks", "orphans", "merge_base", "dirty_repo"} {
		if !seen[want] {
			t.Errorf("check %q missing from JSON report", want)
		}
	}
}

// --- exit code ---------------------------------------------------------------------------------------------

func TestDoctor_ExitCodeOnCritical(t *testing.T) {
	// Critical present (stuck story) -> non-nil exit error.
	d := mkDoctorDeps(t)
	base := d.Now()
	d.Stories = []state.Story{{ID: "s-y", ReqID: "r1", Status: "in_progress", CreatedAt: base}}
	d.LastEvent = map[string]time.Time{"s-y": base.Add(-3 * time.Hour)}
	err := doctorExitErrorIfCritical(collectFindings(d))
	if err == nil {
		t.Fatal("expected exit error with a critical finding")
	}
	if !strings.Contains(err.Error(), "CRITICAL") {
		t.Errorf("error should mention CRITICAL: %v", err)
	}

	// Warnings only -> nil (exit 0).
	d2 := mkDoctorDeps(t)
	d2.GitStatus = func(string) (string, error) { return "?? x\n", nil }
	if err := doctorExitErrorIfCritical(collectFindings(d2)); err != nil {
		t.Fatalf("warnings-only must exit 0, got: %v", err)
	}
}

// --- end-to-end through the command (wiring + JSON + exit code together) -------------------------------------

func TestDoctor_RunIntegration_JSONAndExitCode(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{"id": "REQDOC1", "title": "Doc Req"})
	if err := s.Events.Append(reqEvt); err != nil {
		t.Fatal(err)
	}
	if err := s.Proj.Project(reqEvt); err != nil {
		t.Fatal(err)
	}

	// Backdate creation to 3h ago: the stuck check keys off the NEWEST event
	// per story, so a just-written STORY_CREATED would mask the older
	// STORY_STARTED and the fixture would not be stuck at all.
	createEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "DOC001", map[string]any{
		"id": "DOC001", "req_id": "REQDOC1", "title": "Wedged story", "complexity": 2,
	})
	createEvt.Timestamp = time.Now().Add(-3 * time.Hour)
	if err := s.Events.Append(createEvt); err != nil {
		t.Fatal(err)
	}
	if err := s.Proj.Project(createEvt); err != nil {
		t.Fatal(err)
	}

	// A STORY_STARTED two hours ago leaves DOC001 in_progress and silent —
	// past the 10-minute stuck threshold.
	oldStart := state.Event{
		ID:        "01DOCTESTOLDSTART000000000000000",
		Type:      state.EventStoryStarted,
		Timestamp: time.Now().Add(-2 * time.Hour),
		AgentID:   "agent-doc",
		StoryID:   "DOC001",
		Payload:   []byte(`{"branch":"vxd/DOC001"}`),
	}
	if err := s.Events.Append(oldStart); err != nil {
		t.Fatal(err)
	}
	if err := s.Proj.Project(oldStart); err != nil {
		t.Fatal(err)
	}

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	cmd := newDoctorCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	t.Setenv("HOME", dir)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	runErr := cmd.Execute()

	var rep doctorReport
	if jerr := json.Unmarshal(buf.Bytes(), &rep); jerr != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", jerr, buf.String())
	}
	if rep.Critical < 1 {
		t.Errorf("expected >=1 critical finding, got %+v", rep)
	}
	var stuckFound bool
	for _, f := range rep.Findings {
		if f.Check == "stuck_stories" && f.Severity == sevCritical && strings.Contains(f.Message, "DOC001") {
			stuckFound = true
		}
	}
	if !stuckFound {
		t.Error("expected stuck_stories critical finding for DOC001")
	}
	if runErr == nil {
		t.Fatal("expected exit error due to critical finding")
	}
	if _, ok := runErr.(doctorExitError); !ok {
		t.Errorf("expected doctorExitError, got %T: %v", runErr, runErr)
	}
}

// --- wiring ---------------------------------------------------------------------------------------------------

func TestDoctor_Wiring_RootCommand(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "doctor" {
			found = true
			if c.Flags().Lookup("req") == nil {
				t.Error("doctor command missing --req flag")
			}
			if c.Flags().Lookup("json") == nil {
				t.Error("doctor command missing --json flag")
			}
		}
	}
	if !found {
		t.Fatal("doctor command not registered on rootCmd")
	}
}

func TestDoctor_Wiring_Documentation(t *testing.T) {
	claudeMD, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	if err != nil {
		t.Skipf("cannot read CLAUDE.md: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Skipf("cannot read README.md: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(claudeMD)), "vxd doctor") {
		t.Error("CLAUDE.md CLI table missing 'vxd doctor'")
	}
	if !strings.Contains(strings.ToLower(string(readme)), "vxd doctor") {
		t.Error("README.md missing 'vxd doctor'")
	}
	if !strings.Contains(string(readme), "Troubleshooting") {
		t.Error("README.md missing Troubleshooting section")
	}
}
