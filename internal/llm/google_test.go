package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestGoogleAIClient_ImplementsClientInterface(t *testing.T) {
	var _ llm.Client = llm.NewGoogleAIClient("key")
}

func TestGoogleAIClient_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Errorf("expected key 'test-key', got %q", got)
		}

		if r.URL.Path != "/v1beta/models/gemma-4-27b-it:generateContent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		sysInstr, ok := reqBody["system_instruction"].(map[string]any)
		if !ok {
			t.Fatal("expected system_instruction in request")
		}
		parts := sysInstr["parts"].([]any)
		part := parts[0].(map[string]any)
		if part["text"] != "You are a tech lead" {
			t.Errorf("expected system instruction text, got %v", part["text"])
		}

		contents := reqBody["contents"].([]any)
		if len(contents) != 1 {
			t.Fatalf("expected 1 content, got %d", len(contents))
		}
		content := contents[0].(map[string]any)
		if content["role"] != "user" {
			t.Errorf("expected role 'user', got %v", content["role"])
		}

		genCfg := reqBody["generationConfig"].(map[string]any)
		if genCfg["maxOutputTokens"].(float64) != 4000 {
			t.Errorf("expected maxOutputTokens 4000, got %v", genCfg["maxOutputTokens"])
		}

		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Here is the plan"},
						},
						"role": "model",
					},
					"finishReason": "STOP",
				},
			},
			"modelVersion": "gemma-4-27b-it",
			"usageMetadata": map[string]any{
				"promptTokenCount":     100,
				"candidatesTokenCount": 50,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model:     "gemma-4-27b-it",
		MaxTokens: 4000,
		System:    "You are a tech lead",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Plan this feature"},
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Content != "Here is the plan" {
		t.Errorf("expected 'Here is the plan', got %q", resp.Content)
	}
	if resp.Model != "gemma-4-27b-it" {
		t.Errorf("expected model 'gemma-4-27b-it', got %q", resp.Model)
	}
	if resp.StopReason != "STOP" {
		t.Errorf("expected stop_reason 'STOP', got %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", resp.Usage.OutputTokens)
	}
}

func TestGoogleAIClient_RateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":429,"message":"Rate limit exceeded"}}`))
	}))
	defer server.Close()

	client := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)
	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
	})
	if err == nil {
		t.Fatal("expected error for 429 status")
	}
	if !llm.IsRateLimited(err) {
		t.Errorf("expected rate limited error, got: %v", err)
	}
	if llm.RetryAfterSeconds(err) != 30 {
		t.Errorf("expected RetryAfter 30, got %d", llm.RetryAfterSeconds(err))
	}
}

func TestGoogleAIClient_ResourceExhaustedMapsTo429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":400,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded"}}`))
	}))
	defer server.Close()

	client := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)
	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
	})
	if err == nil {
		t.Fatal("expected error for RESOURCE_EXHAUSTED")
	}
	if !llm.IsRateLimited(err) {
		t.Errorf("expected RESOURCE_EXHAUSTED to map to rate limited, got: %v", err)
	}
}

func TestGoogleAIClient_FatalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":403,"message":"API key invalid"}}`))
	}))
	defer server.Close()

	client := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)
	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
	})
	if err == nil {
		t.Fatal("expected error for 403 status")
	}
	if !llm.IsFatalAPIError(err) {
		t.Errorf("expected fatal error, got: %v", err)
	}
}

func TestGoogleAIClient_AssistantRoleMapsToModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)

		contents := reqBody["contents"].([]any)
		second := contents[1].(map[string]any)
		if second["role"] != "model" {
			t.Errorf("expected role 'model' for assistant message, got %v", second["role"])
		}

		resp := map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}, "role": "model"}, "finishReason": "STOP"},
			},
			"modelVersion": "gemma-4-27b-it",
			"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)
	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Hello"},
			{Role: llm.RoleAssistant, Content: "Hi there"},
			{Role: llm.RoleUser, Content: "How are you?"},
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestGoogleAIClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {}
	}))
	defer server.Close()

	client := llm.NewGoogleAIClient("test-key").WithBaseURL(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Complete(ctx, llm.CompletionRequest{Model: "gemma-4-27b-it"})
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}
