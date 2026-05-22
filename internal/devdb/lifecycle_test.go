package devdb_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
	"github.com/tzone85/vortex-dispatch/internal/devdb/null"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// fakeEventStore captures appended events for assertions.
type fakeEventStore struct {
	appended []state.Event
}

func (f *fakeEventStore) Append(evt state.Event) error {
	f.appended = append(f.appended, evt)
	return nil
}

func TestLifecycle_Provision_EmitsCreatedEvent(t *testing.T) {
	es := &fakeEventStore{}
	cfg := devdb.Config{
		Provider: "null",
		Template: "tpl",
	}
	lc := devdb.NewLifecycle(null.New(), es, cfg)
	worktree := t.TempDir()

	_, err := lc.Provision(context.Background(), "story-1", "myproj", worktree)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(es.appended) != 1 {
		t.Fatalf("appended events = %d, want 1", len(es.appended))
	}
	got := es.appended[0]
	if got.Type != state.EventStoryDBCreated {
		t.Errorf("event type = %v, want STORY_DB_CREATED", got.Type)
	}
	if got.StoryID != "story-1" {
		t.Errorf("story_id = %q", got.StoryID)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["provider"] != "null" {
		t.Errorf("payload.provider = %v, want null", payload["provider"])
	}
	hash, _ := payload["conn_string_hash"].(string)
	if hash == "" || hash[:7] != "sha256:" {
		t.Errorf("conn_string_hash = %q, want sha256:... prefix", hash)
	}
}

func TestLifecycle_Provision_WritesEnvFile(t *testing.T) {
	es := &fakeEventStore{}
	lc := devdb.NewLifecycle(null.New(), es, devdb.Config{Provider: "null"})
	worktree := t.TempDir()

	_, err := lc.Provision(context.Background(), "story-1", "myproj", worktree)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(worktree, ".vxd-db", "connect.env")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("connect.env not created: %v", err)
	}
}

func TestLifecycle_Provision_HashesConnString(t *testing.T) {
	want := sha256.Sum256([]byte("postgres://null@localhost:0/vxd-myproj-story-1"))
	wantHash := "sha256:" + hex.EncodeToString(want[:])

	es := &fakeEventStore{}
	lc := devdb.NewLifecycle(null.New(), es, devdb.Config{Provider: "null"})
	_, err := lc.Provision(context.Background(), "story-1", "myproj", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	_ = json.Unmarshal(es.appended[0].Payload, &payload)
	if payload["conn_string_hash"] != wantHash {
		t.Errorf("hash = %v, want %v", payload["conn_string_hash"], wantHash)
	}
}
