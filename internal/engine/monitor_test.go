package engine_test

import (
	"context"
	"encoding/json"
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

func TestSetManager(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)

	reg, err := newTestRegistry()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}

	mon := engine.NewMonitor(reg, wd, nil, nil, nil, cfg, es, ps)

	// SetManager should not panic.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: "{}"})
	mgr := engine.NewManager(replayClient, "test-model", 4000, es, ps)
	mon.SetManager(mgr)
}

func TestFindDependents(t *testing.T) {
	stories := []engine.PlannedStory{
		{ID: "s-001", DependsOn: nil},
		{ID: "s-002", DependsOn: []string{"s-001"}},
		{ID: "s-003", DependsOn: []string{"s-001", "s-002"}},
		{ID: "s-004", DependsOn: []string{"s-003"}},
	}

	deps := engine.FindDependents(stories, "s-001")
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependents of s-001, got %d: %v", len(deps), deps)
	}

	deps = engine.FindDependents(stories, "s-003")
	if len(deps) != 1 || deps[0] != "s-004" {
		t.Fatalf("expected [s-004], got %v", deps)
	}

	deps = engine.FindDependents(stories, "s-004")
	if len(deps) != 0 {
		t.Fatalf("expected 0 dependents of s-004, got %d", len(deps))
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

// TestMonitor_PostExecution_ReviewRejection_ResetsWithFeedback verifies that
// when a review rejects a story for the first time, the tier-aware
// resetStoryToDraft emits a STORY_REVIEW_FAILED event (in addition to the
// one from the Reviewer), resetting the story to draft for retry within
// the same tier.
func TestMonitor_PostExecution_ReviewRejection_ResetsWithFeedback(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", "s-rej-001", map[string]any{
		"id": "s-rej-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	}))

	repoDir := setupGitRepoWithFeature(t)

	// Reviewer always rejects.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": false, "comments": [{"file": "feature.go", "line": 2, "severity": "critical", "comment": "missing error handling"}], "summary": "rejected for test"}`,
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

	// Two EventStoryReviewFailed events:
	// 1. From Reviewer.Review() (agent_id="reviewer")
	// 2. From resetStoryToDraft() (agent_id="reviewer") — normal retry at tier 0
	events, err := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: "s-rej-001"})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 EventStoryReviewFailed events, got %d", len(events))
	}

	// Verify the reset event has a reason in the payload.
	// The second event is the one from resetStoryToDraft.
	var payload map[string]any
	if err := json.Unmarshal(events[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if reason, ok := payload["reason"].(string); !ok || reason == "" {
		t.Fatal("expected non-empty reason in reset event payload")
	}

	// No escalation events should exist (first failure stays at tier 0).
	escEvents, err := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-rej-001"})
	if err != nil {
		t.Fatalf("list escalation events: %v", err)
	}
	if len(escEvents) != 0 {
		t.Fatalf("expected 0 escalation events on first failure, got %d", len(escEvents))
	}

	// Verify story is reset to draft for re-dispatch.
	story, err := ps.GetStory("s-rej-001")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Status != "draft" {
		t.Fatalf("expected story status 'draft', got %q", story.Status)
	}
}

// TestPostExecution_ReviewRejected_RetriesAtSameTier verifies that when a
// story fails code review for the first time, resetStoryToDraft emits a
// STORY_REVIEW_FAILED event with a reason, and no escalation occurs because
// the tier-0 retry limit has not been reached.
func TestPostExecution_ReviewRejected_RetriesAtSameTier(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	storyID := "s-retry-001"
	ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", storyID, map[string]any{
		"id": storyID, "req_id": "r-001", "title": "Retry task", "description": "desc", "complexity": 3,
	}))

	repoDir := setupGitRepoWithFeature(t)

	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": false, "comments": [{"file": "feature.go", "line": 1, "severity": "critical", "comment": "SQL injection"}], "summary": "Security issues found"}`,
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

	agents := []engine.ActiveAgent{{
		Assignment: engine.Assignment{
			StoryID:     storyID,
			AgentID:     "agent-retry-1",
			SessionName: "vxd-test-retry-1",
			Branch:      "vxd/" + storyID,
		},
		RuntimeName:  "test-runtime",
		WorktreePath: repoDir,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = mon.Run(ctx, agents, repoDir)

	// Two STORY_REVIEW_FAILED events total:
	// 1. From Reviewer.Review() (agent="reviewer")
	// 2. From resetStoryToDraft() (agent="reviewer") — normal retry at tier 0
	allFailEvents, err := es.List(state.EventFilter{
		Type:    state.EventStoryReviewFailed,
		StoryID: storyID,
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(allFailEvents) != 2 {
		t.Fatalf("expected 2 STORY_REVIEW_FAILED events, got %d", len(allFailEvents))
	}

	// Verify the reset event (second one) has a reason.
	var payload map[string]any
	if err := json.Unmarshal(allFailEvents[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if reason, ok := payload["reason"].(string); !ok || reason == "" {
		t.Fatal("expected non-empty reason in reset event payload")
	}

	// No escalation at tier 0 first attempt.
	escEvents, err := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: storyID})
	if err != nil {
		t.Fatalf("list escalation events: %v", err)
	}
	if len(escEvents) != 0 {
		t.Fatalf("expected 0 escalation events, got %d", len(escEvents))
	}

	// Story should be reset to draft.
	story, err := ps.GetStory(storyID)
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Status != "draft" {
		t.Fatalf("expected story status 'draft', got %q", story.Status)
	}
}

// TestPostExecution_ReviewRejected_EscalatesToTier1 verifies that when a
// story's review failures at tier 0 reach the max_retries_before_escalation
// limit, the monitor emits STORY_ESCALATED (tier 0 -> 1) and a
// STORY_REVIEW_FAILED to reset the story to draft for senior dispatch.
func TestPostExecution_ReviewRejected_EscalatesToTier1(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	storyID := "s-esc-001"
	ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", storyID, map[string]any{
		"id": storyID, "req_id": "r-001", "title": "Escalation task", "description": "desc", "complexity": 3,
	}))

	// Pre-seed 1 prior STORY_REVIEW_FAILED at tier 0 (from a previous
	// review cycle). The Reviewer will emit another one during this run,
	// making the total = 2, which equals MaxRetriesBeforeEscalation (2).
	priorEvt := state.NewEvent(state.EventStoryReviewFailed, "reviewer", storyID, map[string]any{
		"reason": "prior rejection",
	})
	es.Append(priorEvt)
	ps.Project(priorEvt)

	repoDir := setupGitRepoWithFeature(t)

	// This review also rejects.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": false, "comments": [{"file": "feature.go", "line": 1, "severity": "major", "comment": "still broken"}], "summary": "Still has issues"}`,
	})
	reviewer := engine.NewReviewer(replayClient, "sonnet", 4000, es, ps)

	cfg := config.DefaultConfig()
	cfg.Monitor.PollIntervalMs = 10
	// Default: MaxRetriesBeforeEscalation = 2

	reg, err := newTestRegistryWithDone()
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}
	wd := engine.NewWatchdog(engine.WatchdogConfig{StuckThresholdS: 120}, es)
	mon := engine.NewMonitor(reg, wd, reviewer, nil, nil, cfg, es, ps)

	agents := []engine.ActiveAgent{{
		Assignment: engine.Assignment{
			StoryID:     storyID,
			AgentID:     "agent-esc-1",
			SessionName: "vxd-test-esc-1",
			Branch:      "vxd/" + storyID,
		},
		RuntimeName:  "test-runtime",
		WorktreePath: repoDir,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = mon.Run(ctx, agents, repoDir)

	// Verify STORY_ESCALATED event was emitted (tier 0 -> 1).
	escalationEvents, err := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: storyID})
	if err != nil {
		t.Fatalf("list escalation events: %v", err)
	}
	if len(escalationEvents) != 1 {
		t.Fatalf("expected 1 STORY_ESCALATED event, got %d", len(escalationEvents))
	}

	var escPayload map[string]any
	if err := json.Unmarshal(escalationEvents[0].Payload, &escPayload); err != nil {
		t.Fatalf("unmarshal escalation payload: %v", err)
	}
	if fromTier, ok := escPayload["from_tier"].(float64); !ok || int(fromTier) != 0 {
		t.Fatalf("expected from_tier 0, got %v", escPayload["from_tier"])
	}
	if toTier, ok := escPayload["to_tier"].(float64); !ok || int(toTier) != 1 {
		t.Fatalf("expected to_tier 1, got %v", escPayload["to_tier"])
	}

	// Story should be reset to draft for senior dispatch.
	story, err := ps.GetStory(storyID)
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Status != "draft" {
		t.Fatalf("expected story status 'draft', got %q", story.Status)
	}
}

