package engine

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newPauseTestStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
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

func TestPauseRequirement_Success(t *testing.T) {
	es, ps, cleanup := newPauseTestStores(t)
	defer cleanup()

	// Create requirement and story
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

	m := &Monitor{
		eventStore: es,
		projStore:  ps,
		config:     config.Config{Routing: config.RoutingConfig{}},
	}

	m.pauseRequirement("r001-s1", "billing exhausted")

	// Verify requirement is now paused
	req, err := ps.GetRequirement("r-001")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Errorf("expected paused status, got %s", req.Status)
	}

	// Verify REQ_PAUSED event was emitted
	events, err := es.List(state.EventFilter{Type: state.EventReqPaused})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 REQ_PAUSED event, got %d", len(events))
	}
}

func TestPauseRequirement_UnknownStory(t *testing.T) {
	es, ps, cleanup := newPauseTestStores(t)
	defer cleanup()

	m := &Monitor{
		eventStore: es,
		projStore:  ps,
		config:     config.Config{Routing: config.RoutingConfig{}},
	}

	// Should not panic for unknown story
	m.pauseRequirement("nonexistent-story", "test reason")

	// No pause event should be emitted
	events, _ := es.List(state.EventFilter{Type: state.EventReqPaused})
	if len(events) != 0 {
		t.Errorf("expected 0 REQ_PAUSED events for unknown story, got %d", len(events))
	}
}
