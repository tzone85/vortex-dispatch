package engine

import (
	"fmt"
	"log"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// checkBudget enforces billing.max_usd_per_req (F2). Called once per story at
// the end of the post-execution pipeline, it sums the requirement's
// STORY_COST_RECORDED est_usd and, over the cap, emits REQ_BUDGET_EXCEEDED
// and pauses the requirement through the SAME clean-pause path as a capacity
// pause: REQ_PAUSED only — no STORY_ESCALATED, no retry-count burn. The
// operator raises billing.max_usd_per_req (or sets 0 = unlimited) and runs
// `vxd resume` to continue.
//
// A cap of 0 (the default) means unlimited and short-circuits immediately.
func (m *Monitor) checkBudget(storyID string) {
	budgetCap := m.config.Billing.MaxUSDPerReq
	if budgetCap <= 0 {
		return // unlimited
	}

	// StoryCostSummaryByReq lives on *SQLiteStore (like ListRequirementsFiltered,
	// it is deliberately not on the narrow ProjectionStore interface).
	sqlite, ok := m.projStore.(*state.SQLiteStore)
	if !ok {
		return
	}

	story, err := m.projStore.GetStory(storyID)
	if err != nil {
		log.Printf("[pipeline] budget check: get story %s: %v", storyID, err)
		return
	}

	sum, err := sqlite.StoryCostSummaryByReq(story.ReqID)
	if err != nil {
		log.Printf("[pipeline] budget check for %s: %v", story.ReqID, err)
		return
	}
	if sum.TotalEstUSD <= budgetCap {
		return
	}

	log.Printf("[pipeline] BUDGET EXCEEDED for %s: $%.4f accumulated against cap $%.2f — pausing requirement",
		story.ReqID, sum.TotalEstUSD, budgetCap)

	evt := state.NewEvent(state.EventReqBudgetExceeded, "monitor", "", map[string]any{
		"req_id":    story.ReqID,
		"spent_usd": sum.TotalEstUSD,
		"cap_usd":   budgetCap,
	})
	if err := m.eventStore.Append(evt); err != nil {
		log.Printf("[pipeline] append REQ_BUDGET_EXCEEDED for %s: %v", story.ReqID, err)
	}
	if err := m.projStore.Project(evt); err != nil {
		log.Printf("[pipeline] project REQ_BUDGET_EXCEEDED for %s: %v", story.ReqID, err)
	}

	// Clean pause — identical mechanics to a capacity pause: REQ_PAUSED flips
	// the requirement status; the escalation machine is never consulted, so
	// no tier advances and no retry budget burns.
	m.pauseRequirement(storyID, fmt.Sprintf(
		"budget cap exceeded: $%.4f accumulated against billing.max_usd_per_req=%.2f — raise the cap in vxd.yaml (or set 0 for unlimited), then 'vxd resume %s'",
		sum.TotalEstUSD, budgetCap, story.ReqID))
}
