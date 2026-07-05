package cli

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestRebuildDAG_CarriesOwnershipThroughResume pins the fix for the resume-path
// ownership gap (found while closing audit E-05): rebuildDAG previously dropped
// OwnedFiles and WaveHint when reconstructing PlannedStory from the projection,
// which silently disabled the dispatcher's overlap filtering and
// sequential-file serialization on EVERY resumed requirement.
func TestRebuildDAG_CarriesOwnershipThroughResume(t *testing.T) {
	dir := t.TempDir()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "vxd.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer ps.Close()

	stories := []state.Story{
		{ID: "s1", ReqID: "r1", Title: "Story 1", Complexity: 3,
			OwnedFiles: []string{"internal/a.go", "internal/b.go"}, WaveHint: "parallel"},
		{ID: "s2", ReqID: "r1", Title: "Story 2", Complexity: 5,
			OwnedFiles: []string{"package.json"}, WaveHint: "sequential"},
	}

	_, planned, err := rebuildDAG(ps, "r1", stories)
	if err != nil {
		t.Fatalf("rebuildDAG: %v", err)
	}
	if len(planned) != 2 {
		t.Fatalf("expected 2 planned stories, got %d", len(planned))
	}
	if !reflect.DeepEqual(planned[0].OwnedFiles, []string{"internal/a.go", "internal/b.go"}) {
		t.Errorf("planned[0].OwnedFiles = %v — ownership dropped on resume", planned[0].OwnedFiles)
	}
	if planned[0].WaveHint != "parallel" {
		t.Errorf("planned[0].WaveHint = %q, want parallel", planned[0].WaveHint)
	}
	if planned[1].WaveHint != "sequential" {
		t.Errorf("planned[1].WaveHint = %q, want sequential — sequential serialization dropped on resume", planned[1].WaveHint)
	}
}

// TestRebuildDAG_CorruptOwnershipForcesSequential pins the conservative decode
// failure mode for audit E-05: a story whose owned_files could not be decoded
// has UNKNOWN ownership, so it must run alone (sequential) rather than in
// parallel where the overlap filter cannot see its files.
func TestRebuildDAG_CorruptOwnershipForcesSequential(t *testing.T) {
	dir := t.TempDir()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "vxd.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer ps.Close()

	stories := []state.Story{
		{ID: "s1", ReqID: "r1", Title: "Corrupt ownership", Complexity: 3,
			WaveHint: "parallel", OwnedFilesCorrupt: true},
	}

	_, planned, err := rebuildDAG(ps, "r1", stories)
	if err != nil {
		t.Fatalf("rebuildDAG: %v", err)
	}
	if planned[0].WaveHint != "sequential" {
		t.Errorf("WaveHint = %q, want sequential for a story with corrupt owned_files", planned[0].WaveHint)
	}
}
