package llm_test

import (
	"context"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestFallbackClient_ImplementsClientInterface(t *testing.T) {
	primary := llm.NewReplayClient(llm.CompletionResponse{Content: "ok"})
	fallback := llm.NewReplayClient(llm.CompletionResponse{Content: "fallback"})
	var _ llm.Client = llm.NewFallbackClient(primary, fallback)
}

func TestFallbackClient_UsesPrimaryOnSuccess(t *testing.T) {
	primary := llm.NewReplayClient(llm.CompletionResponse{Content: "primary"})
	fallback := llm.NewReplayClient(llm.CompletionResponse{Content: "fallback"})
	client := llm.NewFallbackClient(primary, fallback)

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "primary" {
		t.Errorf("expected 'primary', got %q", resp.Content)
	}
	if fallback.CallCount() != 0 {
		t.Errorf("fallback should not have been called, got %d calls", fallback.CallCount())
	}
}

func TestFallbackClient_FallsBackOnRateLimit(t *testing.T) {
	primary := llm.NewErrorClient(&llm.APIError{StatusCode: 429, Message: "rate limited", Retryable: true})
	fallback := llm.NewReplayClient(llm.CompletionResponse{Content: "fallback"})
	client := llm.NewFallbackClient(primary, fallback)

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "fallback" {
		t.Errorf("expected 'fallback', got %q", resp.Content)
	}
}

func TestFallbackClient_FallsBackOnFatalError(t *testing.T) {
	primary := llm.NewErrorClient(&llm.APIError{StatusCode: 403, Message: "forbidden"})
	fallback := llm.NewReplayClient(llm.CompletionResponse{Content: "fallback"})
	client := llm.NewFallbackClient(primary, fallback)

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "fallback" {
		t.Errorf("expected 'fallback', got %q", resp.Content)
	}
}

func TestFallbackClient_ReturnsFallbackErrorWhenBothFail(t *testing.T) {
	primary := llm.NewErrorClient(&llm.APIError{StatusCode: 429, Message: "primary rate limited", Retryable: true})
	fallback := llm.NewErrorClient(&llm.APIError{StatusCode: 500, Message: "fallback server error", Retryable: true})
	client := llm.NewFallbackClient(primary, fallback)

	_, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected error when both providers fail")
	}
	if !llm.IsRetryable(err) {
		t.Errorf("expected retryable error from fallback, got: %v", err)
	}
}

func TestFallbackClient_SkipsPrimaryWhenQuotaExhausted(t *testing.T) {
	primary := llm.NewReplayClient(llm.CompletionResponse{Content: "primary"})
	fallback := llm.NewReplayClient(
		llm.CompletionResponse{Content: "fallback1"},
		llm.CompletionResponse{Content: "fallback2"},
	)
	client := llm.NewFallbackClientWithLimits(primary, fallback, 2, 100)

	// First call uses primary (within limits: minuteCount=0 < threshold (2*9)/10=1)
	resp, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if resp.Content != "primary" {
		t.Errorf("call 1: expected 'primary', got %q", resp.Content)
	}

	// Second call: after recording, minuteCount=1 >= threshold 1, so fallback
	resp, err = client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if resp.Content != "fallback1" {
		t.Errorf("call 2: expected 'fallback1', got %q", resp.Content)
	}
}

func TestFallbackClient_NonAPIErrorFallsBack(t *testing.T) {
	primary := llm.NewErrorClient(context.DeadlineExceeded)
	fallback := llm.NewReplayClient(llm.CompletionResponse{Content: "fallback"})
	client := llm.NewFallbackClient(primary, fallback)

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "fallback" {
		t.Errorf("expected 'fallback', got %q", resp.Content)
	}
}
