package llm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestAPIError_IsInsufficientBalance(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{
			name:   "credit balance message",
			err:    &llm.APIError{StatusCode: 400, Message: `{"error":"Your credit balance is too low"}`},
			expect: true,
		},
		{
			name:   "billing message",
			err:    &llm.APIError{StatusCode: 400, Message: `{"error":"billing issue"}`},
			expect: true,
		},
		{
			name:   "insufficient_quota",
			err:    &llm.APIError{StatusCode: 400, Message: `{"error":"insufficient_quota"}`},
			expect: true,
		},
		{
			name:   "400 but different message",
			err:    &llm.APIError{StatusCode: 400, Message: `{"error":"invalid request"}`},
			expect: false,
		},
		{
			name:   "429 rate limit is not balance",
			err:    &llm.APIError{StatusCode: 429, Message: "rate limited"},
			expect: false,
		},
		{
			name:   "non-APIError",
			err:    fmt.Errorf("some other error"),
			expect: false,
		},
		{
			name:   "wrapped APIError",
			err:    fmt.Errorf("reviewer: %w", &llm.APIError{StatusCode: 400, Message: "credit balance too low"}),
			expect: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := llm.IsInsufficientBalance(tc.err); got != tc.expect {
				t.Errorf("IsInsufficientBalance = %v, want %v", got, tc.expect)
			}
		})
	}
}

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{name: "429 retryable", err: &llm.APIError{StatusCode: 429, Retryable: true}, expect: true},
		{name: "529 retryable", err: &llm.APIError{StatusCode: 529, Retryable: true}, expect: true},
		{name: "400 not retryable", err: &llm.APIError{StatusCode: 400, Retryable: false}, expect: false},
		{name: "non-APIError", err: fmt.Errorf("timeout"), expect: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := llm.IsRetryable(tc.err); got != tc.expect {
				t.Errorf("IsRetryable = %v, want %v", got, tc.expect)
			}
		})
	}
}

func TestRetryClient_NonRetryableFailsFast(t *testing.T) {
	// A billing error (non-retryable) should fail immediately, not retry.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"credit balance is too low"}`))
	}))
	defer server.Close()

	inner := llm.NewAnthropicClient("key").WithBaseURL(server.URL)
	client := llm.NewRetryClient(inner, 3)

	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model:     "test",
		MaxTokens: 10,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !llm.IsInsufficientBalance(err) {
		t.Fatalf("expected insufficient balance error, got: %v", err)
	}
}

func TestRetryClient_RetriesTransientErrors(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		// Succeed on 3rd attempt.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"content":[{"type":"text","text":"ok"}],"model":"test","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	inner := llm.NewAnthropicClient("key").WithBaseURL(server.URL)
	client := llm.NewRetryClient(inner, 3)

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model:     "test",
		MaxTokens: 10,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected 'ok', got %q", resp.Content)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()

	inner := llm.NewAnthropicClient("key").WithBaseURL(server.URL)
	client := llm.NewRetryClient(inner, 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Complete(ctx, llm.CompletionRequest{
		Model:     "test",
		MaxTokens: 10,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
