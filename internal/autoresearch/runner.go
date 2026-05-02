package autoresearch

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// AgentDriver abstracts the "spawn an agent in a worktree and wait for it to
// finish" operation. Real implementation uses internal/runtime; tests inject
// a fake.
type AgentDriver interface {
	// RunAgent runs the agent on the prepared worktree using the given prompt
	// and a hard deadline (budget). On return, the worktree must reflect the
	// agent's edits committed to its branch.
	RunAgent(ctx context.Context, worktree, branch, prompt string, budget time.Duration) error

	// Diff returns the patch (vs base) that the agent produced. Empty string
	// means the agent made no changes.
	Diff(worktree, baseRef string) (string, error)

	// PathsTouched returns the file paths the agent modified relative to repo root.
	PathsTouched(worktree, baseRef string) ([]string, error)
}

// WorktreeOps creates and removes worktrees.
type WorktreeOps interface {
	Create(repoDir, worktreePath, branch string) error
	Remove(repoDir, worktreePath, branch string) error
}

// PathFilter encapsulates the allowlist + denylist check.
type PathFilter struct {
	Allow []string // glob patterns; empty = block everything
	Deny  []string // glob patterns; checked after allow
}

// Check returns nil if all paths satisfy the filter, otherwise the first
// offending path and an explanation.
func (p PathFilter) Check(paths []string) (offending string, ok bool) {
	for _, path := range paths {
		if !p.matchAny(p.Allow, path) {
			return path, false
		}
		if p.matchAny(p.Deny, path) {
			return path, false
		}
	}
	return "", true
}

func (p PathFilter) matchAny(patterns []string, path string) bool {
	for _, pat := range patterns {
		ok, _ := filepath.Match(pat, path)
		if ok {
			return true
		}
		// Support recursive ** prefixes via stdlib doublestar fallback:
		// filepath.Match doesn't expand **, so we approximate with a
		// segment-aware matcher.
		if matchDoublestar(pat, path) {
			return true
		}
	}
	return false
}

// matchDoublestar matches glob patterns containing "**" recursively.
// Limited replacement for filepath.Match where "**" means "any path
// segments". Other special chars use stdlib semantics on the trailing
// segment only.
func matchDoublestar(pat, path string) bool {
	if pat == "" {
		return path == ""
	}
	// Split on first "**".
	const star = "**"
	idx := indexOf(pat, star)
	if idx < 0 {
		ok, _ := filepath.Match(pat, path)
		return ok
	}
	prefix := pat[:idx]
	suffix := pat[idx+len(star):]
	// Strip trailing/leading slashes around prefix/suffix.
	prefix = trimSuffix(prefix, "/")
	suffix = trimPrefix(suffix, "/")

	if prefix != "" && !hasPrefix(path, prefix+"/") && path != prefix {
		return false
	}
	rest := path
	if prefix != "" {
		rest = trimPrefix(rest, prefix+"/")
	}
	// Try matching suffix at any tail position.
	for i := 0; i <= len(rest); i++ {
		tail := rest[i:]
		if suffix == "" {
			return true
		}
		// suffix may itself contain ** — recurse.
		if matchDoublestar(suffix, tail) {
			return true
		}
	}
	return false
}

// ExperimentRunner orchestrates one experiment lifecycle. It is stateless;
// you can construct one and reuse it across many concurrent experiments
// (each invocation operates on its own worktree).
type ExperimentRunner struct {
	RepoDir       string
	BaseBranch    string
	WorktreeRoot  string
	Worktree      WorktreeOps
	Driver        AgentDriver
	Filter        PathFilter
	Metric        *MetricHarness
	Tripwire      *TripwireJudge
	Bank          *HypothesisBank
	Sampler       *BayesSampler
	Gate          *GateRouter
	GateAction    GateAction
	Conventions   Conventions
	Events        EventSink
	Now           func() time.Time // injectable clock for tests
}

// EventSink decouples runner from a concrete event store.
type EventSink interface {
	Append(evt state.Event) error
}

