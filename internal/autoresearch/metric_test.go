package autoresearch

import (
	"context"
	"errors"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestParseMetric_Regex(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "regex", Pattern: `(\d+)\s+ns/op`}
	v, err := ParseMetric(p, "BenchmarkX-8   1000   5432 ns/op   24 B/op", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 5432 {
		t.Errorf("expected 5432, got %v", v)
	}
}

func TestParseMetric_Regex_NoMatch(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "regex", Pattern: `(\d+)\s+ns/op`}
	if _, err := ParseMetric(p, "no match here", 0); err == nil {
		t.Error("expected error on no-match")
	}
}

func TestParseMetric_JSONPath_Object(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "json_path", Pattern: "metrics.latency_ms"}
	v, err := ParseMetric(p, `{"metrics":{"latency_ms":42.5}}`, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 42.5 {
		t.Errorf("expected 42.5, got %v", v)
	}
}

func TestParseMetric_JSONPath_ArrayIndex(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "json_path", Pattern: "results.0.score"}
	v, err := ParseMetric(p, `{"results":[{"score":0.91},{"score":0.78}]}`, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 0.91 {
		t.Errorf("expected 0.91, got %v", v)
	}
}

func TestParseMetric_JSONPath_StringCoercion(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "json_path", Pattern: "metric"}
	v, err := ParseMetric(p, `{"metric":"3.14"}`, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3.14 {
		t.Errorf("expected 3.14, got %v", v)
	}
}

func TestParseMetric_LastFloat(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "last_float"}
	v, err := ParseMetric(p, "result: 12.5\nfinal: 7.25\n", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 7.25 {
		t.Errorf("expected 7.25, got %v", v)
	}
}

func TestParseMetric_ExitCodeInverse(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "exit_code_inverse"}
	v0, _ := ParseMetric(p, "ignored", 0)
	if v0 != 1 {
		t.Errorf("exit 0 should map to 1, got %v", v0)
	}
	v1, _ := ParseMetric(p, "ignored", 1)
	if v1 != 0 {
		t.Errorf("exit 1 should map to 0, got %v", v1)
	}
}

func TestParseMetric_UnknownParser(t *testing.T) {
	p := config.AutoresearchMetricParser{Kind: "magic"}
	if _, err := ParseMetric(p, "x", 0); err == nil {
		t.Error("expected error on unknown parser")
	}
}

func TestNearTie(t *testing.T) {
	if !nearTie(100, 100, 0.0) {
		t.Error("identical scores must be near-tie")
	}
	if !nearTie(100.5, 100, 0.01) {
		t.Error("0.5%% diff at epsilon 1%% must be near-tie")
	}
	if nearTie(110, 100, 0.05) {
		t.Error("10%% diff at epsilon 5%% must NOT be near-tie")
	}
	if !nearTie(0.1, 0, 0.5) {
		t.Error("near-zero baseline must use min-1 denominator")
	}
}

// fakeTiebreaker lets us inject deterministic nudges or errors.
type fakeTiebreaker struct {
	nudge float64
	err   error
}

func (f fakeTiebreaker) Tiebreak(ctx context.Context, _ string, _ string, _ float64, _ float64) (float64, error) {
	return f.nudge, f.err
}

func TestMetricHarness_TiebreakNudgeBoundedByEpsilon(t *testing.T) {
	h := &MetricHarness{
		Metric: config.AutoresearchMetric{
			Command:    "echo 100",
			Parser:     config.AutoresearchMetricParser{Kind: "last_float", LowerIsBetter: false},
			TieEpsilon: 0.01,
		},
		Tiebreaker: fakeTiebreaker{nudge: 1.0},
	}
	dir := t.TempDir()
	score, err := h.Measure(context.Background(), dir, 100, "diff body")
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	// nudge = +1.0, higher_is_better, epsilon = 0.01 → tiebreak nudge = +0.01
	if score.TiebreakNudge != 0.01 {
		t.Errorf("expected nudge +0.01, got %v", score.TiebreakNudge)
	}
	if score.Final != 100.01 {
		t.Errorf("expected final 100.01, got %v", score.Final)
	}
}

func TestMetricHarness_TiebreakerFailFailsClosed(t *testing.T) {
	h := &MetricHarness{
		Metric: config.AutoresearchMetric{
			Command:    "echo 100",
			Parser:     config.AutoresearchMetricParser{Kind: "last_float", LowerIsBetter: true},
			TieEpsilon: 0.01,
		},
		Tiebreaker: fakeTiebreaker{err: errors.New("boom")},
	}
	dir := t.TempDir()
	score, err := h.Measure(context.Background(), dir, 100, "diff")
	if err != nil {
		t.Fatalf("measure must not error on tiebreaker fail; got %v", err)
	}
	// lower_is_better + tiebreaker fail → push score worse (up by epsilon)
	if score.TiebreakNudge != 0.01 {
		t.Errorf("fail-closed nudge for lower_is_better must be +epsilon, got %v", score.TiebreakNudge)
	}
	if score.Improves(100) {
		t.Error("after fail-closed nudge, score must NOT improve baseline")
	}
}

// fakeLLMClient lets us script tiebreaker LLM responses for the
// LLMTiebreaker integration check.
type fakeLLMClient struct{ reply string }

func (f fakeLLMClient) Complete(ctx context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{Content: f.reply}, nil
}

func TestLLMTiebreaker_ParsesNumber(t *testing.T) {
	tb := &LLMTiebreaker{Client: fakeLLMClient{reply: "0.42"}, Model: "test"}
	v, err := tb.Tiebreak(context.Background(), "rubric", "diff", 100, 100)
	if err != nil {
		t.Fatalf("tiebreak: %v", err)
	}
	if v != 0.42 {
		t.Errorf("expected 0.42, got %v", v)
	}
}

func TestLLMTiebreaker_ClampsToRange(t *testing.T) {
	tb := &LLMTiebreaker{Client: fakeLLMClient{reply: "5.0"}, Model: "test"}
	v, _ := tb.Tiebreak(context.Background(), "", "", 0, 0)
	if v != 1.0 {
		t.Errorf("must clamp to 1.0, got %v", v)
	}
}

func TestLLMTiebreaker_NonNumericReplyErrors(t *testing.T) {
	tb := &LLMTiebreaker{Client: fakeLLMClient{reply: "I think it is better"}, Model: "test"}
	if _, err := tb.Tiebreak(context.Background(), "", "", 0, 0); err == nil {
		t.Error("non-numeric reply must error so caller can fail-closed")
	}
}
