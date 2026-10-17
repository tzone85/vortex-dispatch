package devdb_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/null"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// projectingAppender forwards every appended event straight into a real
// SQLiteStore projection, mirroring the production append -> project path. It
// lets the test prove the END-TO-END wiring (emitter payload -> projector
// UPDATE) rather than a hand-built event that could silently supply a field
// the real emitter drops.
type projectingAppender struct {
	store *state.SQLiteStore
}

func (p projectingAppender) Append(evt state.Event) error {
	return p.store.Project(evt)
}

// TestLifecycle_Release_ProjectsDeletedStatus is a regression test for the
// STORY_DB_DELETED projection gap. Release used to emit the event WITHOUT a
// StoryID, so projectStoryDBDeleted's "WHERE story_id = ? AND db_id = ?"
// matched zero rows: the SQLite projection stayed at status="created" forever
// even though the event log recorded the DB as deleted. Because story_databases
// has PRIMARY KEY (story_id, db_id), the delete event must carry the same story
// ID that STORY_DB_CREATED used. Driving the real emitter end-to-end (not a
// hand-built event) is what catches an emitter that omits the story ID.
func TestLifecycle_Release_ProjectsDeletedStatus(t *testing.T) {
	store, err := state.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	const storyID = "s1"
	ctx := context.Background()
	lc := devdb.NewLifecycle(null.New(), projectingAppender{store}, devdb.Config{Provider: "null"})

	// Provision emits+projects STORY_DB_CREATED, inserting the row keyed by
	// (storyID, db.ID).
	db, err := lc.Provision(ctx, storyID, "myproj", t.TempDir())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if got, _ := store.StoryDBStatusAll(); got[storyID] != "created" {
		t.Fatalf("after Provision, status = %q, want created", got[storyID])
	}

	// Release emits+projects STORY_DB_DELETED for the same DB.
	if err := lc.Release(ctx, storyID, db, devdb.OutcomeSuccess); err != nil {
		t.Fatalf("Release: %v", err)
	}

	got, err := store.StoryDBStatusAll()
	if err != nil {
		t.Fatalf("StoryDBStatusAll: %v", err)
	}
	if got[storyID] != "deleted" {
		t.Fatalf("after Release, status = %q, want deleted (STORY_DB_DELETED did not project onto the created row — the delete event is missing its StoryID)", got[storyID])
	}
}