// Run executes one experiment. Always returns an Outcome describing the
// result (kept, discarded, or failed) plus an error only on infrastructure
// failures the caller might retry.
func (r *ExperimentRunner) Run(ctx context.Context, p Proposal, baseline float64, budget time.Duration) (Outcome, error) {
	now := r.now
	started := now()
	out := Outcome{Proposal: p, Started: started, Baseline: baseline}

	worktreeName := "exp-" + p.ID
	worktreePath := filepath.Join(r.WorktreeRoot, worktreeName)
	branch := "autoresearch/exp-" + p.ID
	out.Worktree = worktreePath
	out.BranchOrPR = branch

	// 1. Prepare worktree.
	if err := r.Worktree.Create(r.RepoDir, worktreePath, branch); err != nil {
		out.InfraCaused = true
		out.FailReason = "worktree_create"
		out.Verdict = VerdictRejected
		r.emit(state.EventExperimentFailed, p.Repo, outcomePayload(out, "infra_caused", true))
		return out, fmt.Errorf("worktree create: %w", err)
	}
	r.emit(state.EventExperimentRunning, p.Repo, map[string]any{
		"id":        p.ID,
		"repo":      p.Repo,
		"class":     string(p.Class),
		"worktree":  worktreePath,
		"branch":    branch,
		"started":   started.UTC(),
	})

	// 2. Run agent.
	agentCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if err := r.Driver.RunAgent(agentCtx, worktreePath, branch, p.Prompt, budget); err != nil {
		out.FailReason = "timeout_or_agent_error"
		out.Verdict = VerdictRejected
		out.Finished = now()
		out.Duration = out.Finished.Sub(out.Started)
		r.emit(state.EventExperimentDiscarded, p.Repo, outcomePayload(out, "reason", "timeout"))
		// Bayes loss for agent-caused failure.
		r.Sampler.Update(p.Repo, p.Class, false)
		_ = r.Worktree.Remove(r.RepoDir, worktreePath, branch)
		return out, nil
	}

	// 3. Inspect diff.
	diff, err := r.Driver.Diff(worktreePath, "origin/"+r.BaseBranch)
	if err != nil {
		out.InfraCaused = true
		out.FailReason = "diff_read"
		out.Verdict = VerdictRejected
		r.emit(state.EventExperimentFailed, p.Repo, outcomePayload(out, "infra_caused", true))
		_ = r.Worktree.Remove(r.RepoDir, worktreePath, branch)
		return out, err
	}
	if diff == "" {
		out.FailReason = "no_diff"
		out.Verdict = VerdictRejected
		out.Finished = now()
		out.Duration = out.Finished.Sub(out.Started)
		r.emit(state.EventExperimentDiscarded, p.Repo, outcomePayload(out, "reason", "no_diff"))
		r.Sampler.Update(p.Repo, p.Class, false)
		_ = r.Worktree.Remove(r.RepoDir, worktreePath, branch)
		return out, nil
	}
	out.Diff = diff
	out.DiffHash = HashDiff(diff)

	// 4. Allowlist + denylist.
	paths, err := r.Driver.PathsTouched(worktreePath, "origin/"+r.BaseBranch)
	if err == nil {
		if offending, ok := r.Filter.Check(paths); !ok {
			out.Verdict = VerdictRejected
			out.VerdictNote = "scope: " + offending
			out.Finished = now()
			out.Duration = out.Finished.Sub(out.Started)
			r.emit(state.EventExperimentTripwired, p.Repo, outcomePayload(out,
				"reason", "scope",
				"path", offending,
			))
			r.Sampler.Update(p.Repo, p.Class, false)
			_ = r.Worktree.Remove(r.RepoDir, worktreePath, branch)
			return out, nil
		}
	}

	// 5. Measure.
	score, merr := r.Metric.Measure(ctx, worktreePath, baseline, diff)
	if merr != nil {
		out.FailReason = "metric_failed"
		out.Verdict = VerdictRejected
		out.Finished = now()
		out.Duration = out.Finished.Sub(out.Started)
		r.emit(state.EventExperimentDiscarded, p.Repo, outcomePayload(out, "reason", "metric_failed", "error", merr.Error()))
		r.Sampler.Update(p.Repo, p.Class, false)
		_ = r.Worktree.Remove(r.RepoDir, worktreePath, branch)
		return out, nil
	}
	out.Score = score
	out.Delta = signedDelta(score, baseline)
	r.emit(state.EventExperimentMeasured, p.Repo, map[string]any{
		"id":        p.ID,
		"repo":      p.Repo,
		"score":     score.Final,
		"baseline":  baseline,
		"delta":     out.Delta,
		"diff_hash": out.DiffHash,
	})

	// 6. Tripwire.
	verdict, note, terr := r.Tripwire.Judge(ctx, diff, score.Final, baseline, r.Conventions)
	out.Verdict = verdict
	out.VerdictNote = note
	r.emit(state.EventExperimentTripwired, p.Repo, map[string]any{
		"id":      p.ID,
		"repo":    p.Repo,
		"verdict": string(verdict),
		"reason":  note,
		"diff_hash": out.DiffHash,
	})
	if terr != nil || verdict != VerdictOK {
		out.Finished = now()
		out.Duration = out.Finished.Sub(out.Started)
		r.emit(state.EventExperimentDiscarded, p.Repo, outcomePayload(out, "reason", "tripwire"))
		r.Sampler.Update(p.Repo, p.Class, false)
		_ = r.Worktree.Remove(r.RepoDir, worktreePath, branch)
		return out, nil
	}

	// 7. Did it improve baseline?
	if !score.Improves(baseline) {
		out.Finished = now()
		out.Duration = out.Finished.Sub(out.Started)
		r.emit(state.EventExperimentDiscarded, p.Repo, outcomePayload(out, "reason", "no_improvement"))
		r.Sampler.Update(p.Repo, p.Class, false)
		_ = r.Worktree.Remove(r.RepoDir, worktreePath, branch)
		return out, nil
	}

	// 8. Route through gate.
	gateOut, gerr := r.Gate.Route(r.RepoDir, r.GateAction, Outcome{
		Proposal:    p,
		Score:       score,
		Baseline:    baseline,
		Delta:       out.Delta,
		Verdict:     verdict,
		VerdictNote: note,
		Kept:        true,
		BranchOrPR:  branch,
		Worktree:    worktreePath,
		Started:     started,
		Finished:    now(),
	})
	if gerr != nil {
		out.InfraCaused = true
		out.FailReason = "gate_route"
		out.Finished = now()
		out.Duration = out.Finished.Sub(out.Started)
		r.emit(state.EventExperimentFailed, p.Repo, outcomePayload(out,
			"reason", "gate_route",
			"error", gerr.Error(),
			"infra_caused", true,
		))
		return out, gerr
	}
	out.Kept = true
	out.GateAction = string(r.GateAction)
	out.BranchOrPR = gateOut
	out.Finished = now()
	out.Duration = out.Finished.Sub(out.Started)
	r.emit(state.EventExperimentKept, p.Repo, outcomePayload(out, "branch_or_pr", gateOut))
	r.Sampler.Update(p.Repo, p.Class, true)
	return out, nil
}