// TestPostExecution_ReviewRejected_PausesWhenAllTiersExhausted verifies that
// when a story has been escalated through all tiers (0 -> 1 -> 2 -> 3) and
// the final tier also fails, the requirement is paused for human intervention
// because nextTier (4) exceeds the maximum.
func TestPostExecution_ReviewRejected_PausesWhenAllTiersExhausted(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	storyID := "s-pause-001"

	// Create requirement first so pauseRequirement can look it up.
	ps.Project(state.NewEvent(state.EventReqSubmitted, "user", "", map[string]any{
		"id": "r-pause", "title": "Pause test req", "description": "desc",
	}))

	ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", storyID, map[string]any{
		"id": storyID, "req_id": "r-pause", "title": "Pause task", "description": "desc", "complexity": 3,
	}))

	// Simulate prior escalation through all tiers: 0->1, 1->2, 2->3.
	for _, esc := range []struct{ from, to int }{{0, 1}, {1, 2}, {2, 3}} {
		evt := state.NewEvent(state.EventStoryEscalated, "reviewer", storyID, map[string]any{
			"from_tier": esc.from,
			"to_tier":   esc.to,
			"reason":    "review failed at tier",
		})
		es.Append(evt)
		ps.Project(evt)
	}

	// The Reviewer will emit 1 new STORY_REVIEW_FAILED during this run.
	// At tier 3, MaxRetriesForTier(3) = 1, so count(1) >= max(1) triggers
	// escalation to tier 4, which causes a pause.

	repoDir := setupGitRepoWithFeature(t)

	// Review at tier 3 also rejects.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": false, "comments": [], "summary": "Tech lead also cannot fix this"}`,
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

	agents := []engine.ActiveAgent{{
		Assignment: engine.Assignment{
			StoryID:     storyID,
			AgentID:     "agent-pause-1",
			SessionName: "vxd-test-pause-1",
			Branch:      "vxd/" + storyID,
		},
		RuntimeName:  "test-runtime",
		WorktreePath: repoDir,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = mon.Run(ctx, agents, repoDir)

	// Verify requirement is paused.
	req, err := ps.GetRequirement("r-pause")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Fatalf("expected requirement status 'paused', got %q", req.Status)
	}
}

