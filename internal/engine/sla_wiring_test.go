package engine

import (
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestCheckSLA_EmitsBreachEventOnce(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(dir + "/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	storyID := "s-test-001"

	// Project a STORY_CREATED event to seed the story
	createdEvt := state.NewEvent(state.EventStoryCreated, "system", storyID, map[string]any{
		"id":          storyID,
		"req_id":      "req-001",
		"title":       "Test",
		"description": "Test story",
		"complexity":  3,
	})
	if err := es.Append(createdEvt); err != nil {
		t.Fatal(err)
	}
	if err := ps.Project(createdEvt); err != nil {
		t.Fatal(err)
	}

	// Emit STORY_STARTED 5 hours ago (way past 4hr SLA for complexity 3)
	startedEvt := state.Event{
		ID:        "evt-started",
		Type:      state.EventStoryStarted,
		Timestamp: time.Now().Add(-5 * time.Hour),
		AgentID:   "agent-1",
		StoryID:   storyID,
	}
	if err := es.Append(startedEvt); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	m := &Monitor{
		config:         cfg,
		eventStore:     es,
		projStore:      ps,
		slaStartTimes:  make(map[string]time.Time),
		slaBreachedSet: make(map[string]bool),
	}

	ag := ActiveAgent{
		Assignment: Assignment{
			StoryID: storyID,
			AgentID: "agent-1",
		},
	}

	// First call should emit breach
	m.checkSLA(ag)

	breaches, err := es.List(state.EventFilter{
		Type:    state.EventStorySLABreached,
		StoryID: storyID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(breaches) != 1 {
		t.Errorf("after first call: breach events = %d, want 1", len(breaches))
	}

	// Second call should NOT emit again
	m.checkSLA(ag)

	breaches, _ = es.List(state.EventFilter{
		Type:    state.EventStorySLABreached,
		StoryID: storyID,
	})
	if len(breaches) != 1 {
		t.Errorf("after second call: breach events = %d, want 1 (no duplicate)", len(breaches))
	}
}

func TestCheckSLA_NoBreachWhenWithinLimit(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(dir + "/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	storyID := "s-test-002"
	createdEvt := state.NewEvent(state.EventStoryCreated, "system", storyID, map[string]any{
		"id": storyID, "req_id": "req-001", "title": "Test", "description": "Test", "complexity": 3,
	})
	es.Append(createdEvt)
	ps.Project(createdEvt)

	// Started 30 min ago — well under 4hr SLA for complexity 3
	startedEvt := state.Event{
		ID:        "evt-s2",
		Type:      state.EventStoryStarted,
		Timestamp: time.Now().Add(-30 * time.Minute),
		AgentID:   "agent-2",
		StoryID:   storyID,
	}
	es.Append(startedEvt)

	m := &Monitor{
		config:         config.DefaultConfig(),
		eventStore:     es,
		projStore:      ps,
		slaStartTimes:  make(map[string]time.Time),
		slaBreachedSet: make(map[string]bool),
	}
	ag := ActiveAgent{Assignment: Assignment{StoryID: storyID, AgentID: "agent-2"}}

	m.checkSLA(ag)

	breaches, _ := es.List(state.EventFilter{Type: state.EventStorySLABreached, StoryID: storyID})
	if len(breaches) != 0 {
		t.Errorf("expected no breaches for in-limit story, got %d", len(breaches))
	}
}