func (r *ExperimentRunner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

func (r *ExperimentRunner) emit(t state.EventType, repo string, payload map[string]any) {
	if r.Events == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["repo"]; !ok {
		payload["repo"] = repo
	}
	evt := state.NewEvent(t, "autoresearch", "", payload)
	_ = r.Events.Append(evt)
}

// signedDelta returns a signed improvement magnitude where positive = better,
// regardless of metric direction. Callers can record a single sign-stable
// number in events.
func signedDelta(s Score, baseline float64) float64 {
	if s.LowerIsBetter {
		return baseline - s.Final
	}
	return s.Final - baseline
}

func outcomePayload(o Outcome, kvs ...any) map[string]any {
	m := map[string]any{
		"id":        o.Proposal.ID,
		"repo":      o.Proposal.Repo,
		"class":     string(o.Proposal.Class),
		"diff_hash": o.DiffHash,
		"score":     o.Score.Final,
		"baseline":  o.Baseline,
		"delta":     o.Delta,
		"verdict":   string(o.Verdict),
	}
	if o.FailReason != "" {
		m["fail_reason"] = o.FailReason
	}
	for i := 0; i+1 < len(kvs); i += 2 {
		key, ok := kvs[i].(string)
		if !ok {
			continue
		}
		m[key] = kvs[i+1]
	}
	return m
}

// NewProposalID returns a fresh ULID string for a Proposal.ID.
func NewProposalID() string {
	return ulid.Make().String()
}

// --- tiny string helpers used by matchDoublestar so this package stays
// stdlib-only and self-contained. Not perf-critical.

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimPrefix(s, prefix string) string {
	if hasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
