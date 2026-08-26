package engine

import (
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestMetrics_ShowsCostSection pins the F2 metrics surface: a requirement with
// projected STORY_COST_RECORDED rows renders a Cost section in `vxd metrics`
// with total tokens, estimated USD, and the per-stage breakdown.
func TestMetrics_ShowsCostSection(t *testing.T) {
	es, ps, cleanup := newPostExecTestStores(t)
	defer cleanup()

	seedBudgetFixture(t, es, ps, "r-cost-metrics", "s-cost-metrics", 2, 4.0)

	// A second stage so the breakdown renders more than one line.
	planEvt := state.NewEvent(state.EventStoryCostRecorded, "metering", "s-cost-metrics", map[string]any{
		"story_id":      "s-cost-metrics",
		"req_id":        "r-cost-metrics",
		"stage":         "planning",
		"model":         "claude-opus-4-8",
		"input_tokens":  50_000,
		"output_tokens": 5_000,
		"est_usd":       1.25,
	})
	if err := es.Append(planEvt); err != nil {
		t.Fatalf("append planning cost: %v", err)
	}
	if err := ps.Project(planEvt); err != nil {
		t.Fatalf("project planning cost: %v", err)
	}

	m, err := ComputeMetrics(es, ps, 10, "")
	if err != nil {
		t.Fatalf("compute metrics: %v", err)
	}
	out := FormatMetrics(m)

	if !strings.Contains(out, "Cost:") {
		t.Error("metrics output missing Cost section header")
	}
	if !strings.Contains(out, "$9.2500") {
		t.Errorf("metrics output missing total est USD ($9.25 = 8.00 + 1.25); got:\n%s", out)
	}
	if !strings.Contains(out, "250000 in") {
		t.Errorf("metrics output missing total input tokens (200k review + 50k planning); got:\n%s", out)
	}
	if !strings.Contains(out, "review") || !strings.Contains(out, "planning") {
		t.Errorf("metrics output missing per-stage breakdown lines; got:\n%s", out)
	}

	// Requirements without cost rows must not render an empty Cost section.
	es2, ps2, cleanup2 := newPostExecTestStores(t)
	defer cleanup2()
	seedBudgetFixture(t, es2, ps2, "r-nocost", "s-nocost", 0, 0)

	m2, err := ComputeMetrics(es2, ps2, 10, "")
	if err != nil {
		t.Fatalf("compute metrics (no costs): %v", err)
	}
	out2 := FormatMetrics(m2)
	if strings.Contains(out2, "Cost:") {
		t.Error("Cost section must be omitted when a requirement has no cost rows")
	}
}
