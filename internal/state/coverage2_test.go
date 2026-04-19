package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// DecodePayload edge cases
// ---------------------------------------------------------------------------

func TestDecodePayload_EmptyBytes(t *testing.T) {
	result := DecodePayload([]byte{})
	if len(result) != 0 {
		t.Errorf("expected empty map for empty bytes, got %v", result)
	}
}

func TestDecodePayload_NilBytes(t *testing.T) {
	result := DecodePayload(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for nil bytes, got %v", result)
	}
}

func TestDecodePayload_InvalidJSON(t *testing.T) {
	result := DecodePayload([]byte("not json"))
	if len(result) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %v", result)
	}
}

func TestDecodePayload_ValidJSON(t *testing.T) {
	data := []byte(`{"key":"value","num":42}`)
	result := DecodePayload(data)
	if result["key"] != "value" {
		t.Errorf("expected key=value, got %v", result["key"])
	}
	if result["num"].(float64) != 42 {
		t.Errorf("expected num=42, got %v", result["num"])
	}
}

// ---------------------------------------------------------------------------
// payloadStr / payloadInt / payloadMap edge cases
// ---------------------------------------------------------------------------

func TestPayloadStr_MissingKey(t *testing.T) {
	m := map[string]any{"a": "b"}
	if payloadStr(m, "missing") != "" {
		t.Error("expected empty string for missing key")
	}
}

func TestPayloadStr_NonStringValue(t *testing.T) {
	m := map[string]any{"num": 42}
	if payloadStr(m, "num") != "" {
		t.Error("expected empty string for non-string value")
	}
}

func TestPayloadInt_MissingKey(t *testing.T) {
	m := map[string]any{"a": "b"}
	if payloadInt(m, "missing") != 0 {
		t.Error("expected 0 for missing key")
	}
}

func TestPayloadInt_FloatValue(t *testing.T) {
	m := map[string]any{"num": float64(42)}
	if payloadInt(m, "num") != 42 {
		t.Errorf("expected 42, got %d", payloadInt(m, "num"))
	}
}

func TestPayloadInt_IntValue(t *testing.T) {
	m := map[string]any{"num": 7}
	if payloadInt(m, "num") != 7 {
		t.Errorf("expected 7, got %d", payloadInt(m, "num"))
	}
}

func TestPayloadInt_StringValue(t *testing.T) {
	m := map[string]any{"num": "not-a-number"}
	if payloadInt(m, "num") != 0 {
		t.Error("expected 0 for string value")
	}
}

func TestPayloadMap_MissingKey(t *testing.T) {
	m := map[string]any{"a": "b"}
	result := payloadMap(m, "missing")
	if len(result) != 0 {
		t.Error("expected empty map for missing key")
	}
}

func TestPayloadMap_NonMapValue(t *testing.T) {
	m := map[string]any{"str": "hello"}
	result := payloadMap(m, "str")
	if len(result) != 0 {
		t.Error("expected empty map for non-map value")
	}
}

func TestPayloadMap_ValidMap(t *testing.T) {
	sub := map[string]any{"inner": "value"}
	m := map[string]any{"sub": sub}
	result := payloadMap(m, "sub")
	if result["inner"] != "value" {
		t.Errorf("expected inner=value, got %v", result["inner"])
	}
}

// ---------------------------------------------------------------------------
// SQLiteStore.decodePayload
// ---------------------------------------------------------------------------

func TestSQLiteStore_DecodePayload_Nil(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	result := s.decodePayload(Event{Payload: nil})
	if len(result) != 0 {
		t.Error("expected empty map for nil payload")
	}
}

func TestSQLiteStore_DecodePayload_Invalid(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	result := s.decodePayload(Event{Payload: []byte("bad json")})
	if len(result) != 0 {
		t.Error("expected empty map for invalid JSON payload")
	}
}

// ---------------------------------------------------------------------------
// Project — event types that haven't been tested
// ---------------------------------------------------------------------------

func TestSQLiteStore_ProjectReqResumed(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	// Create requirement
	reqEvt := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-R1", "title": "Test"})
	s.Project(reqEvt)

	// Pause it
	pauseEvt := NewEvent(EventReqPaused, "", "", map[string]any{"id": "REQ-R1"})
	s.Project(pauseEvt)

	req, _ := s.GetRequirement("REQ-R1")
	if req.Status != "paused" {
		t.Fatalf("expected paused, got %s", req.Status)
	}

	// Resume it
	resumeEvt := NewEvent(EventReqResumed, "", "", map[string]any{"id": "REQ-R1"})
	s.Project(resumeEvt)

	req, _ = s.GetRequirement("REQ-R1")
	if req.Status != "planned" {
		t.Errorf("expected planned after resume, got %s", req.Status)
	}
}

func TestSQLiteStore_ProjectUnhandledEventType(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	// Project an unknown event type — should be silently ignored
	unknownEvt := NewEvent(EventType("UNKNOWN_EVENT"), "", "", nil)
	if err := s.Project(unknownEvt); err != nil {
		t.Errorf("expected nil error for unknown event type, got %v", err)
	}
}

