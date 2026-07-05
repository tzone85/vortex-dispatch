package notify

import (
	"context"
	"testing"
)

// recordingNotifier captures forwarded messages so filter behavior can be
// asserted without a live webhook.
type recordingNotifier struct {
	messages []Message
}

func (r *recordingNotifier) Notify(_ context.Context, msg Message) error {
	r.messages = append(r.messages, msg)
	return nil
}

func (r *recordingNotifier) Name() string { return "recording" }

func TestFilteredNotifier_ForwardsAllowedEventTypes(t *testing.T) {
	inner := &recordingNotifier{}
	f := NewFilteredNotifier(inner, []string{"STORY_SLA_BREACHED", "REQ_COMPLETED"})

	if err := f.Notify(context.Background(), Message{Title: "sla", EventType: "STORY_SLA_BREACHED"}); err != nil {
		t.Fatalf("Notify allowed event: %v", err)
	}
	if err := f.Notify(context.Background(), Message{Title: "done", EventType: "REQ_COMPLETED"}); err != nil {
		t.Fatalf("Notify allowed event: %v", err)
	}

	if len(inner.messages) != 2 {
		t.Fatalf("expected 2 forwarded messages, got %d", len(inner.messages))
	}
	if inner.messages[0].EventType != "STORY_SLA_BREACHED" || inner.messages[1].EventType != "REQ_COMPLETED" {
		t.Errorf("forwarded messages out of order or mangled: %+v", inner.messages)
	}
}

func TestFilteredNotifier_DropsDisallowedEventTypes(t *testing.T) {
	inner := &recordingNotifier{}
	f := NewFilteredNotifier(inner, []string{"STORY_SLA_BREACHED"})

	// Disallowed type: dropped silently, no error (drop is policy, not failure).
	if err := f.Notify(context.Background(), Message{Title: "done", EventType: "REQ_COMPLETED"}); err != nil {
		t.Fatalf("dropping a disallowed event must not error: %v", err)
	}
	// Empty EventType: also dropped — every emitting site must declare its type.
	if err := f.Notify(context.Background(), Message{Title: "untyped"}); err != nil {
		t.Fatalf("dropping an untyped event must not error: %v", err)
	}

	if len(inner.messages) != 0 {
		t.Fatalf("expected 0 forwarded messages, got %d: %+v", len(inner.messages), inner.messages)
	}
}

func TestFilteredNotifier_EmptyAllowlistDropsEverything(t *testing.T) {
	inner := &recordingNotifier{}
	f := NewFilteredNotifier(inner, nil)

	if err := f.Notify(context.Background(), Message{Title: "x", EventType: "REQ_COMPLETED"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(inner.messages) != 0 {
		t.Fatalf("expected 0 forwarded messages, got %d", len(inner.messages))
	}
}

func TestFilteredNotifier_NameWrapsInner(t *testing.T) {
	f := NewFilteredNotifier(&recordingNotifier{}, nil)
	if got := f.Name(); got != "filtered(recording)" {
		t.Errorf("Name() = %q, want %q", got, "filtered(recording)")
	}
}
