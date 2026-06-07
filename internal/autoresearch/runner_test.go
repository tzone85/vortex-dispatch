package autoresearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Fakes -----------------------------------------------------------------

type fakeWorktreeOps struct {
	mu        sync.Mutex
	created   []string
	removed   []string
	createErr error
}

func (f *fakeWorktreeOps) Create(_, path, _ string) error {
	if f.createErr != nil {
		return f.createErr
	}
	// Real worktrees are real directories. Mkdir so MetricHarness.Measure
	// can chdir into the worktree and exec its shell command.
	if err := osMkdirAll(path); err != nil {
		return err
	}
	f.mu.Lock()
	f.created = append(f.created, path)
	f.mu.Unlock()
	return nil
}
func (f *fakeWorktreeOps) Remove(_, path, _ string) error {
	f.mu.Lock()
	f.removed = append(f.removed, path)
	f.mu.Unlock()
	return nil
}

// osMkdirAll wraps os.MkdirAll so the test file stays import-light.
func osMkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

type fakeAgentDriver struct {
	diff       string
	paths      []string
	runErr     error
	diffErr    error
	pathsErr   error
}

func (f *fakeAgentDriver) RunAgent(_ context.Context, _, _, _ string, _ time.Duration) error {
	return f.runErr
}
func (f *fakeAgentDriver) Diff(_, _ string) (string, error) {
	return f.diff, f.diffErr
}
func (f *fakeAgentDriver) PathsTouched(_, _ string) ([]string, error) {
	return f.paths, f.pathsErr
}

// Helper to make a runner with sane defaults --------------------------

func newTestRunner(t *testing.T, drv *fakeAgentDriver, wt *fakeWorktreeOps) (*ExperimentRunner, *fakeGateOps, *state.FileStore) {
	t.Helper()
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	t.Cleanup(func() { store.Close() })

	gateOps := newFakeGateOps()
	gateOps.branchExists["autoresearch/winning"] = true

	r := &ExperimentRunner{
		RepoDir:      "/repo",
		BaseBranch:   "main",
		WorktreeRoot: dir,
		Worktree:     wt,
		Driver:       drv,
		Filter:       PathFilter{Allow: []string{"src/**", "internal/**", "internal/**/*.go"}},
		Metric: &MetricHarness{
			Metric: config.AutoresearchMetric{
				Command: "echo 90",
				Parser:  config.AutoresearchMetricParser{Kind: "last_float", LowerIsBetter: true},
			},
		},
		Tripwire: &TripwireJudge{Client: scriptedClient{reply: "OK|fine"}, Model: "test"},
		Bank:     NewHypothesisBank(store),
		Sampler:  NewBayesSampler(nil, 1, 1),
		Gate:     NewGateRouter("main", gateOps),
		GateAction: GateWinning,
		Events:   store,
		Now:      func() time.Time { return time.Unix(0, 0).UTC() },
	}
	return r, gateOps, store
}

// Tests -----------------------------------------------------------------

func TestExperimentRunner_HappyPath_KeptAndGated(t *testing.T) {
	drv := &fakeAgentDriver{
		diff:  "diff content here",
		paths: []string{"src/main.go"},
	}
	r, gate, store := newTestRunner(t, drv, &fakeWorktreeOps{})

	out, err := r.Run(context.Background(), Proposal{ID: "e1", Repo: "r1", Class: ClassPerf}, 100, 5*time.Second)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Kept {
		t.Errorf("expected Kept=true, got %+v", out)
	}
	if len(gate.fastForwarded) != 1 {
		t.Errorf("expected gate fast-forward, got %v", gate.fastForwarded)
	}
	// Bayes should reflect a kept outcome on (r1, perf).
	a, b := r.Sampler.Posterior("r1", ClassPerf)
	if a != 2 || b != 1 {
		t.Errorf("Bayes after kept: α=%v β=%v", a, b)
	}
	// At least 4 events emitted (running, measured, tripwired, kept).
	evts, _ := store.List(state.EventFilter{})
	wantSeq := []state.EventType{
		state.EventExperimentRunning,
		state.EventExperimentMeasured,
		state.EventExperimentTripwired,
		state.EventExperimentKept,
	}
	for i, w := range wantSeq {
		if i >= len(evts) || evts[i].Type != w {
			t.Errorf("event[%d] = %s, want %s", i, evts[i].Type, w)
		}
	}
}

func TestExperimentRunner_NoDiff_DiscardedAndBayesLoss(t *testing.T) {
	drv := &fakeAgentDriver{diff: ""}
	r, gate, store := newTestRunner(t, drv, &fakeWorktreeOps{})

	out, _ := r.Run(context.Background(), Proposal{ID: "e1", Repo: "r1", Class: ClassPerf}, 100, time.Second)
	if out.Kept {
		t.Error("no_diff must not be kept")
	}
	if len(gate.fastForwarded) != 0 {
		t.Error("no_diff must not gate")
	}
	a, b := r.Sampler.Posterior("r1", ClassPerf)
	if a != 1 || b != 2 {
		t.Errorf("Bayes after no_diff: α=%v β=%v (expect 1,2)", a, b)
	}
	hasDiscarded := false
	evts, _ := store.List(state.EventFilter{})
	for _, e := range evts {
		if e.Type == state.EventExperimentDiscarded {
			hasDiscarded = true
		}
	}
	if !hasDiscarded {
		t.Error("EXPERIMENT_DISCARDED must be emitted on no_diff")
	}
}

