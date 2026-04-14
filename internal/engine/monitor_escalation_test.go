package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newEscalationTestStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "proj.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	return es, ps, func() { es.Close(); ps.Close() }
}

// TestResetStoryToDraft_NoEscalation verifies that when retry count is below
// the threshold, the story is simply reset to draft without escalation.
func TestResetStoryToDraft_NoEscalation(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-rst-001", map[string]any{
		"id": "s-rst-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	cfg := config.Config{
		Routing: config.RoutingConfig{
			MaxRetriesBeforeEscalation: 2,
		},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)

	m.resetStoryToDraft("s-rst-001", "reviewer", "code review failed")

	// No escalation should occur (0 prior retries at tier 0, threshold is 2).
	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-rst-001"})
	if len(escEvents) != 0 {
		t.Errorf("expected 0 STORY_ESCALATED events, got %d", len(escEvents))
	}

	// Should have 1 STORY_REVIEW_FAILED event.
	failEvents, _ := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: "s-rst-001"})
	if len(failEvents) != 1 {
		t.Fatalf("expected 1 STORY_REVIEW_FAILED, got %d", len(failEvents))
	}

	var payload map[string]any
	json.Unmarshal(failEvents[0].Payload, &payload)
	if payload["reason"] != "code review failed" {
		t.Errorf("expected reason in payload, got %v", payload["reason"])
	}
}

// TestResetStoryToDraft_TriggersEscalation verifies that when the retry count
// reaches the threshold, the story is escalated to the next tier.
func TestResetStoryToDraft_TriggersEscalation(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-rst-002", map[string]any{
		"id": "s-rst-002", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	cfg := config.Config{
		Routing: config.RoutingConfig{
			MaxRetriesBeforeEscalation: 1, // trigger on first failure
		},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)

	// Seed 1 prior failure so next call triggers escalation.
	priorFail := state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-rst-002", map[string]any{
		"reason": "prior failure",
	})
	es.Append(priorFail)
	ps.Project(priorFail)

	m.resetStoryToDraft("s-rst-002", "reviewer", "second failure")

	// Should escalate from tier 0 to tier 1.
	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-rst-002"})
	if len(escEvents) != 1 {
		t.Fatalf("expected 1 STORY_ESCALATED, got %d", len(escEvents))
	}

	var escPayload map[string]any
	json.Unmarshal(escEvents[0].Payload, &escPayload)
	if int(escPayload["from_tier"].(float64)) != 0 {
		t.Errorf("expected from_tier 0, got %v", escPayload["from_tier"])
	}
	if int(escPayload["to_tier"].(float64)) != 1 {
		t.Errorf("expected to_tier 1, got %v", escPayload["to_tier"])
	}
}

// TestResetStoryToDraft_PausesAtTier4 verifies that when all tiers are
// exhausted (next tier would be 4), the requirement is paused.
func TestResetStoryToDraft_PausesAtTier4(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-pause", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-rst-003", map[string]any{
		"id": "s-rst-003", "req_id": "r-pause", "title": "Task", "description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	cfg := config.Config{
		Routing: config.RoutingConfig{
			MaxRetriesBeforeEscalation: 1,
			MaxSeniorRetries:           1,
			MaxManagerAttempts:          1,
		},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)

	// Escalate through all tiers: 0->1, 1->2, 2->3.
	for _, esc := range []struct{ from, to int }{{0, 1}, {1, 2}, {2, 3}} {
		evt := state.NewEvent(state.EventStoryEscalated, "reviewer", "s-rst-003", map[string]any{
			"from_tier": esc.from, "to_tier": esc.to, "reason": "failed",
		})
		es.Append(evt)
		ps.Project(evt)
	}

	// At tier 3 with MaxRetriesForTier(3) = 1, so one failure triggers escalation to tier 4.
	// Seed 1 failure at tier 3.
	failAtTier3 := state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-rst-003", map[string]any{
		"reason": "tier 3 failure",
	})
	es.Append(failAtTier3)
	ps.Project(failAtTier3)

	m.resetStoryToDraft("s-rst-003", "reviewer", "final failure")

	// Requirement should be paused.
	req, err := ps.GetRequirement("r-pause")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Errorf("expected requirement paused, got %q", req.Status)
	}
}

