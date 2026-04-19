package state_test

import (
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestSQLiteStore_Project_UnknownEventReturnsError(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	err = db.Project(state.NewEvent(state.EventType("UNKNOWN_EVENT"), "system", "", map[string]any{
		"id": "unknown-1",
	}))
	if err == nil {
		t.Fatal("expected error for unknown event type")
	}
	if !strings.Contains(err.Error(), "unhandled event type") {
		t.Fatalf("error = %q, want unhandled event type", err.Error())
	}
}

func TestSQLiteStore_Project_KnownObservationalEventsRemainNoOps(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	events := []state.Event{
		state.NewEvent(state.EventReqEstimated, "estimator", "", map[string]any{"id": "req-1"}),
		state.NewEvent(state.EventSupervisorCheck, "supervisor", "", nil),
		state.NewEvent(state.EventSupervisorReprioritize, "supervisor", "", nil),
		state.NewEvent(state.EventSupervisorDriftDetected, "supervisor", "", nil),
		state.NewEvent(state.EventWorktreePruned, "system", "", nil),
		state.NewEvent(state.EventBranchDeleted, "system", "", nil),
		state.NewEvent(state.EventGCCompleted, "reaper", "", nil),
		state.NewEvent(state.EventReviewModeSet, "system", "", nil),
		state.NewEvent(state.EventPlanApproved, "human", "", nil),
		state.NewEvent(state.EventPlanRejected, "human", "", nil),
		state.NewEvent(state.EventStoryApproved, "human", "story-1", nil),
		state.NewEvent(state.EventRecoveryCompleted, "system", "", nil),
	}

	for _, evt := range events {
		if err := db.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evt.Type, err)
		}
	}
}
