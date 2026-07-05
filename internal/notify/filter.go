package notify

import "context"

// FilteredNotifier wraps another Notifier and forwards only messages whose
// EventType appears in the allowlist. Dropping a disallowed message is policy,
// not failure, so it returns nil. Messages with an empty EventType are also
// dropped — every emitting site must declare its event type or the operator's
// notify_on_* config flags cannot gate it.
type FilteredNotifier struct {
	inner   Notifier
	allowed map[string]bool
}

// NewFilteredNotifier wraps inner with an EventType allowlist. An empty
// allowlist drops everything; callers should prefer NewNoopNotifier in that
// case to make intent explicit.
func NewFilteredNotifier(inner Notifier, allowedEventTypes []string) *FilteredNotifier {
	allowed := make(map[string]bool, len(allowedEventTypes))
	for _, et := range allowedEventTypes {
		allowed[et] = true
	}
	return &FilteredNotifier{inner: inner, allowed: allowed}
}

// Notify forwards the message when its EventType is allowlisted, else drops it.
func (n *FilteredNotifier) Notify(ctx context.Context, msg Message) error {
	if msg.EventType == "" || !n.allowed[msg.EventType] {
		return nil
	}
	return n.inner.Notify(ctx, msg)
}

// Name returns the notifier name, wrapping the inner notifier's name.
func (n *FilteredNotifier) Name() string { return "filtered(" + n.inner.Name() + ")" }
