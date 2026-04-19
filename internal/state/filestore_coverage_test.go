package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// FileStore — cover remaining paths
// ---------------------------------------------------------------------------

func TestFileStore_Count_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	count, err := fs.Count(EventFilter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestFileStore_Count_WithTypeFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	fs.Append(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R1"}))
	fs.Append(NewEvent(EventStoryCreated, "", "S1", map[string]any{"id": "S1"}))
	fs.Append(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R2"}))

	count, err := fs.Count(EventFilter{Type: EventReqSubmitted})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 REQ_SUBMITTED, got %d", count)
	}
}

func TestFileStore_Count_WithAgentFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	fs.Append(NewEvent(EventStoryStarted, "agent-1", "S1", nil))
	fs.Append(NewEvent(EventStoryStarted, "agent-2", "S2", nil))
	fs.Append(NewEvent(EventStoryStarted, "agent-1", "S3", nil))

	count, err := fs.Count(EventFilter{AgentID: "agent-1"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 agent-1 events, got %d", count)
	}
}

func TestFileStore_Count_WithStoryFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	fs.Append(NewEvent(EventStoryStarted, "", "S1", nil))
	fs.Append(NewEvent(EventStoryStarted, "", "S2", nil))

	count, err := fs.Count(EventFilter{StoryID: "S1"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

func TestFileStore_List_WithAfterFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	fs.Append(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R1"}))
	time.Sleep(10 * time.Millisecond)
	afterTime := time.Now()
	time.Sleep(10 * time.Millisecond)
	fs.Append(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R2"}))

	events, err := fs.List(EventFilter{After: afterTime})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event after time, got %d", len(events))
	}
}

func TestFileStore_List_WithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	for i := 0; i < 5; i++ {
		fs.Append(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R"}))
	}

	events, err := fs.List(EventFilter{Limit: 3})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3, got %d", len(events))
	}
}

func TestFileStore_List_MalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	// Write valid + malformed lines
	evt := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R1"})
	fs, _ := NewFileStore(path)
	fs.Append(evt)
	fs.Close()

	// Append a bad line manually
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.Write([]byte("bad json line\n"))
	f.Close()

	fs2, _ := NewFileStore(path)
	defer fs2.Close()

	events, err := fs2.List(EventFilter{})
	if err != nil {
		t.Fatalf("list with malformed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 valid event, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// ListStoryDeps — with actual dependencies
// ---------------------------------------------------------------------------

func TestSQLiteStore_ListStoryDeps_WithDeps(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-D1", "title": "Deps"}))

	// Story A (no deps)
	s.Project(NewEvent(EventStoryCreated, "", "S-A", map[string]any{
		"id": "S-A", "req_id": "REQ-D1", "title": "A", "complexity": 3,
	}))

	// Story B depends on A
	s.Project(NewEvent(EventStoryCreated, "", "S-B", map[string]any{
		"id": "S-B", "req_id": "REQ-D1", "title": "B", "complexity": 3,
		"depends_on": []any{"S-A"},
	}))

	// Story C depends on A and B
	s.Project(NewEvent(EventStoryCreated, "", "S-C", map[string]any{
		"id": "S-C", "req_id": "REQ-D1", "title": "C", "complexity": 3,
		"depends_on": []any{"S-A", "S-B"},
	}))

	deps, err := s.ListStoryDeps("REQ-D1")
	if err != nil {
		t.Fatalf("list deps: %v", err)
	}
	if len(deps) != 3 { // B->A, C->A, C->B
		t.Errorf("expected 3 dependency edges, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// ListAgents — SQL building
// ---------------------------------------------------------------------------

func TestSQLiteStore_ListAgents_NoFilter(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	agents, err := s.ListAgents(AgentFilter{})
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0, got %d", len(agents))
	}
}

// ---------------------------------------------------------------------------
// ListEscalations — with data
// ---------------------------------------------------------------------------

func TestSQLiteStore_Escalation_FullFlow(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-E1", "title": "Esc"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-E1", map[string]any{
		"id": "S-E1", "req_id": "REQ-E1", "title": "Story", "complexity": 3,
	}))

	s.Project(NewEvent(EventStoryEscalated, "agent-1", "S-E1", map[string]any{
		"reason":    "build failed",
		"from_tier": 0,
		"to_tier":   1,
	}))

	story, _ := s.GetStory("S-E1")
	if story.EscalationTier != 1 {
		t.Errorf("expected escalation tier 1, got %d", story.EscalationTier)
	}

	escs, _ := s.ListEscalations()
	if len(escs) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(escs))
	}
	if escs[0].Reason != "build failed" {
		t.Errorf("expected 'build failed', got '%s'", escs[0].Reason)
	}
}
