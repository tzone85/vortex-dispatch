package state_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// Tests to boost coverage for previously-uncovered SQLite store functions.

func TestSQLiteStore_ListRequirements(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer db.Close()

	// Empty list
	reqs, err := db.ListRequirements()
	if err != nil {
		t.Fatalf("list requirements (empty): %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("expected 0 requirements, got %d", len(reqs))
	}

	// Add two requirements
	for _, r := range []struct{ id, title string }{
		{"r-001", "First req"},
		{"r-002", "Second req"},
	} {
		evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
			"id":          r.id,
			"title":       r.title,
			"description": "test desc",
		})
		if err := db.Project(evt); err != nil {
			t.Fatalf("project %s: %v", r.id, err)
		}
	}

	reqs, err = db.ListRequirements()
	if err != nil {
		t.Fatalf("list requirements: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requirements, got %d", len(reqs))
	}
	if reqs[0].ID != "r-001" {
		t.Errorf("expected first req r-001, got %s", reqs[0].ID)
	}
}

func TestSQLiteStore_ListAgents(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer db.Close()

	// Empty list with no filter
	agents, err := db.ListAgents(state.AgentFilter{})
	if err != nil {
		t.Fatalf("list agents (empty, no filter): %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}

	// Empty list with status filter
	agents, err = db.ListAgents(state.AgentFilter{Status: "running"})
	if err != nil {
		t.Fatalf("list agents (empty, with filter): %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents with running filter, got %d", len(agents))
	}
}

func TestSQLiteStore_ListStoryDeps(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer db.Close()

	// Create a requirement and stories
	reqEvt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "r-001",
		"title":       "Test req",
		"description": "test",
	})
	if err := db.Project(reqEvt); err != nil {
		t.Fatalf("project req: %v", err)
	}

	for _, s := range []string{"s-001", "s-002"} {
		evt := state.NewEvent(state.EventStoryCreated, "system", "r-001", map[string]any{
			"id":          s,
			"title":       "Story " + s,
			"description": "test story",
			"complexity":  3,
		})
		if err := db.Project(evt); err != nil {
			t.Fatalf("project story %s: %v", s, err)
		}
	}

	// Empty deps initially
	deps, err := db.ListStoryDeps("r-001")
	if err != nil {
		t.Fatalf("list story deps: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

func TestSQLiteStore_BackfillAcceptanceCriteria(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer db.Close()

	// Create a requirement
	reqEvt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "r-001",
		"title":       "Test req",
		"description": "test",
	})
	if err := db.Project(reqEvt); err != nil {
		t.Fatalf("project req: %v", err)
	}

	// Create a story WITHOUT acceptance criteria
	storyEvt := state.NewEvent(state.EventStoryCreated, "system", "r-001", map[string]any{
		"id":          "s-001",
		"title":       "Story One",
		"description": "test story",
		"complexity":  3,
	})
	if err := db.Project(storyEvt); err != nil {
		t.Fatalf("project story: %v", err)
	}

	// Story should have empty AC
	story, err := db.GetStory("s-001")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.AcceptanceCriteria != "" {
		t.Errorf("expected empty AC, got %q", story.AcceptanceCriteria)
	}

	// Now backfill with an event that has AC
	backfillEvt := state.NewEvent(state.EventStoryCreated, "system", "r-001", map[string]any{
		"id":                  "s-001",
		"title":               "Story One",
		"acceptance_criteria": "Must pass all tests",
		"complexity":          3,
	})
	db.BackfillAcceptanceCriteria([]state.Event{backfillEvt})

	// Story should now have AC
	story, err = db.GetStory("s-001")
	if err != nil {
		t.Fatalf("get story after backfill: %v", err)
	}
	if story.AcceptanceCriteria != "Must pass all tests" {
		t.Errorf("expected backfilled AC, got %q", story.AcceptanceCriteria)
	}
}
