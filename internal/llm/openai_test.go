package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestOpenAIClient_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if got := r.Header.Get("Authorization"); got != "Bearer test-openai-key" {
			t.Errorf("expected Authorization 'Bearer test-openai-key', got %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %q", got)
		}

		// Verify request body
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if reqBody["model"] != "gpt-4o" {
			t.Errorf("expected model 'gpt-4o', got %v", reqBody["model"])
		}

		// Verify system prompt is first message
		messages, ok := reqBody["messages"].([]any)
		if !ok || len(messages) < 2 {
			t.Fatalf("expected at least 2 messages, got %v", reqBody["messages"])
		}
		firstMsg, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("expected first message to be a map, got %T", messages[0])
		}
		if firstMsg["role"] != "system" {
			t.Errorf("expected first message role 'system', got %v", firstMsg["role"])
		}
		if firstMsg["content"] != "You are a code reviewer" {
			t.Errorf("expected system content, got %v", firstMsg["content"])
		}

		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "Code review complete",
					},
					"finish_reason": "stop",
				},
			},
			"model": "gpt-4o",
			"usage": map[string]any{
				"prompt_tokens":     200,
				"completion_tokens": 75,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := llm.NewOpenAIClient("test-openai-key").WithBaseURL(server.URL)

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model:     "gpt-4o",
		MaxTokens: 4000,
		System:    "You are a code reviewer",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Review this code"},
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "Code review complete" {
		t.Fatalf("expected 'Code review complete', got %q", resp.Content)
	}
	if resp.Model != "gpt-4o" {
		t.Fatalf("expected model 'gpt-4o', got %q", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason 'end_turn', got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 200 {
		t.Fatalf("expected 200 input tokens, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 75 {
		t.Fatalf("expected 75 output tokens, got %d", resp.Usage.OutputTokens)
	}
}

func TestOpenAIClient_NoSystemPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		// Without a system prompt, there should be only 1 message
		messages, ok := reqBody["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("expected 1 message without system prompt, got %d", len(messages))
		}
		firstMsg := messages[0].(map[string]any)
		if firstMsg["role"] != "user" {
			t.Errorf("expected role 'user', got %v", firstMsg["role"])
		}

		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"role": "assistant", "content": "ok"},
					"finish_reason": "stop",
				},
			},
			"model": "gpt-4o",
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := llm.NewOpenAIClient("key").WithBaseURL(server.URL)
	resp, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model:     "gpt-4o",
		MaxTokens: 100,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected 'ok', got %q", resp.Content)
	}
}

func TestOpenAIClient_RateLimitRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error": {"message": "rate limited"}}`))
	}))
	defer server.Close()

	client := llm.NewOpenAIClient("test-key").WithBaseURL(server.URL)
	_, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected error for 429 status")
	}

	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *llm.APIError, got %T", err)
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("expected status 429, got %d", apiErr.StatusCode)
	}
	if !apiErr.Retryable {
		t.Error("expected 429 to be retryable")
	}
	if apiErr.RetryAfter != 30 {
		t.Errorf("expected RetryAfter 30, got %d", apiErr.RetryAfter)
	}
}

func TestOpenAIClient_AuthErrorNotRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"message": "invalid api key"}}`))
	}))
	defer server.Close()

	client := llm.NewOpenAIClient("bad-key").WithBaseURL(server.URL)
	_, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected error for 401 status")
	}

	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *llm.APIError, got %T", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("expected status 401, got %d", apiErr.StatusCode)
	}
	if apiErr.Retryable {
		t.Error("expected 401 to NOT be retryable")
	}
}

func TestOpenAIClient_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{},
			"model":   "gpt-4o",
			"usage":   map[string]any{"prompt_tokens": 0, "completion_tokens": 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := llm.NewOpenAIClient("key").WithBaseURL(server.URL)
	_, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAIClient_FinishReasonMapping(t *testing.T) {
	tests := []struct {
		name           string
		finishReason   string
		wantStopReason string
	}{
		{"stop maps to end_turn", "stop", "end_turn"},
		{"length maps to max_tokens", "length", "max_tokens"},
		{"unknown passes through", "content_filter", "content_filter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := map[string]any{
					"choices": []map[string]any{
						{
							"message":       map[string]any{"role": "assistant", "content": "ok"},
							"finish_reason": tt.finishReason,
						},
					},
					"model": "gpt-4o",
					"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client := llm.NewOpenAIClient("key").WithBaseURL(server.URL)
			resp, err := client.Complete(context.Background(), llm.CompletionRequest{
				Model:     "gpt-4o",
				MaxTokens: 100,
				Messages:  []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StopReason != tt.wantStopReason {
				t.Errorf("expected StopReason %q, got %q", tt.wantStopReason, resp.StopReason)
			}
		})
	}
}

func TestOpenAIClient_ImplementsClientInterface(t *testing.T) {
	var _ llm.Client = llm.NewOpenAIClient("key")
}

func TestOpenAIClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {}
	}))
	defer server.Close()

	client := llm.NewOpenAIClient("key").WithBaseURL(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Complete(ctx, llm.CompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}