func TestExperimentRunner_ScopeViolation_TripwiredAndBayesLoss(t *testing.T) {
	drv := &fakeAgentDriver{
		diff:  "diff",
		paths: []string{".github/workflows/release.yml"},
	}
	r, gate, _ := newTestRunner(t, drv, &fakeWorktreeOps{})

	out, _ := r.Run(context.Background(), Proposal{ID: "e1", Repo: "r1", Class: ClassPerf}, 100, time.Second)
	if out.Kept {
		t.Error("scope violation must not be kept")
	}
	if out.Verdict != VerdictRejected {
		t.Errorf("scope violation must be REJECTED, got %s", out.Verdict)
	}
	if len(gate.fastForwarded) != 0 {
		t.Error("scope violation must not gate")
	}
	a, b := r.Sampler.Posterior("r1", ClassPerf)
	if a != 1 || b != 2 {
		t.Errorf("scope must be Bayes loss; got α=%v β=%v", a, b)
	}
}

func TestExperimentRunner_TripwireSuspicious_DiscardedAndBayesLoss(t *testing.T) {
	drv := &fakeAgentDriver{diff: "diff", paths: []string{"src/main.go"}}
	r, gate, _ := newTestRunner(t, drv, &fakeWorktreeOps{})
	r.Tripwire = &TripwireJudge{Client: scriptedClient{reply: "REJECTED|deletes tests"}, Model: "test"}

	out, _ := r.Run(context.Background(), Proposal{ID: "e1", Repo: "r1", Class: ClassPerf}, 100, time.Second)
	if out.Kept {
		t.Error("REJECTED tripwire must discard")
	}
	if len(gate.fastForwarded) != 0 {
		t.Error("REJECTED tripwire must not gate")
	}
	a, b := r.Sampler.Posterior("r1", ClassPerf)
	if a != 1 || b != 2 {
		t.Errorf("Bayes loss expected; got α=%v β=%v", a, b)
	}
}

func TestExperimentRunner_NoImprovement_DiscardedAndBayesLoss(t *testing.T) {
	// Score 100 vs baseline 100 → not an improvement (lower-is-better).
	drv := &fakeAgentDriver{diff: "diff", paths: []string{"src/main.go"}}
	r, gate, _ := newTestRunner(t, drv, &fakeWorktreeOps{})
	r.Metric.Metric.Command = "echo 100" // matches baseline

	out, _ := r.Run(context.Background(), Proposal{ID: "e1", Repo: "r1", Class: ClassPerf}, 100, time.Second)
	if out.Kept {
		t.Error("no improvement must discard")
	}
	if len(gate.fastForwarded) != 0 {
		t.Error("no improvement must not gate")
	}
}

func TestExperimentRunner_WorktreeCreateFailure_InfraSkipsBayes(t *testing.T) {
	drv := &fakeAgentDriver{}
	wt := &fakeWorktreeOps{createErr: errors.New("disk full")}
	r, _, _ := newTestRunner(t, drv, wt)

	out, err := r.Run(context.Background(), Proposal{ID: "e1", Repo: "r1", Class: ClassPerf}, 100, time.Second)
	if err == nil {
		t.Error("worktree create error must surface")
	}
	if !out.InfraCaused {
		t.Error("worktree create failure must set InfraCaused")
	}
	a, b := r.Sampler.Posterior("r1", ClassPerf)
	if a != 1 || b != 1 {
		t.Errorf("infra failure must NOT update Bayes; got α=%v β=%v (expect 1,1)", a, b)
	}
}

func TestExperimentRunner_AgentRunError_BayesLoss(t *testing.T) {
	drv := &fakeAgentDriver{runErr: errors.New("budget exceeded")}
	r, _, _ := newTestRunner(t, drv, &fakeWorktreeOps{})

	out, _ := r.Run(context.Background(), Proposal{ID: "e1", Repo: "r1", Class: ClassPerf}, 100, time.Second)
	if out.Kept {
		t.Error("agent error must discard")
	}
	a, b := r.Sampler.Posterior("r1", ClassPerf)
	if a != 1 || b != 2 {
		t.Errorf("agent timeout must be Bayes loss; got α=%v β=%v", a, b)
	}
}

func TestPathFilter_Doublestar(t *testing.T) {
	f := PathFilter{Allow: []string{"src/**/*.go"}}
	if _, ok := f.Check([]string{"src/foo/bar.go"}); !ok {
		t.Error("src/foo/bar.go should be allowed by src/**/*.go")
	}
	if _, ok := f.Check([]string{"src/main.go"}); !ok {
		t.Error("src/main.go should be allowed by src/**/*.go")
	}
	if _, ok := f.Check([]string{"vendor/x.go"}); ok {
		t.Error("vendor/x.go must NOT be allowed by src/**/*.go")
	}
}

func TestPathFilter_Denylist(t *testing.T) {
	f := PathFilter{
		Allow: []string{"**/*.go"},
		Deny:  []string{"**/*_test.go"},
	}
	if _, ok := f.Check([]string{"src/main.go"}); !ok {
		t.Error("src/main.go must be allowed")
	}
	if _, ok := f.Check([]string{"src/main_test.go"}); ok {
		t.Error("src/main_test.go must be blocked by denylist")
	}
}