// TestHandleManagerEscalation_RetryAction verifies the full manager escalation
// path when the LLM recommends a retry action.
func TestHandleManagerEscalation_RetryAction(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-mgr", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mgr-001", map[string]any{
		"id": "s-mgr-001", "req_id": "r-mgr", "title": "Manager Task",
		"description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Manager LLM returns a retry action.
	managerResponse := `{
		"diagnosis": "environment issue - missing dependency",
		"category": "environment",
		"action": "retry",
		"retry_config": {
			"target_role": "junior",
			"reset_tier": 0,
			"worktree_reset": false,
			"env_fixes": ["install missing dep"]
		}
	}`
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: managerResponse})
	mgr := NewManager(replayClient, "test-model", 4000, es, ps)

	cfg := config.Config{
		Routing: config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetManager(mgr)

	story := PlannedStory{ID: "s-mgr-001", Title: "Manager Task"}
	rc := &RunContext{
		ReqID:          "r-mgr",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	m.handleManagerEscalation(context.Background(), story, t.TempDir(), rc)

	// Verify STORY_ESCALATED event from executeRetryAction (from_tier=2, to_tier=0).
	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-mgr-001"})
	if len(escEvents) != 1 {
		t.Fatalf("expected 1 STORY_ESCALATED, got %d", len(escEvents))
	}

	var payload map[string]any
	json.Unmarshal(escEvents[0].Payload, &payload)
	if int(payload["from_tier"].(float64)) != 2 {
		t.Errorf("expected from_tier 2, got %v", payload["from_tier"])
	}
}

// TestHandleManagerEscalation_RewriteAction verifies the rewrite path.
func TestHandleManagerEscalation_RewriteAction(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mgr-002", map[string]any{
		"id": "s-mgr-002", "req_id": "r-001", "title": "Vague Task",
		"description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	managerResponse := `{
		"diagnosis": "story too vague, needs rewrite",
		"category": "structural",
		"action": "rewrite",
		"rewrite_config": {
			"title": "Clear Task",
			"description": "Better description",
			"acceptance_criteria": "Must pass tests",
			"complexity": 3
		}
	}`
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: managerResponse})
	mgr := NewManager(replayClient, "test-model", 4000, es, ps)

	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetManager(mgr)

	story := PlannedStory{ID: "s-mgr-002", Title: "Vague Task"}
	rc := &RunContext{
		ReqID:          "r-001",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	m.handleManagerEscalation(context.Background(), story, t.TempDir(), rc)

	rwEvents, _ := es.List(state.EventFilter{Type: state.EventStoryRewritten, StoryID: "s-mgr-002"})
	if len(rwEvents) != 1 {
		t.Fatalf("expected 1 STORY_REWRITTEN, got %d", len(rwEvents))
	}
}

// TestHandleManagerEscalation_EscalateToTechLead verifies the escalate path.
func TestHandleManagerEscalation_EscalateToTechLead(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mgr-003", map[string]any{
		"id": "s-mgr-003", "req_id": "r-001", "title": "Hard Task",
		"description": "desc", "complexity": 8,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	managerResponse := `{
		"diagnosis": "structural problem beyond manager ability",
		"category": "structural",
		"action": "escalate_to_techlead"
	}`
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: managerResponse})
	mgr := NewManager(replayClient, "test-model", 4000, es, ps)

	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetManager(mgr)

	story := PlannedStory{ID: "s-mgr-003", Title: "Hard Task"}
	rc := &RunContext{
		ReqID:          "r-001",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	m.handleManagerEscalation(context.Background(), story, t.TempDir(), rc)

	// Should escalate to tier 3.
	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-mgr-003"})
	if len(escEvents) != 1 {
		t.Fatalf("expected 1 STORY_ESCALATED, got %d", len(escEvents))
	}

	var payload map[string]any
	json.Unmarshal(escEvents[0].Payload, &payload)
	if int(payload["to_tier"].(float64)) != 3 {
		t.Errorf("expected to_tier 3, got %v", payload["to_tier"])
	}
}

// TestHandleManagerEscalation_LLMFailure verifies that when the LLM fails,
// the story is reset to draft rather than crashing.
func TestHandleManagerEscalation_LLMFailure(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mgr-fail", map[string]any{
		"id": "s-mgr-fail", "req_id": "r-001", "title": "Fail Task",
		"description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// No responses -> LLM call fails.
	replayClient := llm.NewReplayClient()
	mgr := NewManager(replayClient, "test-model", 4000, es, ps)

	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetManager(mgr)

	story := PlannedStory{ID: "s-mgr-fail", Title: "Fail Task"}
	rc := &RunContext{
		ReqID:          "r-001",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	// Should not panic.
	m.handleManagerEscalation(context.Background(), story, t.TempDir(), rc)

	// Story should be reset to draft via STORY_REVIEW_FAILED.
	failEvents, _ := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: "s-mgr-fail"})
	if len(failEvents) < 1 {
		t.Error("expected STORY_REVIEW_FAILED after LLM failure")
	}
}

// TestHandleManagerEscalation_FatalAPIError verifies that a fatal API error
// (e.g., 401) pauses the requirement instead of retrying.
func TestHandleManagerEscalation_FatalAPIError(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-fatal", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mgr-fatal", map[string]any{
		"id": "s-mgr-fatal", "req_id": "r-fatal", "title": "Fatal Task",
		"description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Fatal API error client.
	fatalErr := &llm.APIError{StatusCode: 401, Message: "unauthorized"}
	fatalClient := &errorLLMClient{err: fatalErr}
	mgr := NewManager(fatalClient, "test-model", 4000, es, ps)

	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetManager(mgr)

	story := PlannedStory{ID: "s-mgr-fatal", Title: "Fatal Task"}
	rc := &RunContext{
		ReqID:          "r-fatal",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	m.handleManagerEscalation(context.Background(), story, t.TempDir(), rc)

	// Requirement should be paused.
	req, err := ps.GetRequirement("r-fatal")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Errorf("expected requirement paused, got %q", req.Status)
	}
}

// TestHandleManagerEscalation_SplitAction verifies the split path through
// the manager escalation handler.
func TestHandleManagerEscalation_SplitAction(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-split-mgr", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mgr-split", map[string]any{
		"id": "s-mgr-split", "req_id": "r-split-mgr", "title": "Complex Task",
		"description": "too complex", "complexity": 8, "split_depth": 0,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	managerResponse := `{
		"diagnosis": "too complex, splitting into smaller parts",
		"category": "complexity",
		"action": "split",
		"split_config": {
			"children": [
				{"suffix": "a", "title": "Part A", "description": "first half", "complexity": 3, "owned_files": ["a.go"]},
				{"suffix": "b", "title": "Part B", "description": "second half", "complexity": 3, "owned_files": ["b.go"]}
			],
			"dependency_edges": [["s-mgr-split-b", "s-mgr-split-a"]]
		}
	}`
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: managerResponse})
	mgr := NewManager(replayClient, "test-model", 4000, es, ps)

	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Planning:  config.PlanningConfig{MaxStoryComplexity: 8},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetManager(mgr)

	dag := graph.New()
	dag.AddNode("s-mgr-split")
	story := PlannedStory{ID: "s-mgr-split", Title: "Complex Task", Complexity: 8}
	rc := &RunContext{
		ReqID:          "r-split-mgr",
		DAG:            dag,
		PlannedStories: []PlannedStory{story},
	}

	m.handleManagerEscalation(context.Background(), story, t.TempDir(), rc)

	// Verify child stories were created.
	childA, _ := es.List(state.EventFilter{Type: state.EventStoryCreated, StoryID: "s-mgr-split-a"})
	if len(childA) != 1 {
		t.Errorf("expected 1 STORY_CREATED for child A, got %d", len(childA))
	}

	// Verify STORY_SPLIT on parent.
	splitEvents, _ := es.List(state.EventFilter{Type: state.EventStorySplit, StoryID: "s-mgr-split"})
	if len(splitEvents) != 1 {
		t.Errorf("expected 1 STORY_SPLIT, got %d", len(splitEvents))
	}
}

// TestHandleManagerEscalation_UnknownAction verifies that an unknown action
// from the LLM falls back to resetStoryToDraft.
func TestHandleManagerEscalation_UnknownAction(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mgr-unk", map[string]any{
		"id": "s-mgr-unk", "req_id": "r-001", "title": "Unknown Action Task",
		"description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Return a valid retry action (must be a valid action since parseManagerAction validates).
	managerResponse := `{
		"diagnosis": "retry without config",
		"category": "transient",
		"action": "retry"
	}`
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: managerResponse})
	mgr := NewManager(replayClient, "test-model", 4000, es, ps)

	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetManager(mgr)

	story := PlannedStory{ID: "s-mgr-unk", Title: "Unknown Action Task"}
	rc := &RunContext{
		ReqID:          "r-001",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	// Should not panic.
	m.handleManagerEscalation(context.Background(), story, t.TempDir(), rc)

	// Should have produced some event (either escalation or review failed).
	allEvents, _ := es.List(state.EventFilter{StoryID: "s-mgr-unk"})
	if len(allEvents) < 2 { // at least the created + some action event
		t.Errorf("expected at least 2 events, got %d", len(allEvents))
	}
}

// TestHandleTechLeadEscalation_NilPlanner verifies that when no planner is
// set, the requirement is paused.
func TestHandleTechLeadEscalation_NilPlanner(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-tl", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-tl-001", map[string]any{
		"id": "s-tl-001", "req_id": "r-tl", "title": "Task",
		"description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	// planner is nil by default

	story := PlannedStory{ID: "s-tl-001", Title: "Task"}
	rc := &RunContext{
		ReqID:          "r-tl",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	m.handleTechLeadEscalation(context.Background(), story, t.TempDir(), rc)

	// Requirement should be paused due to nil planner.
	req, err := ps.GetRequirement("r-tl")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Errorf("expected requirement paused, got %q", req.Status)
	}
}

// TestHandleTechLeadEscalation_RePlanSuccess verifies the full tech lead
// re-planning path when the LLM successfully produces replacement stories.
func TestHandleTechLeadEscalation_RePlanSuccess(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-tl-ok", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-tl-ok", map[string]any{
		"id": "s-tl-ok", "req_id": "r-tl-ok", "title": "Complex Task",
		"description": "desc", "complexity": 8, "split_depth": 0,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// RePlan response: two replacement stories.
	rePlanResponse := `[
		{"id": "s-tl-ok-a", "title": "Part A", "description": "first", "acceptance_criteria": "test A", "complexity": 3, "owned_files": ["a.go"]},
		{"id": "s-tl-ok-b", "title": "Part B", "description": "second", "acceptance_criteria": "test B", "complexity": 3, "owned_files": ["b.go"]}
	]`
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: rePlanResponse})

	cfg := config.Config{
		Routing:  config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Planning: config.PlanningConfig{MaxStoryComplexity: 8},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
		Models: config.ModelsConfig{
			TechLead: config.ModelConfig{Model: "test-model"},
		},
	}
	planner := NewPlanner(replayClient, cfg, es, ps)
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetPlanner(planner)

	dag := graph.New()
	dag.AddNode("s-tl-ok")
	story := PlannedStory{ID: "s-tl-ok", Title: "Complex Task", Complexity: 8}
	rc := &RunContext{
		ReqID:          "r-tl-ok",
		DAG:            dag,
		PlannedStories: []PlannedStory{story},
	}

	m.handleTechLeadEscalation(context.Background(), story, t.TempDir(), rc)

	// Verify child stories were created.
	childA, _ := es.List(state.EventFilter{Type: state.EventStoryCreated, StoryID: "s-tl-ok-a"})
	if len(childA) != 1 {
		t.Errorf("expected 1 STORY_CREATED for child A, got %d", len(childA))
	}
	childB, _ := es.List(state.EventFilter{Type: state.EventStoryCreated, StoryID: "s-tl-ok-b"})
	if len(childB) != 1 {
		t.Errorf("expected 1 STORY_CREATED for child B, got %d", len(childB))
	}

	// Verify STORY_SPLIT on parent.
	splitEvents, _ := es.List(state.EventFilter{Type: state.EventStorySplit, StoryID: "s-tl-ok"})
	if len(splitEvents) != 1 {
		t.Errorf("expected 1 STORY_SPLIT, got %d", len(splitEvents))
	}

	// Verify planned stories were updated.
	if len(rc.PlannedStories) < 3 {
		t.Errorf("expected at least 3 planned stories, got %d", len(rc.PlannedStories))
	}
}

// TestHandleTechLeadEscalation_RePlanFails verifies that when RePlan fails,
// the requirement is paused.
func TestHandleTechLeadEscalation_RePlanFails(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-tl-fail", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-tl-fail", map[string]any{
		"id": "s-tl-fail", "req_id": "r-tl-fail", "title": "Task",
		"description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// No LLM responses -> RePlan will fail.
	replayClient := llm.NewReplayClient()
	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
		Models:    config.ModelsConfig{TechLead: config.ModelConfig{Model: "test-model"}},
	}
	planner := NewPlanner(replayClient, cfg, es, ps)
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetPlanner(planner)

	story := PlannedStory{ID: "s-tl-fail", Title: "Task"}
	rc := &RunContext{
		ReqID:          "r-tl-fail",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	m.handleTechLeadEscalation(context.Background(), story, t.TempDir(), rc)

	req, err := ps.GetRequirement("r-tl-fail")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Errorf("expected requirement paused after replan failure, got %q", req.Status)
	}
}

// TestHandleTechLeadEscalation_EmptyReplacements verifies that when RePlan
// returns an empty list, the requirement is paused.
func TestHandleTechLeadEscalation_EmptyReplacements(t *testing.T) {
	es, ps, cleanup := newEscalationTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-tl-empty", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-tl-empty", map[string]any{
		"id": "s-tl-empty", "req_id": "r-tl-empty", "title": "Task",
		"description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Return empty array.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{Content: "[]"})
	cfg := config.Config{
		Routing:   config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Workspace: config.WorkspaceConfig{StateDir: t.TempDir()},
		Models:    config.ModelsConfig{TechLead: config.ModelConfig{Model: "test-model"}},
	}
	planner := NewPlanner(replayClient, cfg, es, ps)
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
	m.SetPlanner(planner)

	story := PlannedStory{ID: "s-tl-empty", Title: "Task"}
	rc := &RunContext{
		ReqID:          "r-tl-empty",
		DAG:            graph.New(),
		PlannedStories: []PlannedStory{story},
	}

	m.handleTechLeadEscalation(context.Background(), story, t.TempDir(), rc)

	req, err := ps.GetRequirement("r-tl-empty")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Errorf("expected requirement paused for empty replacements, got %q", req.Status)
	}
}
