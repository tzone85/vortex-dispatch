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
// when a review rejects a story for the first time, the monitor emits a
// STORY_REVIEW_FAILED event with feedback (from "monitor") in addition to the
// one from the Reviewer, resetting the story to draft for retry.
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
	// 2. From handleReviewFailure() (agent_id="monitor") with feedback
	events, err := es.List(state.EventFilter{Type: state.EventStoryReviewFailed})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 EventStoryReviewFailed events, got %d", len(events))
	}

	// Verify the monitor event has feedback in the payload.
	monitorEvents, err := es.List(state.EventFilter{
		Type:    state.EventStoryReviewFailed,
		AgentID: "monitor",
		StoryID: "s-rej-001",
	})
	if err != nil {
		t.Fatalf("list monitor events: %v", err)
	}
	if len(monitorEvents) != 1 {
		t.Fatalf("expected 1 monitor review-failed event, got %d", len(monitorEvents))
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

// TestPostExecution_ReviewRejected_RetriesWithFeedback verifies that when a
// story fails code review for the first time, a STORY_REVIEW_FAILED event is
// emitted with feedback and comments in its payload.
func TestPostExecution_ReviewRejected_RetriesWithFeedback(t *testing.T) {
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

	// Verify monitor emitted a STORY_REVIEW_FAILED with feedback.
	monitorEvents, err := es.List(state.EventFilter{
		Type:    state.EventStoryReviewFailed,
		AgentID: "monitor",
		StoryID: storyID,
	})
	if err != nil {
		t.Fatalf("list monitor events: %v", err)
	}
	if len(monitorEvents) != 1 {
		t.Fatalf("expected 1 monitor STORY_REVIEW_FAILED event, got %d", len(monitorEvents))
	}

	// Verify the event has feedback in the payload.
	var payload map[string]any
	if err := json.Unmarshal(monitorEvents[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if feedback, ok := payload["feedback"].(string); !ok || feedback == "" {
		t.Fatal("expected non-empty feedback in event payload")
	}
	if comments, ok := payload["comments"].(string); !ok || comments == "[]" {
		t.Fatal("expected non-empty comments in event payload")
	}
	if reason, ok := payload["reason"].(string); !ok || reason != "review rejected" {
		t.Fatalf("expected reason 'review rejected', got %q", payload["reason"])
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

// TestPostExecution_ReviewRejected_EscalatesOnSecondFailure verifies that when
// a story fails code review twice, an ESCALATION_CREATED event is emitted and
// the story is reset to draft for senior agent dispatch.
func TestPostExecution_ReviewRejected_EscalatesOnSecondFailure(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	storyID := "s-esc-001"
	ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", storyID, map[string]any{
		"id": storyID, "req_id": "r-001", "title": "Escalation task", "description": "desc", "complexity": 3,
	}))

	// Simulate that the reviewer already rejected this story once before
	// (the first STORY_REVIEW_FAILED from reviewer).
	firstReviewEvt := state.NewEvent(state.EventStoryReviewFailed, "reviewer", storyID, map[string]any{
		"passed":        false,
		"comment_count": 1,
		"summary":       "first rejection",
	})
	es.Append(firstReviewEvt)
	ps.Project(firstReviewEvt)

	// And the monitor already emitted a retry event (feedback from first failure).
	monitorRetryEvt := state.NewEvent(state.EventStoryReviewFailed, "monitor", storyID, map[string]any{
		"reason":   "review rejected",
		"feedback": "first rejection",
		"comments": "[]",
	})
	es.Append(monitorRetryEvt)
	ps.Project(monitorRetryEvt)

	// Story was retried and agent started again, then completed.
	es.Append(state.NewEvent(state.EventStoryStarted, "agent-2", storyID, nil))
	ps.Project(state.NewEvent(state.EventStoryStarted, "agent-2", storyID, nil))
	es.Append(state.NewEvent(state.EventStoryCompleted, "agent-2", storyID, nil))
	ps.Project(state.NewEvent(state.EventStoryCompleted, "agent-2", storyID, nil))

	repoDir := setupGitRepoWithFeature(t)

	// Second review also rejects.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": false, "comments": [{"file": "feature.go", "line": 1, "severity": "major", "comment": "still broken"}], "summary": "Still has issues"}`,
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

	// Verify ESCALATION_CREATED event was emitted.
	escalationEvents, err := es.List(state.EventFilter{Type: state.EventEscalationCreated, StoryID: storyID})
	if err != nil {
		t.Fatalf("list escalation events: %v", err)
	}
	if len(escalationEvents) != 1 {
		t.Fatalf("expected 1 ESCALATION_CREATED event, got %d", len(escalationEvents))
	}

	// Verify the escalation event has the correct reason.
	var escPayload map[string]any
	if err := json.Unmarshal(escalationEvents[0].Payload, &escPayload); err != nil {
		t.Fatalf("unmarshal escalation payload: %v", err)
	}
	if reason, ok := escPayload["reason"].(string); !ok || reason != "review failed twice" {
		t.Fatalf("expected escalation reason 'review failed twice', got %q", escPayload["reason"])
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

// TestPostExecution_ReviewRejected_PausesOnThirdFailure verifies that when
// a story fails code review three times (including the senior escalation),
// the requirement is paused for human intervention.
func TestPostExecution_ReviewRejected_PausesOnThirdFailure(t *testing.T) {
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

	// Simulate 2 prior reviewer rejections.
	for i := 0; i < 2; i++ {
		evt := state.NewEvent(state.EventStoryReviewFailed, "reviewer", storyID, map[string]any{
			"passed":        false,
			"comment_count": 1,
			"summary":       "rejection",
		})
		es.Append(evt)
	}

	// Simulate 2 prior monitor retry/escalation events.
	for _, reason := range []string{"review rejected", "review rejected, escalating to senior"} {
		evt := state.NewEvent(state.EventStoryReviewFailed, "monitor", storyID, map[string]any{
			"reason": reason,
		})
		es.Append(evt)
	}

	// Also an escalation event.
	es.Append(state.NewEvent(state.EventEscalationCreated, "monitor", storyID, map[string]any{
		"reason": "review failed twice",
	}))

	repoDir := setupGitRepoWithFeature(t)

	// Third review (from senior) also rejects.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": false, "comments": [], "summary": "Senior also cannot fix this"}`,
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
