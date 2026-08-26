package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// seedBudgetFixture creates a requirement + story and projects n cost rows of
// usdEach dollars each onto the story.
func seedBudgetFixture(t *testing.T, es state.EventStore, ps *state.SQLiteStore, reqID, storyID string, rows int, usdEach float64) {
	t.Helper()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": reqID, "title": "Budget fixture",
	})
	if err := es.Append(reqEvt); err != nil {
		t.Fatalf("append req: %v", err)
	}
	if err := ps.Project(reqEvt); err != nil {
		t.Fatalf("project req: %v", err)
	}

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", storyID, map[string]any{
		"id": storyID, "req_id": reqID, "title": "Task", "complexity": 3,
	})
	if err := es.Append(storyEvt); err != nil {
		t.Fatalf("append story: %v", err)
	}
	if err := ps.Project(storyEvt); err != nil {
		t.Fatalf("project story: %v", err)
	}

	for i := 0; i < rows; i++ {
		evt := state.NewEvent(state.EventStoryCostRecorded, "metering", storyID, map[string]any{
			"story_id":      storyID,
			"req_id":        reqID,
			"stage":         "review",
			"model":         "claude-opus-4-8",
			"input_tokens":  100_000,
			"output_tokens": 10_000,
			"est_usd":       usdEach,
		})
		if err := es.Append(evt); err != nil {
			t.Fatalf("append cost row: %v", err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project cost row: %v", err)
		}
	}
}

// TestMonitor_PausesOnBudgetExceeded pins the F2 acceptance criteria: seeded
// costs over the cap produce REQ_BUDGET_EXCEEDED + a paused requirement, and
// the escalation tier is NOT advanced (clean pause, same path as capacity).
func TestMonitor_PausesOnBudgetExceeded(t *testing.T) {
	es, ps, cleanup := newPostExecTestStores(t)
	defer cleanup()

	seedBudgetFixture(t, es, ps, "r-budget", "s-budget", 3, 4.0) // $12 total

	cfg := config.Config{
		Routing: config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Billing: config.BillingConfig{MaxUSDPerReq: 10},
	}
	m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)

	m.checkBudget("s-budget")

	exceeded, _ := es.List(state.EventFilter{Type: state.EventReqBudgetExceeded})
	if len(exceeded) != 1 {
		t.Fatalf("expected exactly 1 REQ_BUDGET_EXCEEDED, got %d", len(exceeded))
	}
	payload := state.DecodePayload(exceeded[0].Payload)
	if payload["req_id"] != "r-budget" {
		t.Errorf("REQ_BUDGET_EXCEEDED req_id = %v, want r-budget", payload["req_id"])
	}

	req, err := ps.GetRequirement("r-budget")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "paused" {
		t.Errorf("requirement status = %q, want paused", req.Status)
	}

	// Clean pause: the escalation chain must be untouched.
	escalations, _ := es.List(state.EventFilter{Type: state.EventStoryEscalated, StoryID: "s-budget"})
	if len(escalations) != 0 {
		t.Errorf("budget pause must not escalate (tier burn); got %d STORY_ESCALATED events", len(escalations))
	}
	story, _ := ps.GetStory("s-budget")
	if story.EscalationTier != 0 {
		t.Errorf("escalation tier = %d, want 0 (no advance)", story.EscalationTier)
	}
}

// TestMonitor_BudgetUnderCap_NoOp verifies the unlimited/under-cap paths stay
// silent: cap 0 (default) never checks, and spend below the cap emits nothing.
func TestMonitor_BudgetUnderCap_NoOp(t *testing.T) {
	for _, tc := range []struct {
		name string
		cap  float64
	}{{"unlimited_default", 0}, {"under_cap", 100}} {
		t.Run(tc.name, func(t *testing.T) {
			es, ps, cleanup := newPostExecTestStores(t)
			defer cleanup()

			seedBudgetFixture(t, es, ps, "r-ok", "s-ok", 2, 4.0) // $8 total

			cfg := config.Config{
				Routing: config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
				Billing: config.BillingConfig{MaxUSDPerReq: tc.cap},
			}
			m := NewMonitor(nil, nil, nil, nil, nil, cfg, es, ps)

			m.checkBudget("s-ok")

			exceeded, _ := es.List(state.EventFilter{Type: state.EventReqBudgetExceeded})
			if len(exceeded) != 0 {
				t.Errorf("expected no REQ_BUDGET_EXCEEDED, got %d", len(exceeded))
			}
			req, _ := ps.GetRequirement("r-ok")
			if req.Status == "paused" {
				t.Error("requirement must not pause under an unlimited/under-cap budget")
			}
		})
	}
}

// TestWiring_BudgetCheckRunsInPostExecutionPipeline guards the budget cap
// against the dead-wire class: checkBudget must actually be invoked from the
// post-execution pipeline, not merely exist.
func TestWiring_BudgetCheckRunsInPostExecutionPipeline(t *testing.T) {
	src, err := os.ReadFile("monitor_post_execution.go")
	if err != nil {
		t.Fatalf("read monitor_post_execution.go: %v", err)
	}
	if !strings.Contains(string(src), "m.checkBudget(storyID)") {
		t.Error("WIRING FAILURE: post-execution pipeline never calls m.checkBudget(storyID) — the budget cap is implemented but not activated")
	}
}
