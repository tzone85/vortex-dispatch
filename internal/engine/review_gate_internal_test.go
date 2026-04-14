package engine

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newRGStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
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

func TestPendingApprovals_NoPending(t *testing.T) {
	es, ps, cleanup := newRGStores(t)
	defer cleanup()

	// Create a requirement and story
	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "Task", "description": "d", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	gate := NewReviewGate(es)
	pending, err := gate.PendingApprovals("r-001", ps)
	if err != nil {
		t.Fatalf("PendingApprovals: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestPendingApprovals_WithPending(t *testing.T) {
	es, ps, cleanup := newRGStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "Task", "description": "d", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Mark as awaiting approval
	awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "monitor", "r001-s1", map[string]any{
		"story_id": "r001-s1",
	})
	es.Append(awaitEvt)
	ps.Project(awaitEvt)

	gate := NewReviewGate(es)
	pending, err := gate.PendingApprovals("r-001", ps)
	if err != nil {
		t.Fatalf("PendingApprovals: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].ID != "r001-s1" {
		t.Errorf("expected story r001-s1, got %s", pending[0].ID)
	}
}

func TestPendingApprovals_MixedStatuses(t *testing.T) {
	es, ps, cleanup := newRGStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	// Story 1: awaiting approval
	s1 := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "S1", "description": "d", "complexity": 1,
	})
	es.Append(s1)
	ps.Project(s1)
	aw1 := state.NewEvent(state.EventStoryAwaitingApproval, "monitor", "r001-s1", map[string]any{
		"story_id": "r001-s1",
	})
	es.Append(aw1)
	ps.Project(aw1)

	// Story 2: merged (not awaiting)
	s2 := state.NewEvent(state.EventStoryCreated, "tl", "r001-s2", map[string]any{
		"id": "r001-s2", "req_id": "r-001", "title": "S2", "description": "d", "complexity": 1,
	})
	es.Append(s2)
	ps.Project(s2)
	m2 := state.NewEvent(state.EventStoryMerged, "merger", "r001-s2", map[string]any{"pr_url": "u"})
	es.Append(m2)
	ps.Project(m2)

	// Story 3: also awaiting approval
	s3 := state.NewEvent(state.EventStoryCreated, "tl", "r001-s3", map[string]any{
		"id": "r001-s3", "req_id": "r-001", "title": "S3", "description": "d", "complexity": 1,
	})
	es.Append(s3)
	ps.Project(s3)
	aw3 := state.NewEvent(state.EventStoryAwaitingApproval, "monitor", "r001-s3", map[string]any{
		"story_id": "r001-s3",
	})
	es.Append(aw3)
	ps.Project(aw3)

	gate := NewReviewGate(es)
	pending, err := gate.PendingApprovals("r-001", ps)
	if err != nil {
		t.Fatalf("PendingApprovals: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending stories, got %d", len(pending))
	}
}

func TestPendingApprovals_WrongReqID(t *testing.T) {
	es, ps, cleanup := newRGStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "Task", "description": "d", "complexity": 1,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)
	awEvt := state.NewEvent(state.EventStoryAwaitingApproval, "monitor", "r001-s1", map[string]any{
		"story_id": "r001-s1",
	})
	es.Append(awEvt)
	ps.Project(awEvt)

	gate := NewReviewGate(es)
	pending, err := gate.PendingApprovals("r-999", ps)
	if err != nil {
		t.Fatalf("PendingApprovals: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending for wrong req, got %d", len(pending))
	}
}
