package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// newActionTestStores creates fresh event and projection stores for action tests.
func newActionTestStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
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
	cleanup := func() {
		es.Close()
		ps.Close()
	}
	return es, ps, cleanup
}

// newTestMonitor creates a Monitor wired to the given stores with default config.
func newTestMonitor(t *testing.T, es state.EventStore, ps state.ProjectionStore) *Monitor {
	t.Helper()
	cfg := config.Config{
		Routing: config.RoutingConfig{
			MaxRetriesBeforeEscalation: 2,
			MaxSeniorRetries:           2,
			MaxManagerAttempts:          1,
		},
		Planning: config.PlanningConfig{
			MaxStoryComplexity: 8,
		},
	}
	return NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)
}

// --- executeRetryAction tests ---

func TestExecuteRetryAction_WithWorktreeReset(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	// Create the story first so projection works.
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-retry-001", map[string]any{
		"id": "s-retry-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	// Create a temp directory to simulate a worktree that gets removed.
	worktreeDir := t.TempDir()

	action := ManagerAction{
		Diagnosis: "environment issue, clean start needed",
		Category:  "environment",
		Action:    "retry",
		RetryConfig: &RetryConfig{
			TargetRole:    "junior",
			ResetTier:     0,
			WorktreeReset: true,
			EnvFixes:      []string{"fix env"},
		},
	}

	m.executeRetryAction("s-retry-001", action, worktreeDir)

	// Verify STORY_ESCALATED event (from tier 2 -> reset tier 0).
	escEvents, err := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-retry-001"})
	if err != nil {
		t.Fatalf("list escalation events: %v", err)
	}
	if len(escEvents) != 1 {
		t.Fatalf("expected 1 STORY_ESCALATED event, got %d", len(escEvents))
	}

	var escPayload map[string]any
	if err := json.Unmarshal(escEvents[0].Payload, &escPayload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if int(escPayload["from_tier"].(float64)) != 2 {
		t.Errorf("expected from_tier 2, got %v", escPayload["from_tier"])
	}
	if int(escPayload["to_tier"].(float64)) != 0 {
		t.Errorf("expected to_tier 0, got %v", escPayload["to_tier"])
	}
	if reason, ok := escPayload["reason"].(string); !ok || reason == "" {
		t.Error("expected non-empty reason in escalation payload")
	}

	// Verify STORY_REVIEW_FAILED event (reset for re-dispatch).
	reviewEvents, err := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: "s-retry-001"})
	if err != nil {
		t.Fatalf("list review events: %v", err)
	}
	if len(reviewEvents) != 1 {
		t.Fatalf("expected 1 STORY_REVIEW_FAILED event, got %d", len(reviewEvents))
	}
}

func TestExecuteRetryAction_WithoutWorktreeReset(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-retry-002", map[string]any{
		"id": "s-retry-002", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis: "transient network issue",
		Category:  "transient",
		Action:    "retry",
		RetryConfig: &RetryConfig{
			TargetRole:    "intermediate",
			ResetTier:     1,
			WorktreeReset: false,
		},
	}

	m.executeRetryAction("s-retry-002", action, "/nonexistent/path")

	escEvents, err := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-retry-002"})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(escEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(escEvents))
	}

	var payload map[string]any
	json.Unmarshal(escEvents[0].Payload, &payload)
	if int(payload["to_tier"].(float64)) != 1 {
		t.Errorf("expected reset to tier 1, got %v", payload["to_tier"])
	}
}

func TestExecuteRetryAction_NilRetryConfig(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-retry-003", map[string]any{
		"id": "s-retry-003", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis:   "retry without config",
		Action:      "retry",
		RetryConfig: nil,
	}

	m.executeRetryAction("s-retry-003", action, "/tmp")

	// Should default to resetTier=0, no worktree removal.
	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-retry-003"})
	if len(escEvents) != 1 {
		t.Fatalf("expected 1 STORY_ESCALATED, got %d", len(escEvents))
	}
	var payload map[string]any
	json.Unmarshal(escEvents[0].Payload, &payload)
	if int(payload["to_tier"].(float64)) != 0 {
		t.Errorf("expected default reset tier 0 with nil config, got %v", payload["to_tier"])
	}
}

// --- executeRewriteAction tests ---

