package llm

import "context"

// UsageRecorder receives one report per metered, successful completion.
// Implemented by the CLI layer (store-backed costRecorder) so the llm
// package stays free of state imports.
type UsageRecorder interface {
	RecordUsage(stage, reqID, storyID, model string, inputTokens, outputTokens int, estUSD float64)
}

// MeteredClient decorates any Client and records token usage for calls that
// opt in via CompletionRequest.Stage (agent|review|planning|conflict|diagnosis).
// This is the single emission point for STORY_COST_RECORDED: every client —
// Anthropic/Google API clients that return real usage, and the Claude CLI
// subscription path — flows through here when wired in resume.go.
//
// Pricing follows the billing mode chosen at construction:
//   - priced=true (billing.llm_costs.mode=per_token): est_usd comes from the
//     static price table (unknown model → 0).
//   - priced=false (subscription): raw tokens are recorded with est_usd=0 —
//     no marginal cost, but volume stays visible in `vxd metrics`.
//
// Failed calls record nothing (no work was done), and calls without a Stage
// pass through unmetered (e.g. doc generation, replan).
type MeteredClient struct {
	inner  Client
	rec    UsageRecorder
	priced bool
}

// NewMeteredClient wraps inner so successful, Stage-tagged completions are
// reported to rec. priced selects table-based USD estimation vs subscription
// (est_usd=0) accounting.
func NewMeteredClient(inner Client, rec UsageRecorder, priced bool) *MeteredClient {
	return &MeteredClient{inner: inner, rec: rec, priced: priced}
}

// Complete delegates to the wrapped client, then reports usage on success.
func (m *MeteredClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	resp, err := m.inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	if req.Stage != "" && m.rec != nil {
		model := resp.Model
		if model == "" {
			model = req.Model
		}
		est := 0.0
		if m.priced {
			est, _ = EstimateCostUSD(model, resp.Usage.InputTokens, resp.Usage.OutputTokens)
		}
		m.rec.RecordUsage(req.Stage, req.ReqID, req.StoryID, model,
			resp.Usage.InputTokens, resp.Usage.OutputTokens, est)
	}
	return resp, nil
}
