package state

import (
	"testing"
)

// seedStoryRow inserts a story row directly, bypassing the projection, so the
// owned_files column can hold arbitrary (including corrupt) JSON.
func seedStoryRow(t *testing.T, s *SQLiteStore, id, ownedFilesJSON string) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO stories (id, req_id, title, description, acceptance_criteria, complexity, status, owned_files, wave_hint)
		 VALUES (?, 'r-001', 'title', 'desc', 'ac', 3, 'draft', ?, 'parallel')`,
		id, ownedFilesJSON,
	)
	if err != nil {
		t.Fatalf("seed story row: %v", err)
	}
}

// TestGetStory_CorruptOwnedFilesFlagged pins the conservative failure mode for
// audit finding E-05: a story whose owned_files column holds invalid JSON must
// come back with OwnedFilesCorrupt=true so downstream consumers (rebuildDAG →
// dispatcher) can serialize it instead of dispatching it in parallel with no
// ownership information.
func TestGetStory_CorruptOwnedFilesFlagged(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	seedStoryRow(t, s, "s-corrupt", `{not json[`)
	seedStoryRow(t, s, "s-good", `["a.go","b.go"]`)

	corrupt, err := s.GetStory("s-corrupt")
	if err != nil {
		t.Fatalf("GetStory corrupt: %v", err)
	}
	if !corrupt.OwnedFilesCorrupt {
		t.Error("OwnedFilesCorrupt = false, want true for invalid JSON")
	}
	if len(corrupt.OwnedFiles) != 0 {
		t.Errorf("OwnedFiles = %v, want empty on corrupt JSON", corrupt.OwnedFiles)
	}

	good, err := s.GetStory("s-good")
	if err != nil {
		t.Fatalf("GetStory good: %v", err)
	}
	if good.OwnedFilesCorrupt {
		t.Error("OwnedFilesCorrupt = true for valid JSON, want false")
	}
	if len(good.OwnedFiles) != 2 {
		t.Errorf("OwnedFiles = %v, want 2 entries", good.OwnedFiles)
	}
}

// TestListStories_CorruptOwnedFilesFlagged pins the same behavior on the list
// path (the one rebuildDAG actually uses on resume).
func TestListStories_CorruptOwnedFilesFlagged(t *testing.T) {
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	seedStoryRow(t, s, "s-corrupt", `[unterminated`)
	seedStoryRow(t, s, "s-good", `["c.go"]`)

	stories, err := s.ListStories(StoryFilter{ReqID: "r-001"})
	if err != nil {
		t.Fatalf("ListStories: %v", err)
	}
	if len(stories) != 2 {
		t.Fatalf("expected 2 stories, got %d", len(stories))
	}
	byID := map[string]Story{}
	for _, st := range stories {
		byID[st.ID] = st
	}
	if !byID["s-corrupt"].OwnedFilesCorrupt {
		t.Error("s-corrupt: OwnedFilesCorrupt = false, want true")
	}
	if byID["s-good"].OwnedFilesCorrupt {
		t.Error("s-good: OwnedFilesCorrupt = true, want false")
	}
}