func TestSQLiteStore_ProjectStoryRewritten_AllFields(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	// Create requirement and story
	reqEvt := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-RW1", "title": "Rewrite"})
	s.Project(reqEvt)

	storyEvt := NewEvent(EventStoryCreated, "", "S-RW1", map[string]any{
		"id": "S-RW1", "req_id": "REQ-RW1", "title": "Original", "complexity": 3,
	})
	s.Project(storyEvt)

	// Rewrite with all fields changed including int complexity
	rewriteEvt := NewEvent(EventStoryRewritten, "", "S-RW1", map[string]any{
		"changes": map[string]any{
			"title":               "New Title",
			"description":         "New Description",
			"acceptance_criteria": "New AC",
			"complexity":          5,
		},
	})
	s.Project(rewriteEvt)

	story, err := s.GetStory("S-RW1")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Title != "New Title" {
		t.Errorf("expected new title, got %s", story.Title)
	}
	if story.Status != "draft" {
		t.Errorf("expected draft after rewrite, got %s", story.Status)
	}
}

func TestSQLiteStore_ProjectStoryRewritten_ComplexityFloat(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	reqEvt := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-RW2", "title": "Rewrite2"})
	s.Project(reqEvt)

	storyEvt := NewEvent(EventStoryCreated, "", "S-RW2", map[string]any{
		"id": "S-RW2", "req_id": "REQ-RW2", "title": "Orig", "complexity": 3,
	})
	s.Project(storyEvt)

	// JSON unmarshalling turns numbers to float64
	rewriteEvt := NewEvent(EventStoryRewritten, "", "S-RW2", map[string]any{
		"changes": map[string]any{
			"complexity": float64(8),
		},
	})
	s.Project(rewriteEvt)

	story, _ := s.GetStory("S-RW2")
	if story.Complexity != 8 {
		t.Errorf("expected complexity 8, got %d", story.Complexity)
	}
}

// ---------------------------------------------------------------------------
// FileStore — Count with After filter
// ---------------------------------------------------------------------------

func TestFileStore_CountWithAfterFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	// Add events at different times
	evt1 := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R1"})
	fs.Append(evt1)

	// Tiny delay to ensure different timestamps
	time.Sleep(10 * time.Millisecond)
	afterTime := time.Now()
	time.Sleep(10 * time.Millisecond)

	evt2 := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R2"})
	fs.Append(evt2)

	count, err := fs.Count(EventFilter{After: afterTime})
	if err != nil {
		t.Fatalf("count with after: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 event after time, got %d", count)
	}
}

func TestFileStore_CountWithLimit(t *testing.T) {
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

	count, err := fs.Count(EventFilter{Limit: 3})
	if err != nil {
		t.Fatalf("count with limit: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count limited to 3, got %d", count)
	}
}

func TestFileStore_CountWithMalformedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	// Write a valid event then a malformed line
	evt := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "R1"})
	data, _ := json.Marshal(evt)

	os.WriteFile(path, append(append(data, '\n'), []byte("bad json\n")...), 0o644)

	fs, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer fs.Close()

	count, err := fs.Count(EventFilter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 valid event, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// ListStoryDeps — edge cases
// ---------------------------------------------------------------------------

func TestSQLiteStore_ListStoryDeps_NoDeps(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	reqEvt := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-ND1", "title": "No Deps"})
	s.Project(reqEvt)

	storyEvt := NewEvent(EventStoryCreated, "", "S-ND1", map[string]any{
		"id": "S-ND1", "req_id": "REQ-ND1", "title": "Solo", "complexity": 3,
	})
	s.Project(storyEvt)

	deps, err := s.ListStoryDeps("REQ-ND1")
	if err != nil {
		t.Fatalf("list deps: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

// ---------------------------------------------------------------------------
// ListAgents — edge cases
// ---------------------------------------------------------------------------

func TestSQLiteStore_ListAgents_WithStatusFilter(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	// No agents yet
	agents, err := s.ListAgents(AgentFilter{Status: "active"})
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

// ---------------------------------------------------------------------------
// projectStoryCreated — edge case: owned_files and wave_hint
// ---------------------------------------------------------------------------

func TestSQLiteStore_ProjectStoryCreated_WithOwnedFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	reqEvt := NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-OF1", "title": "Owned"})
	s.Project(reqEvt)

	storyEvt := NewEvent(EventStoryCreated, "", "S-OF1", map[string]any{
		"id":          "S-OF1",
		"req_id":      "REQ-OF1",
		"title":       "With owned files",
		"complexity":  5,
		"owned_files": []any{"file1.go", "file2.go"},
		"wave_hint":   "sequential",
		"split_depth": 2,
	})
	s.Project(storyEvt)

	story, err := s.GetStory("S-OF1")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Title != "With owned files" {
		t.Errorf("expected title, got %s", story.Title)
	}
}

// ---------------------------------------------------------------------------
// GetStory — story not found
// ---------------------------------------------------------------------------

func TestSQLiteStore_GetStory_NotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	_, err = s.GetStory("NONEXISTENT")
	if err == nil {
		t.Error("expected error for nonexistent story")
	}
}

// ---------------------------------------------------------------------------
// GetRequirement — not found
// ---------------------------------------------------------------------------

func TestSQLiteStore_GetRequirement_NotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	_, err = s.GetRequirement("NONEXISTENT")
	if err == nil {
		t.Error("expected error for nonexistent requirement")
	}
}