func TestExecuteRewriteAction_FullRewrite(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-rw-001", map[string]any{
		"id": "s-rw-001", "req_id": "r-001", "title": "Original", "description": "old desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis: "story too vague, rewriting",
		Action:    "rewrite",
		RewriteConfig: &RewriteConfig{
			Title:              "Rewritten Title",
			Description:        "Better description",
			AcceptanceCriteria: "Must pass all tests",
			Complexity:         3,
		},
	}

	m.executeRewriteAction("s-rw-001", action)

	// Verify STORY_REWRITTEN event.
	rwEvents, err := es.List(state.EventFilter{Type: state.EventStoryRewritten, StoryID: "s-rw-001"})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(rwEvents) != 1 {
		t.Fatalf("expected 1 STORY_REWRITTEN event, got %d", len(rwEvents))
	}

	var payload map[string]any
	json.Unmarshal(rwEvents[0].Payload, &payload)

	changes, ok := payload["changes"].(map[string]any)
	if !ok {
		t.Fatal("expected changes map in payload")
	}
	if changes["title"] != "Rewritten Title" {
		t.Errorf("expected rewritten title, got %v", changes["title"])
	}
	if changes["description"] != "Better description" {
		t.Errorf("expected rewritten description, got %v", changes["description"])
	}
	if changes["acceptance_criteria"] != "Must pass all tests" {
		t.Errorf("expected rewritten AC, got %v", changes["acceptance_criteria"])
	}
	if int(changes["complexity"].(float64)) != 3 {
		t.Errorf("expected complexity 3, got %v", changes["complexity"])
	}
	if payload["reason"] != "story too vague, rewriting" {
		t.Errorf("expected diagnosis as reason, got %v", payload["reason"])
	}
}

func TestExecuteRewriteAction_PartialRewrite(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-rw-002", map[string]any{
		"id": "s-rw-002", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	// Only rewrite description, leave other fields empty/zero.
	action := ManagerAction{
		Diagnosis: "clarify description",
		Action:    "rewrite",
		RewriteConfig: &RewriteConfig{
			Description: "Clarified description",
		},
	}

	m.executeRewriteAction("s-rw-002", action)

	rwEvents, _ := es.List(state.EventFilter{Type: state.EventStoryRewritten, StoryID: "s-rw-002"})
	if len(rwEvents) != 1 {
		t.Fatalf("expected 1 STORY_REWRITTEN event, got %d", len(rwEvents))
	}

	var payload map[string]any
	json.Unmarshal(rwEvents[0].Payload, &payload)
	changes := payload["changes"].(map[string]any)

	// Only description should be in changes, not title/AC/complexity.
	if changes["description"] != "Clarified description" {
		t.Errorf("expected description in changes")
	}
	if _, ok := changes["title"]; ok {
		t.Error("empty title should not be in changes")
	}
	if _, ok := changes["complexity"]; ok {
		t.Error("zero complexity should not be in changes")
	}
}

func TestExecuteRewriteAction_NilConfig(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-rw-003", map[string]any{
		"id": "s-rw-003", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis:     "rewrite with no config",
		Action:        "rewrite",
		RewriteConfig: nil,
	}

	m.executeRewriteAction("s-rw-003", action)

	// Should fall back to resetStoryToDraft, emitting STORY_REVIEW_FAILED.
	rwEvents, _ := es.List(state.EventFilter{Type: state.EventStoryRewritten, StoryID: "s-rw-003"})
	if len(rwEvents) != 0 {
		t.Errorf("expected 0 STORY_REWRITTEN events with nil config, got %d", len(rwEvents))
	}

	// Should have emitted a review failed event via resetStoryToDraft.
	failEvents, _ := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: "s-rw-003"})
	if len(failEvents) < 1 {
		t.Error("expected at least 1 STORY_REVIEW_FAILED event from resetStoryToDraft")
	}
}

// --- executeSplitAction tests ---

