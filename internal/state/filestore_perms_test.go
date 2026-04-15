package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestFileStore_CreatesWithRestrictedPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	fs, err := state.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}
