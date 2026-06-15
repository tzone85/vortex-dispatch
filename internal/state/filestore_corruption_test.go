package state_test

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestFileStore_CorruptLine_LoggedAndSkipped pins the line-numbered
// corruption visibility added to filestore. Prior code silently skipped
// any line that failed json.Unmarshal — a single bad row from disk
// failure or a partial truncated write would vanish, but every
// downstream consumer (projection, dashboard, resume) would compute its
// view from the partial event log without any signal.
//
// The new behaviour: log "<path>:<line>: corrupt event JSON: <err>"
// with the surviving events still returned to the caller. The check
// asserts both halves — the bad line is reported by line number, and
// the valid events flanking it are still readable.
func TestFileStore_CorruptLine_LoggedAndSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	store, err := state.NewFileStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	good1 := state.NewEvent(state.EventReqSubmitted, "agent-1", "", map[string]any{"k": 1})
	if err := store.Append(good1); err != nil {
		t.Fatalf("append good1: %v", err)
	}
	_ = store.Close()

	// Manually append a corrupt line (truncated JSON) and a third valid line.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString("{this-is-not-json\n"); err != nil {
		t.Fatalf("write corrupt line: %v", err)
	}
	_ = f.Close()

	store2, err := state.NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store2.Close()
	good2 := state.NewEvent(state.EventStoryCreated, "agent-2", "s-1", map[string]any{"k": 2})
	if err := store2.Append(good2); err != nil {
		t.Fatalf("append good2: %v", err)
	}

	// Capture log output during List.
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	events, err := store2.List(state.EventFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 surviving events, got %d", len(events))
	}

	logged := buf.String()
	if !strings.Contains(logged, ":2:") {
		t.Errorf("log = %q, expected ':2:' line-number marker for the corrupt row", logged)
	}
	if !strings.Contains(logged, "corrupt event JSON") {
		t.Errorf("log = %q, expected 'corrupt event JSON' marker", logged)
	}

	// Count must surface the bad-line log too — it scans the same file.
	buf.Reset()
	n, err := store2.Count(state.EventFilter{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("Count = %d, want 2", n)
	}
	if !strings.Contains(buf.String(), ":2:") {
		t.Errorf("Count log = %q, expected ':2:' line-number marker", buf.String())
	}
}

// TestNewEvent_MarshalFailure_StubPayload pins the marshal-failure
// stub. Prior code silently dropped data on json.Marshal error
// (payload stayed nil), so a NaN float or cycle in the data map would
// erase the event's payload with no signal. The fix substitutes a
// `_marshal_error` stub so downstream readers see the corruption
// instead of a confidently-empty payload.
func TestNewEvent_MarshalFailure_StubPayload(t *testing.T) {
	// math.NaN cannot be marshalled by encoding/json — Go returns
	// "json: unsupported value: NaN" rather than emit invalid JSON.
	bad := map[string]any{"bad": jsonUnmarshalable{}}
	evt := state.NewEvent(state.EventReqSubmitted, "a", "", bad)
	if evt.Payload == nil {
		t.Fatal("payload nil; expected marshal-error stub")
	}
	if !strings.Contains(string(evt.Payload), "_marshal_error") {
		t.Errorf("payload = %q, expected _marshal_error stub", evt.Payload)
	}
}

// jsonUnmarshalable triggers json.Marshal to fail with
// "json: unsupported type" — channels are explicitly rejected by encoding/json.
type jsonUnmarshalable struct {
	C chan int `json:"c"`
}
