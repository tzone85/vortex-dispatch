package state

import "testing"

// TestProject_StoryCostRecorded_Idempotent proves that re-projecting the same
// STORY_COST_RECORDED event does not double-count. The row is keyed on the
// event's own ULID (evt.ID) and inserted with INSERT OR IGNORE, so a second
// projection of the identical event is a no-op. Keying on a freshly minted
// random ULID (the prior behaviour) made the OR IGNORE useless — every
// re-projection inserted a new row, double-counting tokens/USD that feed the
// billing.max_usd_per_req budget cap.
func TestProject_StoryCostRecorded_Idempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	evt := NewEvent(EventStoryCostRecorded, "metering", "s-cost", map[string]any{
		"story_id":      "s-cost",
		"req_id":        "r-idem",
		"stage":         "review",
		"model":         "claude-sonnet-4-6",
		"input_tokens":  1000,
		"output_tokens": 500,
		"est_usd":       0.01,
	})

	// Project the identical event twice (same evt.ID).
	if err := s.Project(evt); err != nil {
		t.Fatalf("first project: %v", err)
	}
	if err := s.Project(evt); err != nil {
		t.Fatalf("second project: %v", err)
	}

	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM story_costs WHERE req_id = ?`, "r-idem").Scan(&rows); err != nil {
		t.Fatalf("count story_costs: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected exactly 1 cost row after re-projecting the same event, got %d", rows)
	}

	sum, err := s.StoryCostSummaryByReq("r-idem")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.TotalInputTokens != 1000 {
		t.Fatalf("re-projection double-counted: TotalInputTokens = %d, want 1000", sum.TotalInputTokens)
	}
}

// TestProject_StoryEscalated_Idempotent proves the escalation projector is
// likewise deterministic and idempotent: re-projecting the same STORY_ESCALATED
// event does not insert a duplicate escalation row, because the row is keyed on
// evt.ID via INSERT OR IGNORE rather than a random ULID.
func TestProject_StoryEscalated_Idempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	evt := NewEvent(EventStoryEscalated, "monitor", "s-esc", map[string]any{
		"from_tier": 0,
		"to_tier":   1,
		"reason":    "review rejected",
	})

	if err := s.Project(evt); err != nil {
		t.Fatalf("first project: %v", err)
	}
	if err := s.Project(evt); err != nil {
		t.Fatalf("second project: %v", err)
	}

	escalations, err := s.ListEscalations()
	if err != nil {
		t.Fatalf("list escalations: %v", err)
	}
	if len(escalations) != 1 {
		t.Fatalf("expected exactly 1 escalation row after re-projecting the same event, got %d", len(escalations))
	}
	if escalations[0].ID != evt.ID {
		t.Fatalf("escalation row not keyed on event ULID: got %q, want %q", escalations[0].ID, evt.ID)
	}
}
