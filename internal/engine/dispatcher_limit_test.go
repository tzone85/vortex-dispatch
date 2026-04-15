package engine_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestDispatchWave_RespectsMaxConcurrentAgents(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	// Pre-populate all stories in the projection (required for STORY_ASSIGNED update)
	for _, s := range []struct {
		id    string
		title string
	}{
		{"s-001", "Story 1"},
		{"s-002", "Story 2"},
		{"s-003", "Story 3"},
		{"s-004", "Story 4"},
		{"s-005", "Story 5"},
	} {
		evt := state.NewEvent(state.EventStoryCreated, "tech-lead", s.id, map[string]any{
			"id": s.id, "req_id": "req-001", "title": s.title, "description": "d", "complexity": 1,
		})
		ps.Project(evt)
	}

	cfg := config.DefaultConfig()
	cfg.Routing.MaxConcurrentAgents = 2

	d := engine.NewDispatcher(cfg, es, ps)

	stories := []engine.PlannedStory{
		{ID: "s-001", Title: "Story 1", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"a.go"}},
		{ID: "s-002", Title: "Story 2", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"b.go"}},
		{ID: "s-003", Title: "Story 3", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"c.go"}},
		{ID: "s-004", Title: "Story 4", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"d.go"}},
		{ID: "s-005", Title: "Story 5", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"e.go"}},
	}

	dag := graph.New()
	for _, s := range stories {
		dag.AddNode(s.ID)
	}

	completed := map[string]bool{}

	assignments, err := d.DispatchWave(dag, completed, "req-001", stories, 1)
	if err != nil {
		t.Fatalf("DispatchWave: %v", err)
	}

	if len(assignments) != 2 {
		t.Errorf("assignments = %d, want 2 (max_concurrent_agents)", len(assignments))
	}
}

func TestDispatchWave_NoLimitWhenZero(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()

	// Pre-populate all stories in the projection
	for _, s := range []struct {
		id    string
		title string
	}{
		{"s-001", "Story 1"},
		{"s-002", "Story 2"},
		{"s-003", "Story 3"},
	} {
		evt := state.NewEvent(state.EventStoryCreated, "tech-lead", s.id, map[string]any{
			"id": s.id, "req_id": "req-002", "title": s.title, "description": "d", "complexity": 1,
		})
		ps.Project(evt)
	}

	cfg := config.DefaultConfig()
	cfg.Routing.MaxConcurrentAgents = 0 // disabled

	d := engine.NewDispatcher(cfg, es, ps)

	stories := []engine.PlannedStory{
		{ID: "s-001", Title: "Story 1", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"a.go"}},
		{ID: "s-002", Title: "Story 2", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"b.go"}},
		{ID: "s-003", Title: "Story 3", Complexity: 1, WaveHint: "parallel", OwnedFiles: []string{"c.go"}},
	}

	dag := graph.New()
	for _, s := range stories {
		dag.AddNode(s.ID)
	}

	completed := map[string]bool{}

	assignments, err := d.DispatchWave(dag, completed, "req-002", stories, 1)
	if err != nil {
		t.Fatalf("DispatchWave: %v", err)
	}

	if len(assignments) != 3 {
		t.Errorf("assignments = %d, want 3 (no limit when 0)", len(assignments))
	}
}