func TestExecuteSplitAction_ValidSplit(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-split", "title": "Split test req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-sp-001", map[string]any{
		"id": "s-sp-001", "req_id": "r-split", "title": "Complex Task",
		"description": "desc", "complexity": 8, "split_depth": 0,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	dag := graph.New()
	dag.AddNode("s-sp-001")

	rc := &RunContext{
		ReqID: "r-split",
		DAG:   dag,
		PlannedStories: []PlannedStory{
			{ID: "s-sp-001", Title: "Complex Task", Complexity: 8},
		},
	}

	action := ManagerAction{
		Diagnosis: "too complex, splitting",
		Action:    "split",
		SplitConfig: &SplitConfig{
			Children: []SplitChildConfig{
				{Suffix: "a", Title: "Part A", Description: "first part", Complexity: 3, OwnedFiles: []string{"a.go"}},
				{Suffix: "b", Title: "Part B", Description: "second part", Complexity: 3, OwnedFiles: []string{"b.go"}},
			},
			DependencyEdges: [][]string{{"s-sp-001-b", "s-sp-001-a"}},
		},
	}

	story := PlannedStory{ID: "s-sp-001", Title: "Complex Task", Complexity: 8}
	m.executeSplitAction(context.Background(), "s-sp-001", action, rc, story)

	// Verify child STORY_CREATED events.
	childAEvents, _ := es.List(state.EventFilter{Type: state.EventStoryCreated, StoryID: "s-sp-001-a"})
	if len(childAEvents) != 1 {
		t.Fatalf("expected 1 STORY_CREATED for child A, got %d", len(childAEvents))
	}
	childBEvents, _ := es.List(state.EventFilter{Type: state.EventStoryCreated, StoryID: "s-sp-001-b"})
	if len(childBEvents) != 1 {
		t.Fatalf("expected 1 STORY_CREATED for child B, got %d", len(childBEvents))
	}

	// Verify STORY_SPLIT event on parent.
	splitEvents, _ := es.List(state.EventFilter{Type: state.EventStorySplit, StoryID: "s-sp-001"})
	if len(splitEvents) != 1 {
		t.Fatalf("expected 1 STORY_SPLIT for parent, got %d", len(splitEvents))
	}

	var splitPayload map[string]any
	json.Unmarshal(splitEvents[0].Payload, &splitPayload)
	childIDs, ok := splitPayload["child_story_ids"].([]any)
	if !ok || len(childIDs) != 2 {
		t.Fatalf("expected 2 child IDs, got %v", splitPayload["child_story_ids"])
	}

	// Verify DAG was mutated (children should exist as nodes).
	allReady := dag.ReadyNodes(map[string]bool{})
	found := map[string]bool{}
	for _, id := range allReady {
		found[id] = true
	}
	if !found["s-sp-001-a"] {
		t.Error("expected child A in DAG ready nodes")
	}

	// Verify planned stories were updated.
	if len(rc.PlannedStories) < 3 {
		t.Errorf("expected at least 3 planned stories (parent + 2 children), got %d", len(rc.PlannedStories))
	}
}

func TestExecuteSplitAction_NilSplitConfig(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-sp-002", map[string]any{
		"id": "s-sp-002", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis:   "split with no config",
		Action:      "split",
		SplitConfig: nil,
	}

	rc := &RunContext{ReqID: "r-001", DAG: graph.New(), PlannedStories: nil}
	story := PlannedStory{ID: "s-sp-002"}

	m.executeSplitAction(context.Background(), "s-sp-002", action, rc, story)

	// Should fall back to resetStoryToDraft.
	splitEvents, _ := es.List(state.EventFilter{Type: state.EventStorySplit, StoryID: "s-sp-002"})
	if len(splitEvents) != 0 {
		t.Errorf("expected 0 STORY_SPLIT events with nil config, got %d", len(splitEvents))
	}

	failEvents, _ := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: "s-sp-002"})
	if len(failEvents) < 1 {
		t.Error("expected STORY_REVIEW_FAILED from resetStoryToDraft")
	}
}

func TestExecuteSplitAction_EmptyChildren(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-sp-003", map[string]any{
		"id": "s-sp-003", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis:   "split with empty children",
		Action:      "split",
		SplitConfig: &SplitConfig{Children: []SplitChildConfig{}},
	}

	rc := &RunContext{ReqID: "r-001", DAG: graph.New()}
	story := PlannedStory{ID: "s-sp-003"}

	m.executeSplitAction(context.Background(), "s-sp-003", action, rc, story)

	splitEvents, _ := es.List(state.EventFilter{Type: state.EventStorySplit, StoryID: "s-sp-003"})
	if len(splitEvents) != 0 {
		t.Errorf("expected 0 STORY_SPLIT with empty children, got %d", len(splitEvents))
	}
}

func TestExecuteSplitAction_ValidationFailure_OverlappingFiles(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-overlap", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-sp-004", map[string]any{
		"id": "s-sp-004", "req_id": "r-overlap", "title": "Task",
		"description": "desc", "complexity": 5, "split_depth": 0,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis: "splitting with overlap",
		Action:    "split",
		SplitConfig: &SplitConfig{
			Children: []SplitChildConfig{
				{Suffix: "a", Title: "Part A", Complexity: 2, OwnedFiles: []string{"shared.go"}},
				{Suffix: "b", Title: "Part B", Complexity: 2, OwnedFiles: []string{"shared.go"}},
			},
		},
	}

	rc := &RunContext{ReqID: "r-overlap", DAG: graph.New(), PlannedStories: nil}
	story := PlannedStory{ID: "s-sp-004"}

	m.executeSplitAction(context.Background(), "s-sp-004", action, rc, story)

	// Should fail validation and fall back to resetStoryToDraft.
	splitEvents, _ := es.List(state.EventFilter{Type: state.EventStorySplit, StoryID: "s-sp-004"})
	if len(splitEvents) != 0 {
		t.Errorf("expected 0 STORY_SPLIT events due to validation failure, got %d", len(splitEvents))
	}

	failEvents, _ := es.List(state.EventFilter{Type: state.EventStoryReviewFailed, StoryID: "s-sp-004"})
	if len(failEvents) < 1 {
		t.Error("expected STORY_REVIEW_FAILED from resetStoryToDraft after validation failure")
	}
}

