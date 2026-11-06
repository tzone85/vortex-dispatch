package llm

import (
	"context"
	"testing"
)

type fakeRecorder struct {
	calls []recordedCall
}

type recordedCall struct {
	stage, reqID, storyID, model string
	in, out                      int
	est                          float64
}

func (f *fakeRecorder) RecordUsage(stage, reqID, storyID, model string, in, out int, est float64) {
	f.calls = append(f.calls, recordedCall{stage, reqID, storyID, model, in, out, est})
}

func TestMeteredClient_RecordsTaggedCalls(t *testing.T) {
	rec := &fakeRecorder{}
	inner := NewReplayClient(CompletionResponse{
		Content: "ok",
		Model:   "claude-sonnet-4-6",
		Usage:   Usage{InputTokens: 1_000_000, OutputTokens: 500_000},
	})
	client := NewMeteredClient(inner, rec, true)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Model: "claude-sonnet-4-6", Stage: "review", ReqID: "r-1", StoryID: "s-1",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d", len(rec.calls))
	}
	c := rec.calls[0]
	if c.stage != "review" || c.reqID != "r-1" || c.storyID != "s-1" || c.model != "claude-sonnet-4-6" {
		t.Errorf("attribution = %+v", c)
	}
	if c.in != 1_000_000 || c.out != 500_000 {
		t.Errorf("tokens = %d/%d", c.in, c.out)
	}
	want := 3.00 + 0.5*15.00
	if c.est < want-1e-9 || c.est > want+1e-9 {
		t.Errorf("est = %f, want %f", c.est, want)
	}
}

func TestMeteredClient_SubscriptionRecordsZeroUSD(t *testing.T) {
	rec := &fakeRecorder{}
	inner := NewReplayClient(CompletionResponse{
		Content: "ok",
		Model:   "claude-opus-4-8",
		Usage:   Usage{InputTokens: 10_000, OutputTokens: 1_000},
	})
	client := NewMeteredClient(inner, rec, false) // subscription mode

	if _, err := client.Complete(context.Background(), CompletionRequest{
		Stage: "agent", StoryID: "s-2",
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(rec.calls) != 1 || rec.calls[0].est != 0 {
		t.Fatalf("subscription call must record tokens with est_usd=0, got %+v", rec.calls)
	}
	if rec.calls[0].in != 10_000 || rec.calls[0].out != 1_000 {
		t.Errorf("token volume lost: %+v", rec.calls[0])
	}
}

func TestMeteredClient_UnstagedAndErrorsUnmetered(t *testing.T) {
	rec := &fakeRecorder{}
	client := NewMeteredClient(NewReplayClient(
		CompletionResponse{Content: "ok", Model: "m", Usage: Usage{InputTokens: 5, OutputTokens: 5}},
	), rec, true)

	// No Stage → passthrough, no recording (doc-gen / replan style calls).
	if _, err := client.Complete(context.Background(), CompletionRequest{Model: "m"}); err != nil {
		t.Fatalf("unstaged complete: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("unstaged call must not record, got %+v", rec.calls)
	}

	// Error → no recording (no work was done).
	errClient := NewMeteredClient(NewErrorClient(context.Canceled), rec, true)
	if _, err := errClient.Complete(context.Background(), CompletionRequest{Stage: "review"}); err == nil {
		t.Fatal("expected error passthrough")
	}
	if len(rec.calls) != 0 {
		t.Fatalf("failed call must not record, got %+v", rec.calls)
	}
}
