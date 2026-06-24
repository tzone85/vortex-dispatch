package engine

import (
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestCurrentTier_RespectsReset pins the transient-failure recovery fix: a
// STORY_RESET event zeroes the effective escalation tier, so a story whose tiers
// were exhausted by a transient cause (e.g. a 429 storm) is no longer
// permanently pinned and re-pausing at tier 4 forever.
func TestCurrentTier_RespectsReset(t *testing.T) {
	fs := testEscalationStore(t)
	esc := NewEscalationMachine(fs, defaultRoutingConfig())

	base := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	appendAt := func(typ state.EventType, payload map[string]any, ts time.Time) {
		evt := state.NewEvent(typ, "", "s-001", payload)
		evt.Timestamp = ts
		if err := fs.Append(evt); err != nil {
			t.Fatal(err)
		}
	}

	// Escalate all the way to tier 4 (paused).
	appendAt(state.EventStoryEscalated, map[string]any{"from_tier": 0, "to_tier": 1}, base)
	appendAt(state.EventStoryEscalated, map[string]any{"from_tier": 1, "to_tier": 2}, base.Add(time.Minute))
	appendAt(state.EventStoryEscalated, map[string]any{"from_tier": 2, "to_tier": 3}, base.Add(2*time.Minute))
	appendAt(state.EventStoryEscalated, map[string]any{"from_tier": 3, "to_tier": 4}, base.Add(3*time.Minute))

	if tier, _ := esc.CurrentTier("s-001"); tier != 4 {
		t.Fatalf("precondition: expected tier 4, got %d", tier)
	}

	// Operator resets the story (transient cause cleared).
	appendAt(state.EventStoryReset, map[string]any{"reason": "transient capacity failure cleared"}, base.Add(10*time.Minute))

	if tier, err := esc.CurrentTier("s-001"); err != nil || tier != 0 {
		t.Fatalf("after reset: expected tier 0, got %d (err %v)", tier, err)
	}

	// A fresh escalation after the reset counts again from there.
	appendAt(state.EventStoryEscalated, map[string]any{"from_tier": 0, "to_tier": 1}, base.Add(20*time.Minute))
	if tier, _ := esc.CurrentTier("s-001"); tier != 1 {
		t.Errorf("after reset + one escalation: expected tier 1, got %d", tier)
	}
}
