package llm

import (
	"math"
	"testing"
)

// TestPricing_KnownAndUnknownModels pins the static price table: known
// Anthropic/Google models price positively on both directions, and unknown
// models are flagged unpriced with a zero estimate (token volume is still
// recorded upstream — only the USD estimate degrades).
func TestPricing_KnownAndUnknownModels(t *testing.T) {
	known := []string{
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
	}
	for _, model := range known {
		p, ok := ModelPrice(model)
		if !ok {
			t.Errorf("ModelPrice(%q) reported unpriced; want known", model)
			continue
		}
		if p.InputPerMTok <= 0 || p.OutputPerMTok <= 0 {
			t.Errorf("ModelPrice(%q) = %+v; want positive per-MTok rates", model, p)
		}
	}

	unknown := []string{"", "gpt-99", "claude-opus-3-legacy", "gemini-9.9-ultra"}
	for _, model := range unknown {
		cost, ok := EstimateCostUSD(model, 1_000_000, 1_000_000)
		if ok {
			t.Errorf("EstimateCostUSD(%q) reported priced; want unpriced", model)
		}
		if cost != 0 {
			t.Errorf("EstimateCostUSD(%q) = %f; want 0 for unpriced model", model, cost)
		}
	}

	// Spot-check arithmetic: sonnet at $3/$15 per MTok.
	cost, ok := EstimateCostUSD("claude-sonnet-4-6", 1_000_000, 500_000)
	if !ok {
		t.Fatal("sonnet should be priced")
	}
	want := 3.00 + 0.5*15.00
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("EstimateCostUSD(sonnet, 1M in, 0.5M out) = %f; want %f", cost, want)
	}
}
