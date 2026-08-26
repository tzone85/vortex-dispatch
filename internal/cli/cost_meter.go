package cli

import (
	"log"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// costRecorder implements llm.UsageRecorder against the project stores: each
// reported completion becomes one STORY_COST_RECORDED event (appended to the
// event log and projected into the story_costs table). It is constructed once
// per `vxd resume` run and shared by every metered LLM client.
//
// Requirements are resolved from the story when the caller only knows the
// story ID (review/conflict/diagnosis paths), so per-requirement aggregation
// in `vxd metrics` and the budget cap stay complete.
type costRecorder struct {
	events state.EventStore
	proj   *state.SQLiteStore
	// priced mirrors billing.llm_costs.mode=="per_token": when true the
	// recorder prices unpriced reports (engine-side metering passes est_usd=0)
	// via the static table; subscription mode keeps est_usd=0.
	priced bool
}

// RecordUsage appends + projects one STORY_COST_RECORDED event. Best-effort:
// a metering failure logs but never fails the LLM call that produced it.
func (c *costRecorder) RecordUsage(stage, reqID, storyID, model string, inputTokens, outputTokens int, estUSD float64) {
	if c == nil || c.events == nil || c.proj == nil {
		return
	}
	if reqID == "" && storyID != "" {
		if story, err := c.proj.GetStory(storyID); err == nil {
			reqID = story.ReqID
		}
	}
	if c.priced && estUSD == 0 {
		estUSD, _ = llm.EstimateCostUSD(model, inputTokens, outputTokens)
	}
	evt := state.NewEvent(state.EventStoryCostRecorded, "metering", storyID, map[string]any{
		"story_id":      storyID,
		"req_id":        reqID,
		"stage":         stage,
		"model":         model,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"est_usd":       estUSD,
	})
	if err := c.events.Append(evt); err != nil {
		log.Printf("[metering] append STORY_COST_RECORDED for %s: %v", storyID, err)
	}
	if err := c.proj.Project(evt); err != nil {
		log.Printf("[metering] project STORY_COST_RECORDED for %s: %v", storyID, err)
	}
}
