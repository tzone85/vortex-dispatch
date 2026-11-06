package state_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestSQLiteStore_HasIndexes: every index the schema declares is created.
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

// indexFailureDB writes a database whose schema NewSQLiteStore can apply but
// whose index step cannot: a TABLE already carries the name one of the
// indexes wants, so CREATE INDEX IF NOT EXISTS fails with "there is already
// a table named idx_stories_req_id".
func indexFailureDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vxd.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE idx_stories_req_id (x TEXT)`); err != nil {
		t.Fatalf("seed the colliding name: %v", err)
	}
	return dbPath
}

// TestNewSQLiteStore_IndexFailure_ReturnsTheError: the index step is a real
// failure path, not an unreachable one.
func TestNewSQLiteStore_IndexFailure_ReturnsTheError(t *testing.T) {
	ps, err := state.NewSQLiteStore(indexFailureDB(t))
	if err == nil {
		_ = ps.Close()
		t.Fatal("a name collision on an index must fail the open")
	}
	if ps != nil {
		t.Fatalf("no store on error, got %#v", ps)
	}
	if got := err.Error(); !strings.Contains(got, "create index") {
		t.Fatalf("the error must say which step failed, got %q", got)
	}
}

// TestNewSQLiteStore_IndexFailure_ClosesTheHandle: the caller gets no store,
// so nothing can close the handle sql.Open left behind. Twenty failed opens
// that each leaked one would show as twenty more descriptors; the slack is
// for whatever else the test binary opens meanwhile.
//
// The count is process-wide, so this holds only while nothing in this package
// runs with t.Parallel(). The first test here that does needs a per-test
// measure instead.
func TestNewSQLiteStore_IndexFailure_ClosesTheHandle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("/dev/fd is a unix interface")
	}
	dbPath := indexFailureDB(t)
	before := openDescriptors(t)
	const opens = 20
	for i := 0; i < opens; i++ {
		if ps, err := state.NewSQLiteStore(dbPath); err == nil {
			_ = ps.Close()
			t.Fatal("the open must keep failing")
		}
	}
	if after := openDescriptors(t); after-before >= opens/2 {
		t.Fatalf("%d failed opens leaked their handles: %d -> %d descriptors", opens, before, after)
	}
}

func openDescriptors(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("no /dev/fd here: %v", err)
	}
	return len(ents)
}
