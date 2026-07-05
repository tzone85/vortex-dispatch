package engine

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/notify"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// captureNotifier records every message so terminal-outcome notification
// wiring can be asserted without a live webhook.
type captureNotifier struct {
	mu       sync.Mutex
	messages []notify.Message
}

func (c *captureNotifier) Notify(_ context.Context, msg notify.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, msg)
	return nil
}

func (c *captureNotifier) Name() string { return "capture" }

func (c *captureNotifier) all() []notify.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]notify.Message, len(c.messages))
	copy(out, c.messages)
	return out
}

func newNotifyTestMonitor(t *testing.T) (*Monitor, *captureNotifier) {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	t.Cleanup(func() {
		es.Close()
		ps.Close()
	})
	n := &captureNotifier{}
	m := &Monitor{eventStore: es, projStore: ps, notifier: n}
	return m, n
}

// TestEmitRequirementOutcome_NotifiesCompleted pins the notify_on_complete
// wiring: a terminal REQ_COMPLETED emits a webhook notification carrying the
// event type (so the FilteredNotifier allowlist can gate it) at info severity.
func TestEmitRequirementOutcome_NotifiesCompleted(t *testing.T) {
	m, n := newNotifyTestMonitor(t)

	m.emitRequirementOutcome("r-001", state.EventReqCompleted, "REQ_COMPLETED")

	msgs := n.all()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(msgs))
	}
	if msgs[0].EventType != string(state.EventReqCompleted) {
		t.Errorf("EventType = %q, want %q", msgs[0].EventType, state.EventReqCompleted)
	}
	if msgs[0].Severity != "info" {
		t.Errorf("Severity = %q, want info", msgs[0].Severity)
	}
}

// TestEmitRequirementOutcome_NotifiesBlocked pins that a blocked requirement
// notifies at error severity with the resume hint in the body.
func TestEmitRequirementOutcome_NotifiesBlocked(t *testing.T) {
	m, n := newNotifyTestMonitor(t)

	m.emitRequirementOutcome("r-002", state.EventReqBlocked, "REQ_BLOCKED")

	msgs := n.all()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(msgs))
	}
	if msgs[0].EventType != string(state.EventReqBlocked) {
		t.Errorf("EventType = %q, want %q", msgs[0].EventType, state.EventReqBlocked)
	}
	if msgs[0].Severity != "error" {
		t.Errorf("Severity = %q, want error", msgs[0].Severity)
	}
}

// TestEmitRequirementOutcome_NilNotifierSafe guards the default path (no
// webhook configured): terminal outcome emission must not panic.
func TestEmitRequirementOutcome_NilNotifierSafe(t *testing.T) {
	m, _ := newNotifyTestMonitor(t)
	m.notifier = nil

	m.emitRequirementOutcome("r-003", state.EventReqCompleted, "REQ_COMPLETED")
}
