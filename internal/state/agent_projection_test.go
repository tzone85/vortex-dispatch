package state_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestSQLiteStore_ProjectsAgentLifecycle(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	project := func(evt state.Event) {
		t.Helper()
		if err := db.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evt.Type, err)
		}
	}

	project(state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "req-1",
		"title":       "Req 1",
		"description": "Req 1",
		"repo_path":   "/repo",
	}))
	project(state.NewEvent(state.EventStoryCreated, "system", "story-1", map[string]any{
		"id":                  "story-1",
		"req_id":              "req-1",
		"title":               "Story 1",
		"description":         "Story 1",
		"acceptance_criteria": "ok",
		"complexity":          2,
	}))

	project(state.NewEvent(state.EventAgentSpawned, "agent-1", "story-1", map[string]any{
		"agent_type":   "junior",
		"model":        "claude-sonnet",
		"runtime":      "tmux",
		"session_name": "vxd-story-1",
	}))
	project(state.NewEvent(state.EventStoryAssigned, "agent-1", "story-1", map[string]any{
		"agent_id": "agent-1",
		"wave":     1,
	}))

	agents, err := db.ListAgents(state.AgentFilter{})
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(agents))
	}
	if agents[0].ID != "agent-1" {
		t.Fatalf("agent id = %q, want agent-1", agents[0].ID)
	}
	if agents[0].Type != "junior" {
		t.Fatalf("agent type = %q, want junior", agents[0].Type)
	}
	if agents[0].Status != "active" {
		t.Fatalf("agent status = %q, want active", agents[0].Status)
	}
	if agents[0].CurrentStoryID != "story-1" {
		t.Fatalf("current_story_id = %q, want story-1", agents[0].CurrentStoryID)
	}
	if agents[0].SessionName != "vxd-story-1" {
		t.Fatalf("session_name = %q, want vxd-story-1", agents[0].SessionName)
	}

	project(state.NewEvent(state.EventStoryCompleted, "agent-1", "story-1", nil))

	agents, err = db.ListAgents(state.AgentFilter{Status: "idle"})
	if err != nil {
		t.Fatalf("list idle agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("idle agents len = %d, want 1", len(agents))
	}
	if agents[0].CurrentStoryID != "" {
		t.Fatalf("idle current_story_id = %q, want empty", agents[0].CurrentStoryID)
	}

	project(state.NewEvent(state.EventAgentStuck, "", "", map[string]any{
		"session_name": "vxd-story-1",
	}))
	agents, err = db.ListAgents(state.AgentFilter{Status: "stuck"})
	if err != nil {
		t.Fatalf("list stuck agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("stuck agents len = %d, want 1", len(agents))
	}

	project(state.NewEvent(state.EventAgentTerminated, "agent-1", "", map[string]any{
		"reason": "manual kill",
	}))
	agents, err = db.ListAgents(state.AgentFilter{Status: "terminated"})
	if err != nil {
		t.Fatalf("list terminated agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("terminated agents len = %d, want 1", len(agents))
	}
}
