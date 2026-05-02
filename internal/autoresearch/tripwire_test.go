package autoresearch

import (
	"context"
	"errors"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

type scriptedClient struct {
	reply string
	err   error
}

func (s scriptedClient) Complete(ctx context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	if s.err != nil {
		return llm.CompletionResponse{}, s.err
	}
	return llm.CompletionResponse{Content: s.reply}, nil
}

func TestTripwireJudge_OK(t *testing.T) {
	j := &TripwireJudge{Client: scriptedClient{reply: "OK|seems fine"}, Model: "test"}
	v, reason, err := j.Judge(context.Background(), "diff", 100, 110, Conventions{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != VerdictOK {
		t.Errorf("expected OK, got %s", v)
	}
	if reason != "seems fine" {
		t.Errorf("reason: %q", reason)
	}
}

func TestTripwireJudge_Rejected(t *testing.T) {
	j := &TripwireJudge{Client: scriptedClient{reply: "REJECTED|deletes failing tests"}, Model: "test"}
	v, _, _ := j.Judge(context.Background(), "diff", 100, 110, Conventions{})
	if v != VerdictRejected {
		t.Errorf("expected REJECTED, got %s", v)
	}
}

func TestTripwireJudge_LLMErrorFailsClosed(t *testing.T) {
	j := &TripwireJudge{Client: scriptedClient{err: errors.New("provider down")}, Model: "test"}
	v, _, err := j.Judge(context.Background(), "diff", 100, 110, Conventions{})
	if err == nil {
		t.Error("expected upstream error to be surfaced")
	}
	if v != VerdictSuspicious {
		t.Errorf("FAIL-CLOSED VIOLATION: LLM error must yield SUSPICIOUS, got %s", v)
	}
}

func TestTripwireJudge_NilClientFailsClosed(t *testing.T) {
	j := &TripwireJudge{}
	v, _, err := j.Judge(context.Background(), "diff", 100, 110, Conventions{})
	if err == nil {
		t.Error("nil client must surface error")
	}
	if v != VerdictSuspicious {
		t.Errorf("FAIL-CLOSED VIOLATION: nil client must yield SUSPICIOUS, got %s", v)
	}
}

func TestTripwireJudge_UnparseableReplyFailsClosed(t *testing.T) {
	j := &TripwireJudge{Client: scriptedClient{reply: "I have analyzed the diff and..."}, Model: "test"}
	v, _, _ := j.Judge(context.Background(), "diff", 100, 110, Conventions{})
	if v != VerdictSuspicious {
		t.Errorf("FAIL-CLOSED VIOLATION: unparseable reply must yield SUSPICIOUS, got %s", v)
	}
}

func TestParseTripwireReply_StripsCodeFences(t *testing.T) {
	v, reason := parseTripwireReply("```OK|all good```")
	if v != VerdictOK {
		t.Errorf("expected OK, got %s", v)
	}
	if reason != "all good" {
		t.Errorf("reason: %q", reason)
	}
}

func TestParseTripwireReply_TakesFirstLineOnly(t *testing.T) {
	reply := "REJECTED|deleted test\nfurther explanation here\nand more"
	v, reason := parseTripwireReply(reply)
	if v != VerdictRejected || reason != "deleted test" {
		t.Errorf("got verdict=%s reason=%q", v, reason)
	}
}

func TestParseTripwireReply_NoPipe(t *testing.T) {
	v, _ := parseTripwireReply("OK")
	if v != VerdictOK {
		t.Error("OK without pipe should still parse")
	}
}
