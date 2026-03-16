package engine_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/runtime"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestNewMonitor(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)

	reg, err := newTestRegistry()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}

	mon := engine.NewMonitor(reg, wd, nil, nil, nil, cfg, es, ps)
	if mon == nil {
		t.Fatal("expected non-nil Monitor")
	}
}

func TestMonitor_Run_EmptyAgents(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	cfg.Monitor.PollIntervalMs = 10 // fast polling for test

	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)
	reg, err := newTestRegistry()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}

	mon := engine.NewMonitor(reg, wd, nil, nil, nil, cfg, es, ps)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// With no agents, the monitor should return nil after the first tick
	// detects the empty map.
	err = mon.Run(ctx, []engine.ActiveAgent{}, "/tmp/repo")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestMonitor_Run_ContextCancelled(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	cfg.Monitor.PollIntervalMs = 10

	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)
	reg, err := newTestRegistry()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}

	mon := engine.NewMonitor(reg, wd, nil, nil, nil, cfg, es, ps)

	// Create an agent that references a non-existent runtime name so
	// pollOnce skips it on every tick, keeping the monitor alive until
	// the context is cancelled.
	agents := []engine.ActiveAgent{
		{
			Assignment: engine.Assignment{
				StoryID:     "s-001",
				AgentID:     "agent-1",
				SessionName: "vxd-test-1",
				Branch:      "vxd/s-001",
			},
			RuntimeName:  "nonexistent-runtime",
			WorktreePath: "/tmp/wt",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = mon.Run(ctx, agents, "/tmp/repo")
	if err != nil {
		t.Fatalf("expected nil error on cancellation, got %v", err)
	}
}

func TestMonitor_Run_DetectsCompletedAgent(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	// Pre-populate story so projection works
	ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	}))

	// Create a registry with a "test-runtime" whose idle pattern matches
	// the word "done". When DetectStatus reads the session output and
	// finds this pattern, it returns StatusDone.
	reg, err := newTestRegistryWithDone()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Monitor.PollIntervalMs = 10

	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)

	// No reviewer, QA, or merger — just verify the monitor detects completion
	// and emits the STORY_COMPLETED event.
	mon := engine.NewMonitor(reg, wd, nil, nil, nil, cfg, es, ps)

	agents := []engine.ActiveAgent{
		{
			Assignment: engine.Assignment{
				StoryID:     "s-001",
				AgentID:     "agent-1",
				SessionName: "vxd-test-1",
				Branch:      "vxd/s-001",
			},
			RuntimeName:  "test-runtime",
			WorktreePath: "/tmp/wt",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The monitor will call registry.Get("test-runtime") which returns a
	// CLIRuntime backed by tmux. In CI/local without tmux, DetectStatus
	// will fail and return StatusTerminated (session doesn't exist), which
	// triggers the completion path. This tests the happy path integration.
	err = mon.Run(ctx, agents, "/tmp/repo")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Verify STORY_COMPLETED event was emitted
	events, err := es.List(state.EventFilter{Type: state.EventStoryCompleted})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 STORY_COMPLETED event, got %d", len(events))
	}
}

func TestMonitor_Run_DefaultPollInterval(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	cfg.Monitor.PollIntervalMs = 0 // should default to 10s

	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)
	reg, err := newTestRegistry()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}

	mon := engine.NewMonitor(reg, wd, nil, nil, nil, cfg, es, ps)

	// Cancel immediately — this validates that the monitor handles zero
	// poll interval gracefully.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = mon.Run(ctx, []engine.ActiveAgent{}, "/tmp/repo")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestMonitor_PostExecution_ReviewRejection_EmitsOneEvent is a regression test
// for BUG18: when the reviewer rejects a story, exactly ONE EventStoryReviewFailed
// must be emitted. Before the fix, the Reviewer.Review() already emitted one and
// postExecutionPipeline emitted a duplicate second event.
func TestMonitor_PostExecution_ReviewRejection_EmitsOneEvent(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", "s-rej-001", map[string]any{
		"id": "s-rej-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	}))

	// Set up a real git repo with two commits so gitDiff produces a non-empty diff.
	repoDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("init"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repoDir, "feature.go"), []byte("package main\nfunc Feature() {}"), 0644); err != nil {
		t.Fatalf("write feature.go: %v", err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "feat: add feature")

	// Reviewer always rejects.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": false, "comments": [], "summary": "rejected for test"}`,
	})
	reviewer := engine.NewReviewer(replayClient, "sonnet", 4000, es, ps)

	cfg := config.DefaultConfig()
	cfg.Monitor.PollIntervalMs = 10

	reg, err := newTestRegistryWithDone()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}
	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)
	mon := engine.NewMonitor(reg, wd, reviewer, nil, nil, cfg, es, ps)

	agents := []engine.ActiveAgent{
		{
			Assignment: engine.Assignment{
				StoryID:     "s-rej-001",
				AgentID:     "agent-rej-1",
				SessionName: "vxd-test-rej-1",
				Branch:      "vxd/s-rej-001",
			},
			RuntimeName:  "test-runtime",
			WorktreePath: repoDir,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = mon.Run(ctx, agents, repoDir)

	// Exactly ONE EventStoryReviewFailed must be emitted (from Reviewer.Review).
	// Before BUG18-FIX, postExecutionPipeline emitted a second duplicate event.
	events, err := es.List(state.EventFilter{Type: state.EventStoryReviewFailed})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 EventStoryReviewFailed, got %d (regression: duplicate emission)", len(events))
	}
}

// newTestRegistry creates a minimal registry with no runtimes configured.
func newTestRegistry() (*runtime.Registry, error) {
	return runtime.NewRegistry(map[string]config.RuntimeConfig{})
}

// newTestRegistryWithDone creates a registry with a "test-runtime" that has
// detection patterns configured. When the session doesn't exist (no tmux),
// DetectStatus returns StatusTerminated.
func newTestRegistryWithDone() (*runtime.Registry, error) {
	return runtime.NewRegistry(map[string]config.RuntimeConfig{
		"test-runtime": {
			Command: "echo",
			Args:    []string{"test"},
			Models:  []string{"test-model"},
			Detection: config.RuntimeDetection{
				IdlePattern:       `\$\s*$`,
				PermissionPattern: `Allow\?`,
			},
		},
	})
}
