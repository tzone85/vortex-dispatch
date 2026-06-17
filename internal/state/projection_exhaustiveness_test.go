package state

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestProject_PipelineStalled verifies the PIPELINE_STALLED event is handled
// explicitly by the projection switch (returns nil, no default-WARNING branch).
// It was previously emitted via a raw string literal with no declared constant
// and no projection case — a latent gap if the event log is ever replayed.
func TestProject_PipelineStalled(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	evt := NewEvent(EventPipelineStalled, "monitor", "", map[string]any{
		"req_id":        "REQ-1",
		"pending_count": 2,
	})
	if err := s.Project(evt); err != nil {
		t.Errorf("expected nil error projecting PIPELINE_STALLED, got: %v", err)
	}
}

// TestProject_AllDeclaredEventsHandled is a static guard against the highest-value
// wiring bug class in this event-sourced system: an EventType constant that is
// declared (and therefore emittable) but never handled in sqlite.go's Project()
// switch, so it silently falls through to the default-WARNING branch and the
// SQLite projection diverges from the event log.
//
// It scans events.go for every `EventXxx EventType = "..."` constant, then
// asserts each constant name is referenced somewhere in sqlite.go. A new event
// type added without a projection case (even an explicit `return nil` for
// observational events) fails this test at authoring time.
func TestProject_AllDeclaredEventsHandled(t *testing.T) {
	eventsSrc, err := os.ReadFile("events.go")
	if err != nil {
		t.Fatalf("read events.go: %v", err)
	}
	sqliteSrc, err := os.ReadFile("sqlite.go")
	if err != nil {
		t.Fatalf("read sqlite.go: %v", err)
	}
	sqlite := string(sqliteSrc)

	// Match: EventStoryCreated EventType = "STORY_CREATED"
	re := regexp.MustCompile(`\b(Event\w+)\s+EventType\s*=`)
	matches := re.FindAllStringSubmatch(string(eventsSrc), -1)
	if len(matches) == 0 {
		t.Fatal("no EventType constants found in events.go — regex may be stale")
	}

	var unhandled []string
	for _, m := range matches {
		name := m[1]
		// The constant must be referenced in sqlite.go — either as its own
		// `case EventXxx:` or grouped in a multi-constant case line.
		if !strings.Contains(sqlite, name) {
			unhandled = append(unhandled, name)
		}
	}
	if len(unhandled) > 0 {
		t.Errorf("event constants declared in events.go but not referenced in sqlite.go Project() switch "+
			"(they will hit the default-WARNING branch and silently corrupt the projection): %s\n"+
			"Add an explicit case (use `return nil` for observational events).",
			strings.Join(unhandled, ", "))
	}
}
