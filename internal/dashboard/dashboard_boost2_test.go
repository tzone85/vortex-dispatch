package dashboard

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// --- fetchData with real stores (currently 10% covered) ---

func TestFetchData_WithRealStores(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	// Seed data
	evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id": "req-1", "title": "Test", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(evt)
	ps.Project(evt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s1", map[string]any{
		"id": "s1", "req_id": "req-1", "title": "Story 1", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := New(es, ps, "1.0", state.ReqFilter{})
	cmd := m.fetchData()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Execute the cmd (it returns a dataMsg)
	msg := cmd()
	d, ok := msg.(dataMsg)
	if !ok {
		t.Fatal("expected dataMsg")
	}
	if d.err != nil {
		t.Fatalf("fetchData error: %v", d.err)
	}
	if len(d.requirements) != 1 {
		t.Errorf("expected 1 requirement, got %d", len(d.requirements))
	}
	if len(d.stories) != 1 {
		t.Errorf("expected 1 story, got %d", len(d.stories))
	}
}

func TestFetchData_WithFilter(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	// Seed multiple requirements
	for _, id := range []string{"req-1", "req-2"} {
		evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
			"id": id, "title": "Test " + id, "description": "desc", "repo_path": "/tmp",
		})
		es.Append(evt)
		ps.Project(evt)
	}

	// Stories for req-1 only
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s1", map[string]any{
		"id": "s1", "req_id": "req-1", "title": "Story", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m := New(es, ps, "1.0", state.ReqFilter{})
	cmd := m.fetchData()
	msg := cmd()
	d := msg.(dataMsg)
	if d.err != nil {
		t.Fatalf("error: %v", d.err)
	}
	if len(d.stories) != 1 {
		t.Errorf("expected 1 story, got %d", len(d.stories))
	}
}

func TestFetchData_WithEvents(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	evt := state.NewEvent(state.EventReqSubmitted, "sys", "", map[string]any{
		"id": "r1", "title": "T", "description": "D", "repo_path": "/tmp",
	})
	es.Append(evt)
	ps.Project(evt)

	m := New(es, ps, "1.0", state.ReqFilter{})
	cmd := m.fetchData()
	msg := cmd()
	d := msg.(dataMsg)
	if d.err != nil {
		t.Fatalf("error: %v", d.err)
	}
	if len(d.events) == 0 {
		t.Error("expected at least 1 event")
	}
}

// --- tickCmd coverage ---

func TestTickCmd_ProducesTickMsg(t *testing.T) {
	cmd := tickCmd()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}

// --- Update with tickMsg triggers fetch ---

func TestUpdate_TickMsg_TriggersFetch(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ps.Close()

	m := New(es, ps, "1.0", state.ReqFilter{})
	_, cmd := m.Update(tickMsg(time.Now()))
	if cmd == nil {
		t.Error("expected non-nil batch command from tick")
	}
}

// --- renderAgents with real data ---

func TestRenderAgents_MultipleMixed(t *testing.T) {
	agents := []state.Agent{
		{ID: "a1", Status: "active", SessionName: "vxd-1", CurrentStoryID: "s1"},
		{ID: "a2", Status: "idle", SessionName: "vxd-2", CurrentStoryID: "s2"},
		{ID: "a3", Status: "stuck", SessionName: "vxd-3", CurrentStoryID: "s3"},
	}
	m := Model{width: 100, agents: agents}
	out := m.renderAgents(100, 10)
	if !strings.Contains(out, "Agents") {
		t.Error("expected Agents header")
	}
}

// --- View with real data ---

func TestView_WithStories(t *testing.T) {
	m := Model{
		width:  100,
		height: 40,
		stories: []state.Story{
			{ID: "s1", Title: "Test Story", Status: "in_progress", Complexity: 3},
		},
		requirements: []state.Requirement{
			{ID: "r1", Title: "Test Req", Status: "in_progress"},
		},
	}
	out := m.View()
	if out == "Loading..." {
		t.Error("should not show loading with width set")
	}
}

// --- handleKey - ctrl+c ---

func TestHandleKey_CtrlC_ReturnsQuit(t *testing.T) {
	m := Model{width: 80, height: 24}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("expected quit command for ctrl+c")
	}
}
