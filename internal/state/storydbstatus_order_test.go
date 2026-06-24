package state

import (
	"path/filepath"
	"testing"
	"time"
)

// TestStoryDBStatusByReq_LatestWins pins that when a story has multiple DB rows
// (e.g. an old DB plus a newer re-provisioned one), the status reflects the
// LATEST row by created_at — not whatever order SQLite happens to return. The
// previous query had no ORDER BY, so the displayed devdb status could flip
// between dashboard refreshes.
func TestStoryDBStatusByReq_LatestWins(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "r1", "title": "req"}))
	s.Project(NewEvent(EventStoryCreated, "", "s1", map[string]any{"id": "s1", "req_id": "r1", "title": "story 1", "complexity": 3}))

	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	// Older DB (db1) created first; a newer DB (db2) is created later and then
	// deleted. The latest row by created_at is db2 -> "deleted".
	mustProject(t, s, Event{Type: EventStoryDBCreated, StoryID: "s1", Timestamp: t1,
		Payload: mustJSONBytes(t, map[string]any{"db_id": "db1", "db_name": "n1", "provider": "docker"})})
	mustProject(t, s, Event{Type: EventStoryDBCreated, StoryID: "s1", Timestamp: t2,
		Payload: mustJSONBytes(t, map[string]any{"db_id": "db2", "db_name": "n2", "provider": "docker"})})
	mustProject(t, s, Event{Type: EventStoryDBDeleted, StoryID: "s1", Timestamp: t2,
		Payload: mustJSONBytes(t, map[string]any{"db_id": "db2", "status": "deleted"})})

	statuses, err := s.StoryDBStatusByReq("r1")
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses["s1"]; got != "deleted" {
		t.Errorf("StoryDBStatusByReq latest status = %q, want %q (latest row by created_at)", got, "deleted")
	}

	all, err := s.StoryDBStatusAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := all["s1"]; got != "deleted" {
		t.Errorf("StoryDBStatusAll latest status = %q, want %q", got, "deleted")
	}
}
