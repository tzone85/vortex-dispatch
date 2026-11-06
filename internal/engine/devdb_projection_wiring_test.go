package engine_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/null"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// projectingAppender forwards every appended event straight into a real
// SQLiteStore projection AND records it, so a test can exercise the live
// emitter->projector path and then replay the captured log elsewhere.
type projectingAppender struct {
	store  *state.SQLiteStore
	events []state.Event
}

func (p *projectingAppender) Append(e state.Event) error {
	p.events = append(p.events, e)
	return p.store.Project(e)
}

// TestDevDBReleaseProjectsThroughToDeletedRow is an end-to-end regression guard
// for the STORY_DB_DELETED wiring bug: devdb.Lifecycle.Release emitted the
// delete event with an empty StoryID, so the projector's
// `UPDATE story_databases ... WHERE story_id = ? AND db_id = ?` matched zero
// rows and the DB was reported "created"/active forever (on the live
// projection and on replay). The earlier projection unit tests missed it
// because they hand-built the delete event with a StoryID the real emitter
// never set.
//
// This test drives the REAL Lifecycle (null provider) through Provision +
// Release, projects the events it actually emits into a real SQLiteStore, and
// asserts the DB reads back as deleted with its duration accrued. It then
// replays the captured event log into a second fresh store and asserts the
// projection is identical — the determinism guarantee the event-sourced model
// requires.
func TestDevDBReleaseProjectsThroughToDeletedRow(t *testing.T) {
	const reqID, storyID = "r-dbwire", "s-dbwire"

	newStoreWithStory := func(t *testing.T) *state.SQLiteStore {
		t.Helper()
		store, err := state.NewSQLiteStore(filepath.Join(t.TempDir(), "proj.db"))
		if err != nil {
			t.Fatalf("new store: %v", err)
		}
		t.Cleanup(func() { store.Close() })
		// A story row is required for the req-scoped readers' JOIN.
		storyEvt := state.NewEvent(state.EventStoryCreated, "tl", storyID, map[string]any{
			"id": storyID, "req_id": reqID, "title": "wire", "complexity": 2,
		})
		if err := store.Project(storyEvt); err != nil {
			t.Fatalf("project story: %v", err)
		}
		return store
	}

	store := newStoreWithStory(t)
	app := &projectingAppender{store: store}
	lc := devdb.NewLifecycle(null.New(), app, devdb.Config{Provider: "null"})

	ctx := context.Background()
	db, err := lc.Provision(ctx, storyID, "proj", t.TempDir())
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	// Before release the DB is active.
	if status, _ := store.StoryDBStatusByReq(reqID); status[storyID] != "created" {
		t.Fatalf("pre-release status = %q, want created", status[storyID])
	}

	if err := lc.Release(ctx, storyID, db, devdb.OutcomeSuccess); err != nil {
		t.Fatalf("release: %v", err)
	}

	// The core assertion: the delete event joined back to the created row.
	status, err := store.StoryDBStatusByReq(reqID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status[storyID] != "deleted" {
		t.Fatalf("post-release status = %q, want deleted (delete event did not join to the created row)", status[storyID])
	}
	metrics, err := store.StoryDBMetricsByReq(reqID)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.DeletedDBs != 1 || metrics.ActiveDBs != 0 {
		t.Fatalf("metrics = %+v, want DeletedDBs=1 ActiveDBs=0", metrics)
	}

	// Determinism: replaying the exact captured event log into a fresh store
	// must reproduce the same projection.
	replay := newStoreWithStory(t)
	for _, e := range app.events {
		if err := replay.Project(e); err != nil {
			t.Fatalf("replay project %s: %v", e.Type, err)
		}
	}
	replayStatus, err := replay.StoryDBStatusByReq(reqID)
	if err != nil {
		t.Fatalf("replay status: %v", err)
	}
	if replayStatus[storyID] != status[storyID] {
		t.Fatalf("replay status = %q, want %q (non-deterministic projection)", replayStatus[storyID], status[storyID])
	}
	replayMetrics, err := replay.StoryDBMetricsByReq(reqID)
	if err != nil {
		t.Fatalf("replay metrics: %v", err)
	}
	if replayMetrics.DeletedDBs != metrics.DeletedDBs || replayMetrics.ActiveDBs != metrics.ActiveDBs {
		t.Fatalf("replay metrics = %+v, want %+v", replayMetrics, metrics)
	}
}
