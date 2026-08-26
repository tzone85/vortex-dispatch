package state

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// STORY_COST_RECORDED projection (F2 cost tracking)
// ---------------------------------------------------------------------------

func TestProject_StoryCostRecorded(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	record := func(storyID, stage, model string, in, out int, usd float64) {
		t.Helper()
		evt := NewEvent(EventStoryCostRecorded, "metering", storyID, map[string]any{
			"story_id":      storyID,
			"req_id":        "r-cost",
			"stage":         stage,
			"model":         model,
			"input_tokens":  in,
			"output_tokens": out,
			"est_usd":       usd,
		})
		if err := s.Project(evt); err != nil {
			t.Fatalf("project STORY_COST_RECORDED (%s): %v", stage, err)
		}
	}

	record("s-cost-1", "review", "claude-sonnet-4-6", 1000, 500, 0.0105)
	record("s-cost-1", "review", "claude-sonnet-4-6", 2000, 100, 0.0075)
	record("s-cost-2", "planning", "claude-opus-4-8", 800, 400, 0.042)

	sum, err := s.StoryCostSummaryByReq("r-cost")
	if err != nil {
		t.Fatalf("StoryCostSummaryByReq: %v", err)
	}
	if sum.TotalInputTokens != 3800 {
		t.Errorf("TotalInputTokens = %d, want 3800", sum.TotalInputTokens)
	}
	if sum.TotalOutputTokens != 1000 {
		t.Errorf("TotalOutputTokens = %d, want 1000", sum.TotalOutputTokens)
	}
	if math.Abs(sum.TotalEstUSD-0.06) > 1e-9 {
		t.Errorf("TotalEstUSD = %f, want 0.06", sum.TotalEstUSD)
	}
	rev := sum.ByStage["review"]
	if rev.InputTokens != 3000 || rev.OutputTokens != 600 {
		t.Errorf("review stage = %+v, want in=3000 out=600", rev)
	}
	plan := sum.ByStage["planning"]
	if plan.InputTokens != 800 || plan.OutputTokens != 400 {
		t.Errorf("planning stage = %+v, want in=800 out=400", plan)
	}
	if math.Abs(sum.ByStage["review"].EstUSD-0.018) > 1e-9 {
		t.Errorf("review est_usd = %f, want 0.018", sum.ByStage["review"].EstUSD)
	}

	// Unknown requirement aggregates to zero, not an error.
	empty, err := s.StoryCostSummaryByReq("r-nonexistent")
	if err != nil {
		t.Fatalf("empty summary: %v", err)
	}
	if empty.TotalInputTokens != 0 || empty.TotalEstUSD != 0 {
		t.Errorf("empty summary = %+v, want zeros", empty)
	}
}

// TestProject_ReqBudgetExceeded_Handled pins the explicit projector case:
// REQ_BUDGET_EXCEEDED is informational (the accompanying REQ_PAUSED performs
// the status transition) but must NOT fall through to the default-WARNING
// branch.
func TestProject_ReqBudgetExceeded_Handled(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(dir + "/test.db")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	evt := NewEvent(EventReqBudgetExceeded, "monitor", "", map[string]any{
		"req_id":    "r-budget",
		"spent_usd": 12.5,
		"cap_usd":   10.0,
	})
	if err := s.Project(evt); err != nil {
		t.Fatalf("project REQ_BUDGET_EXCEEDED: %v", err)
	}
}
