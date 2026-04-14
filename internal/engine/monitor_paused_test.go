package engine

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newMonitorTestStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
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

func TestIsRequirementPaused_NotPaused(t *testing.T) {
	es, ps, cleanup := newMonitorTestStores(t)
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
		projStore: ps,
		config:    config.Config{Routing: config.RoutingConfig{}},
	}
	if m.isRequirementPaused("r001-s1") {
		t.Error("expected not paused")
	}
}

func TestIsRequirementPaused_Paused(t *testing.T) {
	es, ps, cleanup := newMonitorTestStores(t)
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

	// Pause the requirement
	pauseEvt := state.NewEvent(state.EventReqPaused, "monitor", "", map[string]any{
		"id": "r-001", "reason": "billing exhausted",
	})
	es.Append(pauseEvt)
	ps.Project(pauseEvt)

	m := &Monitor{
		projStore: ps,
		config:    config.Config{Routing: config.RoutingConfig{}},
	}
	if !m.isRequirementPaused("r001-s1") {
		t.Error("expected paused")
	}
}

func TestIsRequirementPaused_UnknownStory(t *testing.T) {
	es, ps, cleanup := newMonitorTestStores(t)
	defer cleanup()

	m := &Monitor{
		projStore: ps,
		eventStore: es,
		config:    config.Config{Routing: config.RoutingConfig{}},
	}
	// Should return false for non-existent story
	if m.isRequirementPaused("nonexistent-story") {
		t.Error("expected false for unknown story")
	}
}

func TestIsRequirementPaused_PausedThenResumed(t *testing.T) {
	es, ps, cleanup := newMonitorTestStores(t)
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

	// Pause then resume
	pauseEvt := state.NewEvent(state.EventReqPaused, "monitor", "", map[string]any{
		"id": "r-001", "reason": "billing exhausted",
	})
	es.Append(pauseEvt)
	ps.Project(pauseEvt)

	resumeEvt := state.NewEvent(state.EventReqResumed, "cli", "", map[string]any{
		"id": "r-001",
	})
	es.Append(resumeEvt)
	ps.Project(resumeEvt)

	m := &Monitor{
		projStore: ps,
		config:    config.Config{Routing: config.RoutingConfig{}},
	}
	if m.isRequirementPaused("r001-s1") {
		t.Error("expected not paused after resume")
	}
}