// setupGitRepoWithFeature creates a temporary git repo that simulates a
// worktree with origin/main. It creates a bare "remote" repo, clones it,
// makes an initial commit on main, pushes, creates a feature branch with
// a feature.go file, and returns the clone directory.
func setupGitRepoWithFeature(t *testing.T) string {
	t.Helper()

	// Create a bare "remote" repo.
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	runGitIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v (%s)", args, dir, err, out)
		}
	}

	cmd := exec.Command("git", "init", "--bare", bareDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v (%s)", err, out)
	}

	// Clone into a working directory.
	cloneDir := filepath.Join(t.TempDir(), "clone")
	cloneCmd := exec.Command("git", "clone", bareDir, cloneDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v (%s)", err, out)
	}

	runGitIn(cloneDir, "config", "user.email", "test@test.com")
	runGitIn(cloneDir, "config", "user.name", "Test")

	// Initial commit on main.
	if err := os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("init"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitIn(cloneDir, "add", ".")
	runGitIn(cloneDir, "commit", "-m", "init")
	runGitIn(cloneDir, "push", "origin", "main")

	// Create feature branch with changes.
	runGitIn(cloneDir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(cloneDir, "feature.go"), []byte("package main\nfunc Feature() {}"), 0644); err != nil {
		t.Fatalf("write feature.go: %v", err)
	}
	runGitIn(cloneDir, "add", ".")
	runGitIn(cloneDir, "commit", "-m", "feat: add feature")

	return cloneDir
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
