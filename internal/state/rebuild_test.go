package state_test

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestRebuild_ReconstructsProjectionFromLog verifies that a fresh projection
// rebuilt from the event log matches the incrementally-built one — the
// recovery path for log/projection divergence.
func TestRebuild_ReconstructsProjectionFromLog(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()

	// Live store that projects as events are appended.
	live, err := state.NewSQLiteStore(filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()

	events := []state.Event{
		state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{"id": "REQ-1", "title": "Build it"}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "REQ-1-s1", map[string]any{
			"id": "REQ-1-s1", "req_id": "REQ-1", "title": "Story 1", "complexity": 2,
		}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "REQ-1-s2", map[string]any{
			"id": "REQ-1-s2", "req_id": "REQ-1", "title": "Story 2", "complexity": 3,
		}),
	}
	for _, e := range events {
		if err := es.Append(e); err != nil {
			t.Fatal(err)
		}
		if err := live.Project(e); err != nil {
			t.Fatal(err)
		}
	}

	// Rebuild a fresh store purely from the log.
	rebuilt, err := state.NewSQLiteStore(filepath.Join(dir, "rebuilt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	if err := rebuilt.Rebuild(es); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	for _, id := range []string{"REQ-1-s1", "REQ-1-s2"} {
		liveStory, err := live.GetStory(id)
		if err != nil {
			t.Fatalf("live GetStory(%s): %v", id, err)
		}
		rebuiltStory, err := rebuilt.GetStory(id)
		if err != nil {
			t.Fatalf("rebuilt GetStory(%s): %v", id, err)
		}
		if rebuiltStory.Title != liveStory.Title || rebuiltStory.Complexity != liveStory.Complexity {
			t.Errorf("rebuilt story %s = %+v, want %+v", id, rebuiltStory, liveStory)
		}
	}
}

// TestRebuild_IsIdempotent verifies Rebuild can run repeatedly without
// duplicating rows or erroring.
func TestRebuild_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer es.Close()
	_ = es.Append(state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{"id": "REQ-X", "title": "X"}))
	_ = es.Append(state.NewEvent(state.EventStoryCreated, "tl", "REQ-X-s1", map[string]any{
		"id": "REQ-X-s1", "req_id": "REQ-X", "title": "S", "complexity": 1,
	}))

	store, err := state.NewSQLiteStore(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for i := 0; i < 3; i++ {
		if err := store.Rebuild(es); err != nil {
			t.Fatalf("rebuild #%d: %v", i, err)
		}
	}
	stories, err := store.ListStories(state.StoryFilter{ReqID: "REQ-X"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stories) != 1 {
		t.Errorf("expected exactly 1 story after repeated rebuilds, got %d", len(stories))
	}
}

// TestProjectStoryCreated_Idempotent verifies a duplicate STORY_CREATED does
// not error (the projection must be replay-safe).
func TestProjectStoryCreated_Idempotent(t *testing.T) {
	store, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	evt := state.NewEvent(state.EventStoryCreated, "tl", "s-dup", map[string]any{
		"id": "s-dup", "req_id": "REQ-D", "title": "Dup", "complexity": 2,
	})
	if err := store.Project(evt); err != nil {
		t.Fatalf("first project: %v", err)
	}
	if err := store.Project(evt); err != nil {
		t.Fatalf("duplicate project must be idempotent, got: %v", err)
	}
}
