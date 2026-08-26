package llm

// Pricing holds per-million-token (MTok) USD rates for a model.
type Pricing struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// modelPricing is the static price table used to convert raw token usage
// from the API clients (Anthropic, Google AI) into an estimated USD cost.
// Rates are per million tokens, USD. Unknown models are UNPRICED: they
// contribute token volume but zero estimated cost, and callers can detect
// the gap via the boolean return.
var modelPricing = map[string]Pricing{
	"claude-opus-4-8":   {InputPerMTok: 15.00, OutputPerMTok: 75.00},
	"claude-opus-4-7":   {InputPerMTok: 15.00, OutputPerMTok: 75.00},
	"claude-sonnet-4-6": {InputPerMTok: 3.00, OutputPerMTok: 15.00},
	"claude-haiku-4-5":  {InputPerMTok: 0.80, OutputPerMTok: 4.00},
	"gemini-2.5-pro":    {InputPerMTok: 1.25, OutputPerMTok: 10.00},
	"gemini-2.5-flash":  {InputPerMTok: 0.30, OutputPerMTok: 2.50},
}

// ModelPrice returns the per-MTok pricing for a model. The boolean result is
// false for models absent from the table ("unpriced") — including the empty
// model ID — in which case the returned Pricing is the zero value.
func ModelPrice(model string) (Pricing, bool) {
	p, ok := modelPricing[model]
	return p, ok
}

// EstimateCostUSD converts raw token counts into an estimated USD cost using
// the static price table. For an unknown model it returns (0, false): the
// caller records the token volume (so spend visibility is never lost) but a
// zero estimate, flagged unpriced.
func EstimateCostUSD(model string, inputTokens, outputTokens int) (float64, bool) {
	p, ok := ModelPrice(model)
	if !ok {
		return 0, false
	}
	cost := float64(inputTokens)/1_000_000*p.InputPerMTok +
		float64(outputTokens)/1_000_000*p.OutputPerMTok
	return cost, true
}