func TestExecuteSplitAction_StoryNotFound(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	// No story created in projection store.
	m := newTestMonitor(t, es, ps)

	action := ManagerAction{
		Diagnosis: "splitting nonexistent",
		Action:    "split",
		SplitConfig: &SplitConfig{
			Children: []SplitChildConfig{
				{Suffix: "a", Title: "Part A", Complexity: 2},
			},
		},
	}

	rc := &RunContext{ReqID: "r-001", DAG: graph.New()}
	story := PlannedStory{ID: "s-sp-missing"}

	// Should not panic, should fall back to resetStoryToDraft.
	m.executeSplitAction(context.Background(), "s-sp-missing", action, rc, story)

	splitEvents, _ := es.List(state.EventFilter{Type: state.EventStorySplit, StoryID: "s-sp-missing"})
	if len(splitEvents) != 0 {
		t.Errorf("expected 0 STORY_SPLIT for missing story, got %d", len(splitEvents))
	}
}

// --- escalateToTier tests ---

func TestEscalateToTier_Tier3(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-esc-001", map[string]any{
		"id": "s-esc-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	m.escalateToTier("s-esc-001", 3, "manager escalated to tech lead")

	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-esc-001"})
	if len(escEvents) != 1 {
		t.Fatalf("expected 1 STORY_ESCALATED, got %d", len(escEvents))
	}

	var payload map[string]any
	json.Unmarshal(escEvents[0].Payload, &payload)

	if int(payload["from_tier"].(float64)) != 0 {
		t.Errorf("expected from_tier 0 (no prior escalation), got %v", payload["from_tier"])
	}
	if int(payload["to_tier"].(float64)) != 3 {
		t.Errorf("expected to_tier 3, got %v", payload["to_tier"])
	}
	if payload["reason"] != "manager escalated to tech lead" {
		t.Errorf("expected reason, got %v", payload["reason"])
	}
	if escEvents[0].AgentID != "monitor" {
		t.Errorf("expected agent_id 'monitor', got %q", escEvents[0].AgentID)
	}
}

func TestEscalateToTier_WithPriorEscalation(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-esc-002", map[string]any{
		"id": "s-esc-002", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Pre-seed a prior escalation to tier 1.
	priorEsc := state.NewEvent(state.EventStoryEscalated, "reviewer", "s-esc-002", map[string]any{
		"from_tier": 0, "to_tier": 1, "reason": "first escalation",
	})
	es.Append(priorEsc)
	ps.Project(priorEsc)

	m := newTestMonitor(t, es, ps)

	m.escalateToTier("s-esc-002", 2, "senior also failed")

	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-esc-002"})
	if len(escEvents) != 2 {
		t.Fatalf("expected 2 total STORY_ESCALATED events, got %d", len(escEvents))
	}

	// The new escalation should record from_tier=1 (the current tier).
	var payload map[string]any
	json.Unmarshal(escEvents[1].Payload, &payload)
	if int(payload["from_tier"].(float64)) != 1 {
		t.Errorf("expected from_tier 1, got %v", payload["from_tier"])
	}
	if int(payload["to_tier"].(float64)) != 2 {
		t.Errorf("expected to_tier 2, got %v", payload["to_tier"])
	}
}

func TestEscalateToTier_MultipleTiers(t *testing.T) {
	es, ps, cleanup := newActionTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-esc-003", map[string]any{
		"id": "s-esc-003", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 5,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := newTestMonitor(t, es, ps)

	// Escalate through tiers 1, 2, 3.
	for _, tier := range []int{1, 2, 3} {
		m.escalateToTier("s-esc-003", tier, "escalate")
	}

	escEvents, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-esc-003"})
	if len(escEvents) != 3 {
		t.Fatalf("expected 3 STORY_ESCALATED events, got %d", len(escEvents))
	}

	// Verify the last escalation records from_tier=2 (current) to_tier=3.
	var last map[string]any
	json.Unmarshal(escEvents[2].Payload, &last)
	if int(last["from_tier"].(float64)) != 2 {
		t.Errorf("expected from_tier 2 on third escalation, got %v", last["from_tier"])
	}
	if int(last["to_tier"].(float64)) != 3 {
		t.Errorf("expected to_tier 3 on third escalation, got %v", last["to_tier"])
	}
}
