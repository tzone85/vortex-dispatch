package state_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestNewEvent(t *testing.T) {
	evt := state.NewEvent(state.EventStoryCreated, "agent-1", "story-1", map[string]any{
		"title":      "Add auth",
		"complexity": 5,
	})

	if evt.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if evt.Type != state.EventStoryCreated {
		t.Fatalf("expected type %s, got %s", state.EventStoryCreated, evt.Type)
	}
	if evt.AgentID != "agent-1" {
		t.Fatalf("expected agent-1, got %s", evt.AgentID)
	}
	if evt.StoryID != "story-1" {
		t.Fatalf("expected story-1, got %s", evt.StoryID)
	}
	if time.Since(evt.Timestamp) > time.Second {
		t.Fatal("timestamp should be recent")
	}

	var payload map[string]any
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload["title"] != "Add auth" {
		t.Fatalf("expected title 'Add auth', got %v", payload["title"])
	}
}

func TestEventTypeConstants(t *testing.T) {
	types := []state.EventType{
		state.EventReqSubmitted,
		state.EventReqAnalyzed,
		state.EventReqPlanned,
		state.EventReqCompleted,
		state.EventStoryCreated,
		state.EventStoryEstimated,
		state.EventStoryAssigned,
		state.EventStoryStarted,
		state.EventStoryProgress,
		state.EventStoryCompleted,
		state.EventStoryReviewRequested,
		state.EventStoryReviewPassed,
		state.EventStoryReviewFailed,
		state.EventStoryQAStarted,
		state.EventStoryQAPassed,
		state.EventStoryQAFailed,
		state.EventStoryPRCreated,
		state.EventStoryMerged,
		state.EventAgentSpawned,
		state.EventAgentCheckpoint,
		state.EventAgentResumed,
		state.EventAgentStuck,
		state.EventAgentTerminated,
		state.EventSupervisorCheck,
		state.EventSupervisorReprioritize,
		state.EventSupervisorDriftDetected,
		state.EventWorktreePruned,
		state.EventBranchDeleted,
		state.EventGCCompleted,
	}
	seen := make(map[state.EventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Fatalf("duplicate event type: %s", et)
		}
		seen[et] = true
		if et == "" {
			t.Fatal("empty event type")
		}
	}
}

func TestNewEvent_NilPayload(t *testing.T) {
	evt := state.NewEvent(state.EventReqSubmitted, "system", "", nil)
	if evt.Payload != nil {
		t.Fatal("expected nil payload")
	}
}

func TestEscalationEventTypes(t *testing.T) {
	types := []state.EventType{
		state.EventStoryEscalated,
		state.EventStoryRewritten,
		state.EventStorySplit,
	}
	for _, et := range types {
		if et == "" {
			t.Errorf("event type should not be empty")
		}
	}
}

func TestEventFilterAfter(t *testing.T) {
	dir := t.TempDir()
	fs, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	defer fs.Close()

	e1 := state.NewEvent(state.EventStoryReviewFailed, "agent1", "s-001", nil)
	fs.Append(e1)
	time.Sleep(10 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(10 * time.Millisecond)
	e2 := state.NewEvent(state.EventStoryReviewFailed, "agent2", "s-001", nil)
	fs.Append(e2)

	count, err := fs.Count(state.EventFilter{
		Type:    state.EventStoryReviewFailed,
		StoryID: "s-001",
		After:   cutoff,
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 event after cutoff, got %d", count)
	}
}
