package autoresearch

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestCoordinator_RunsTickOnEachWave(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	defer store.Close()

	gateOps := newFakeGateOps()
	gateOps.branchExists["autoresearch/winning"] = true

	var ticks int32
	drv := &fakeAgentDriver{
		diff:  "diff",
		paths: []string{"src/main.go"},
	}
	wt := &fakeWorktreeOps{}
	runner := &ExperimentRunner{
		RepoDir:      "/repo",
		BaseBranch:   "main",
		WorktreeRoot: dir,
		Worktree:     wt,
		Driver:       drv,
		Filter:       PathFilter{Allow: []string{"src/**"}},
		Metric: &MetricHarness{
			Metric: config.AutoresearchMetric{
				Command: "echo 90",
				Parser:  config.AutoresearchMetricParser{Kind: "last_float", LowerIsBetter: true},
			},
		},
		Tripwire:   &TripwireJudge{Client: scriptedClient{reply: "OK|fine"}, Model: "test"},
		Bank:       NewHypothesisBank(store),
		Sampler:    NewBayesSampler(nil, 1, 1),
		Gate:       NewGateRouter("main", gateOps),
		GateAction: GateWinning,
		Events:     store,
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	}

	// Spy on the prompt builder to count invocations.
	c := NewCoordinator("r1", runner.Bank, runner.Sampler, runner, func() float64 { return 100 }, 2, time.Second)
	c.PromptBuilder = countingPromptBuilder{n: &ticks, inner: SimplePromptBuilder{}}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = c.Run(ctx)

	if atomic.LoadInt32(&ticks) < 2 {
		t.Errorf("coordinator should have ticked at least once for each parallel slot, got %d", ticks)
	}
}

func TestCoordinator_StopDrainsCleanly(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	defer store.Close()

	gateOps := newFakeGateOps()
	gateOps.branchExists["autoresearch/winning"] = true

	drv := &fakeAgentDriver{diff: "diff", paths: []string{"src/main.go"}}
	runner := &ExperimentRunner{
		RepoDir:      "/repo",
		BaseBranch:   "main",
		WorktreeRoot: dir,
		Worktree:     &fakeWorktreeOps{},
		Driver:       drv,
		Filter:       PathFilter{Allow: []string{"src/**"}},
		Metric: &MetricHarness{
			Metric: config.AutoresearchMetric{
				Command: "echo 90",
				Parser:  config.AutoresearchMetricParser{Kind: "last_float", LowerIsBetter: true},
			},
		},
		Tripwire:   &TripwireJudge{Client: scriptedClient{reply: "OK|fine"}, Model: "test"},
		Bank:       NewHypothesisBank(store),
		Sampler:    NewBayesSampler(nil, 1, 1),
		Gate:       NewGateRouter("main", gateOps),
		GateAction: GateWinning,
		Events:     store,
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	}

	c := NewCoordinator("r1", runner.Bank, runner.Sampler, runner, func() float64 { return 100 }, 1, time.Second)

	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	c.Stop()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not unblock Run within 2s")
	}
}

type countingPromptBuilder struct {
	n     *int32
	inner PromptBuilder
}

func (c countingPromptBuilder) Build(repo string, class ExperimentClass, programMD string, wins, losses []Experiment) string {
	atomic.AddInt32(c.n, 1)
	return c.inner.Build(repo, class, programMD, wins, losses)
}

func TestSimplePromptBuilder_IncludesWinsAndLosses(t *testing.T) {
	wins := []Experiment{{Class: ClassPerf, Delta: +5, DiffHash: "abc"}}
	losses := []Experiment{{Class: ClassRefactor, FailReason: "scope", DiffHash: "def"}}
	p := SimplePromptBuilder{}.Build("r1", ClassPerf, "the program", wins, losses)
	if !contains(p, "Prior wins") {
		t.Error("prompt must include wins section")
	}
	if !contains(p, "Prior losses") {
		t.Error("prompt must include losses section")
	}
	if !contains(p, "the program") {
		t.Error("prompt must include program.md")
	}
	if !contains(p, "Do NOT delete or weaken tests") {
		t.Error("prompt must include scope guardrails")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}
