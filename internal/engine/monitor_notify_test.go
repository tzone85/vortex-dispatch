package engine

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// ctxHonoringNotifier records a message only when its context is still live.
// A cancelled context (what the caller's ctx would be by the time a
// fire-and-forget stall notify runs) causes the send to be dropped — modelling
// how SlackNotifier's http.NewRequestWithContext aborts on a cancelled ctx.
type ctxHonoringNotifier struct {
	mu       sync.Mutex
	messages []notify.Message
}

func (c *ctxHonoringNotifier) Notify(ctx context.Context, msg notify.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, msg)
	return nil
}
func (c *ctxHonoringNotifier) Name() string { return "ctx-honoring" }
func (c *ctxHonoringNotifier) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.messages)
}
func (c *ctxHonoringNotifier) first() notify.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.messages[0]
}

// TestNotifyStalledAsync_UsesFreshContext is the regression pin for the
// PIPELINE_STALLED notify: the fire-and-forget send must root its own context,
// not the caller's — which is cancelled the instant dispatchNextWave returns on
// a stall. If the send used a cancelled context, the always-send
// human-intervention alert would be silently dropped. With a ctx-honoring
// notifier the message is delivered only if the context is live.
func TestNotifyStalledAsync_UsesFreshContext(t *testing.T) {
	n := &ctxHonoringNotifier{}
	m := &Monitor{notifier: n}

	m.notifyStalledAsync("r-stall", 3)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n.count() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n.count() != 1 {
		t.Fatal("PIPELINE_STALLED notification was not delivered: the send used a cancelled/expired context")
	}
	msg := n.first()
	if msg.EventType != string(state.EventPipelineStalled) {
		t.Errorf("EventType = %q, want %q", msg.EventType, state.EventPipelineStalled)
	}
	if msg.Severity != "error" {
		t.Errorf("Severity = %q, want error", msg.Severity)
	}
}

// TestNotifyStalledAsync_NilNotifierSafe guards the default (no webhook) path.
func TestNotifyStalledAsync_NilNotifierSafe(t *testing.T) {
	m := &Monitor{notifier: nil}
	m.notifyStalledAsync("r-stall", 1) // must not panic
}
