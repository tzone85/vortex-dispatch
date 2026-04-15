package state_test

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestSQLiteStore_HasIndexes(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	db, err := sql.Open("sqlite3", dir+"/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	expectedIndexes := []string{
		"idx_stories_req_id",
		"idx_stories_status",
		"idx_story_deps_story_id",
		"idx_escalations_story_id",
		"idx_agent_scores_agent_id",
	}

	for _, idx := range expectedIndexes {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&name)
		if err != nil {
			t.Errorf("index %q not found: %v", idx, err)
		}
	}
}
