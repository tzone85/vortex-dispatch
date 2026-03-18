package state_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestSQLiteStore_ArchiveRequirement(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	// Create a requirement
	evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "r-archive-001",
		"title":       "Build feature X",
		"description": "Implement feature X end to end",
	})
	if err := db.Project(evt); err != nil {
		t.Fatalf("project req: %v", err)
	}

	// Create stories for the requirement
	for _, sid := range []string{"s-001", "s-002", "s-003"} {
		storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", sid, map[string]any{
			"id":          sid,
			"req_id":      "r-archive-001",
			"title":       "Story " + sid,
			"description": "Do something",
			"complexity":  3,
		})
		if err := db.Project(storyEvt); err != nil {
			t.Fatalf("project story %s: %v", sid, err)
		}
	}

	// Archive the requirement
	if err := db.ArchiveRequirement("r-archive-001"); err != nil {
		t.Fatalf("archive requirement: %v", err)
	}

	req, err := db.GetRequirement("r-archive-001")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "archived" {
		t.Fatalf("expected requirement status 'archived', got %s", req.Status)
	}

	// Archive stories
	if err := db.ArchiveStoriesByReq("r-archive-001"); err != nil {
		t.Fatalf("archive stories: %v", err)
	}

	stories, err := db.ListStories(state.StoryFilter{ReqID: "r-archive-001"})
	if err != nil {
		t.Fatalf("list stories: %v", err)
	}

	for _, story := range stories {
		if story.Status != "archived" {
			t.Fatalf("expected story %s status 'archived', got %s", story.ID, story.Status)
		}
	}
}

func TestSQLiteStore_ArchiveRequirement_NonExistent(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	// Archiving a non-existent requirement should not error (no rows affected)
	if err := db.ArchiveRequirement("non-existent"); err != nil {
		t.Fatalf("unexpected error archiving non-existent requirement: %v", err)
	}
}

func TestSQLiteStore_ArchiveStoriesByReq_NoStories(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	// Archiving stories for a requirement with no stories should not error
	if err := db.ArchiveStoriesByReq("no-stories-req"); err != nil {
		t.Fatalf("unexpected error archiving stories for empty requirement: %v", err)
	}
}
