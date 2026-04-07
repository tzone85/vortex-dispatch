# Gemma 4 + Google AI Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Google AI Studio as a free-tier provider for execution roles (Junior, Intermediate, Supervisor) with quota-aware fallback to Anthropic/OpenAI, and structured function calling for Gemma models.

**Architecture:** Composable client wrappers following VXD's existing decorator pattern. Four new files in `internal/llm/` (GoogleAIClient, FallbackClient, ToolCallAdapter, ToolSchemas), two edited Go files (config defaults, provider wiring), and three doc updates. Zero changes to engine files — the ToolCallAdapter produces the same JSON structures that existing `extractJSON()` + `json.Unmarshal()` already parse.

**Tech Stack:** Go 1.23+, `net/http`, `encoding/json`, `sync/atomic`, `httptest` for tests

**Spec:** `docs/superpowers/specs/2026-04-07-gemma4-google-ai-integration-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/llm/google.go` | Create | GoogleAIClient — HTTP transport to Google AI Studio generateContent API |
| `internal/llm/google_test.go` | Create | Tests for GoogleAIClient (httptest server, request/response mapping, error codes) |
| `internal/llm/fallback.go` | Create | FallbackClient + QuotaTracker — provider-level switching on quota exhaustion |
| `internal/llm/fallback_test.go` | Create | Tests for FallbackClient (quota tracking, fallback triggers, edge cases) |
| `internal/llm/toolcall.go` | Create | ToolCallAdapter — injects tool schemas for Gemma, parses tool-call tokens |
| `internal/llm/toolcall_test.go` | Create | Tests for ToolCallAdapter (passthrough, injection, parsing, degradation) |
| `internal/llm/toolschemas.go` | Create | Per-role tool schema definitions (TechLead, Reviewer, Supervisor, Manager) |
| `internal/llm/toolschemas_test.go` | Create | Tests for schema lookup and JSON validity |
| `internal/config/loader.go` | Edit | Update DefaultConfig() — Junior, Intermediate, Supervisor → google/gemma-4-27b-it |
| `internal/cli/req.go` | Edit | Add `"google"` case to buildLLMClient() and buildPlanningClient() |
| `docs/gemma-4-guide.md` | Create | Integration guide: setup, cost comparison, fallback behavior |
| `docs/model-selection.md` | Create | Model selection guide with execution/verification tier concept |
| `docs/getting-started.md` | Edit | Add GOOGLE_AI_API_KEY to authentication section |

---

### Task 1: GoogleAIClient — Failing Tests

**Files:**
- Create: `internal/llm/google_test.go`

- [ ] **Step 1: Write the interface compliance test**

```go
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
```

- [ ] **Step 2: Write the happy-path Complete test**

```go
func TestGoogleAIClient_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify API key in query param
		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Errorf("expected key 'test-key', got %q", got)
		}

		// Verify URL path contains model name
		if r.URL.Path != "/v1beta/models/gemma-4-27b-it:generateContent" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Verify request body structure
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		// Check system_instruction
		sysInstr, ok := reqBody["system_instruction"].(map[string]any)
		if !ok {
			t.Fatal("expected system_instruction in request")
		}
		parts := sysInstr["parts"].([]any)
		part := parts[0].(map[string]any)
		if part["text"] != "You are a tech lead" {
			t.Errorf("expected system instruction text, got %v", part["text"])
		}

		// Check contents
		contents := reqBody["contents"].([]any)
		if len(contents) != 1 {
			t.Fatalf("expected 1 content, got %d", len(contents))
		}
		content := contents[0].(map[string]any)
		if content["role"] != "user" {
			t.Errorf("expected role 'user', got %v", content["role"])
		}

		// Check generationConfig
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
```

- [ ] **Step 3: Write error status tests**

```go
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
	// RESOURCE_EXHAUSTED should be mapped to 429 so IsRateLimited catches it
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
		// Second message should have role "model" (mapped from "assistant")
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
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestGoogleAI -v`
Expected: Compilation failure — `llm.NewGoogleAIClient` not defined.

- [ ] **Step 5: Commit the failing tests**

```bash
git add internal/llm/google_test.go
git commit -m "test: add failing tests for GoogleAIClient"
```

---

### Task 2: GoogleAIClient — Implementation

**Files:**
- Create: `internal/llm/google.go`

- [ ] **Step 1: Implement GoogleAIClient**

```go
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const googleAIBaseURL = "https://generativelanguage.googleapis.com"

// GoogleAIClient communicates with the Google AI Studio generateContent API.
type GoogleAIClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewGoogleAIClient creates a client configured with the given API key.
func NewGoogleAIClient(apiKey string) *GoogleAIClient {
	return &GoogleAIClient{
		apiKey:     apiKey,
		httpClient: &http.Client{},
		baseURL:    googleAIBaseURL,
	}
}

// WithBaseURL returns a copy of the client with a custom base URL,
// useful for testing with httptest servers.
func (c *GoogleAIClient) WithBaseURL(url string) *GoogleAIClient {
	return &GoogleAIClient{
		apiKey:     c.apiKey,
		httpClient: c.httpClient,
		baseURL:    url,
	}
}

type googleRequest struct {
	SystemInstruction *googleSystemInstruction `json:"system_instruction,omitempty"`
	Contents          []googleContent          `json:"contents"`
	GenerationConfig  googleGenerationConfig   `json:"generationConfig"`
}

type googleSystemInstruction struct {
	Parts []googlePart `json:"parts"`
}

type googleContent struct {
	Role  string       `json:"role"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text string `json:"text"`
}

type googleGenerationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature,omitempty"`
}

type googleResponse struct {
	Candidates    []googleCandidate   `json:"candidates"`
	ModelVersion  string              `json:"modelVersion"`
	UsageMetadata googleUsageMetadata `json:"usageMetadata"`
}

type googleCandidate struct {
	Content      googleContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type googleUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}

// Complete sends a completion request to the Google AI Studio generateContent
// API and returns the parsed response.
func (c *GoogleAIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	contents := make([]googleContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := string(m.Role)
		if role == string(RoleAssistant) {
			role = "model"
		}
		contents = append(contents, googleContent{
			Role:  role,
			Parts: []googlePart{{Text: m.Content}},
		})
	}

	body := googleRequest{
		Contents: contents,
		GenerationConfig: googleGenerationConfig{
			MaxOutputTokens: req.MaxTokens,
			Temperature:     req.Temperature,
		},
	}

	if req.System != "" {
		body.SystemInstruction = &googleSystemInstruction{
			Parts: []googlePart{{Text: req.System}},
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", c.baseURL, req.Model, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, c.classifyError(resp.StatusCode, respBody, resp.Header)
	}

	var apiResp googleResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return CompletionResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(apiResp.Candidates) == 0 {
		return CompletionResponse{}, fmt.Errorf("google AI returned no candidates")
	}

	candidate := apiResp.Candidates[0]
	content := ""
	for _, part := range candidate.Content.Parts {
		content += part.Text
	}

	return CompletionResponse{
		Content:    content,
		Model:      apiResp.ModelVersion,
		StopReason: candidate.FinishReason,
		Usage: Usage{
			InputTokens:  apiResp.UsageMetadata.PromptTokenCount,
			OutputTokens: apiResp.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}

// classifyError maps Google AI error responses to APIError. Notably,
// RESOURCE_EXHAUSTED (HTTP 400) is remapped to StatusCode 429 so that
// the existing IsRateLimited() helper catches it for fallback triggering.
func (c *GoogleAIClient) classifyError(statusCode int, body []byte, headers http.Header) *APIError {
	msg := string(body)
	retryAfter, _ := strconv.Atoi(headers.Get("Retry-After"))

	// Google returns RESOURCE_EXHAUSTED as 400 or 429.
	// Map 400+RESOURCE_EXHAUSTED to 429 so IsRateLimited() catches it.
	if statusCode == 400 && strings.Contains(msg, "RESOURCE_EXHAUSTED") {
		return &APIError{
			StatusCode: 429,
			Message:    msg,
			Retryable:  true,
			RetryAfter: retryAfter,
		}
	}

	retryable := statusCode == 429 || statusCode >= 500
	return &APIError{
		StatusCode: statusCode,
		Message:    msg,
		Retryable:  retryable,
		RetryAfter: retryAfter,
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestGoogleAI -v`
Expected: All 6 tests PASS.

- [ ] **Step 3: Run full llm package tests**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -v`
Expected: All tests PASS (no regressions).

- [ ] **Step 4: Commit**

```bash
git add internal/llm/google.go internal/llm/google_test.go
git commit -m "feat: add GoogleAIClient for Google AI Studio API"
```

---

### Task 3: FallbackClient + QuotaTracker — Failing Tests

**Files:**
- Create: `internal/llm/fallback_test.go`

- [ ] **Step 1: Write interface compliance and happy-path tests**

```go
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
```

- [ ] **Step 2: Write fallback-on-rate-limit test**

```go
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
```

- [ ] **Step 3: Write fallback-on-fatal-error test**

```go
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
```

- [ ] **Step 4: Write both-fail test**

```go
func TestFallbackClient_ReturnsFallbackErrorWhenBothFail(t *testing.T) {
	primary := llm.NewErrorClient(&llm.APIError{StatusCode: 429, Message: "primary rate limited", Retryable: true})
	fallback := llm.NewErrorClient(&llm.APIError{StatusCode: 500, Message: "fallback server error", Retryable: true})
	client := llm.NewFallbackClient(primary, fallback)

	_, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("expected error when both providers fail")
	}
	// Should return the fallback error, not the primary
	if !llm.IsRetryable(err) {
		t.Errorf("expected retryable error from fallback, got: %v", err)
	}
}
```

- [ ] **Step 5: Write quota tracker preemption test**

```go
func TestFallbackClient_SkipsPrimaryWhenQuotaExhausted(t *testing.T) {
	primary := llm.NewReplayClient(llm.CompletionResponse{Content: "primary"})
	fallback := llm.NewReplayClient(
		llm.CompletionResponse{Content: "fallback1"},
		llm.CompletionResponse{Content: "fallback2"},
	)
	client := llm.NewFallbackClientWithLimits(primary, fallback, 1, 100) // RPM=1, RPD=100

	// First call uses primary (within limits)
	resp, err := client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if resp.Content != "primary" {
		t.Errorf("call 1: expected 'primary', got %q", resp.Content)
	}

	// Second call should skip primary (RPM=1 exhausted at 90%) and go to fallback
	resp, err = client.Complete(context.Background(), llm.CompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if resp.Content != "fallback1" {
		t.Errorf("call 2: expected 'fallback1', got %q", resp.Content)
	}
}
```

- [ ] **Step 6: Write non-API error fallback test**

```go
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
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestFallbackClient -v`
Expected: Compilation failure — `llm.NewFallbackClient` not defined.

- [ ] **Step 8: Commit the failing tests**

```bash
git add internal/llm/fallback_test.go
git commit -m "test: add failing tests for FallbackClient and QuotaTracker"
```

---

### Task 4: FallbackClient + QuotaTracker — Implementation

**Files:**
- Create: `internal/llm/fallback.go`

- [ ] **Step 1: Implement FallbackClient and QuotaTracker**

```go
package llm

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	defaultRPM = 10
	defaultRPD = 1500
)

// QuotaTracker tracks request counts against rate limits using simple
// counters protected by a mutex. Counters reset on minute/day boundaries
// without background goroutines.
type QuotaTracker struct {
	mu sync.Mutex

	rpmLimit int
	rpdLimit int

	minuteCount int
	dayCount    int

	currentMinute int // minute of hour (0-59)
	currentDay    int // day of year (1-366)

	exhaustedUntil time.Time
}

// NewQuotaTracker creates a tracker with the given limits.
func NewQuotaTracker(rpmLimit, rpdLimit int) *QuotaTracker {
	now := time.Now()
	return &QuotaTracker{
		rpmLimit:      rpmLimit,
		rpdLimit:      rpdLimit,
		currentMinute: now.Minute(),
		currentDay:    now.YearDay(),
	}
}

// IsExhausted returns true if either counter is at 90% of its limit
// or if a manual exhaustion cooldown is active.
func (q *QuotaTracker) IsExhausted() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.maybeReset()

	if time.Now().Before(q.exhaustedUntil) {
		return true
	}

	return q.minuteCount >= (q.rpmLimit*9)/10 || q.dayCount >= (q.rpdLimit*9)/10
}

// RecordRequest increments the request counters.
func (q *QuotaTracker) RecordRequest() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.maybeReset()
	q.minuteCount++
	q.dayCount++
}

// MarkExhausted suppresses primary usage for the given cooldown.
func (q *QuotaTracker) MarkExhausted(cooldown time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.exhaustedUntil = time.Now().Add(cooldown)
}

// maybeReset checks if minute/day boundaries have been crossed and
// resets counters accordingly. Must be called under lock.
func (q *QuotaTracker) maybeReset() {
	now := time.Now()
	if now.Minute() != q.currentMinute {
		q.minuteCount = 0
		q.currentMinute = now.Minute()
	}
	if now.YearDay() != q.currentDay {
		q.dayCount = 0
		q.currentDay = now.YearDay()
	}
}

// FallbackClient wraps a primary Client and falls back to a secondary
// when the primary fails due to quota exhaustion or other errors.
type FallbackClient struct {
	primary      Client
	fallback     Client
	quotaTracker *QuotaTracker
}

// NewFallbackClient creates a FallbackClient with default Google AI free-tier limits.
func NewFallbackClient(primary, fallback Client) *FallbackClient {
	return &FallbackClient{
		primary:      primary,
		fallback:     fallback,
		quotaTracker: NewQuotaTracker(defaultRPM, defaultRPD),
	}
}

// NewFallbackClientWithLimits creates a FallbackClient with custom rate limits,
// useful for testing.
func NewFallbackClientWithLimits(primary, fallback Client, rpmLimit, rpdLimit int) *FallbackClient {
	return &FallbackClient{
		primary:      primary,
		fallback:     fallback,
		quotaTracker: NewQuotaTracker(rpmLimit, rpdLimit),
	}
}

// Complete tries the primary client first. On quota/rate-limit errors or any
// failure, it falls back to the secondary client. If the quota tracker
// indicates the primary is exhausted, it skips directly to fallback.
func (f *FallbackClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if !f.quotaTracker.IsExhausted() {
		resp, err := f.primary.Complete(ctx, req)
		if err == nil {
			f.quotaTracker.RecordRequest()
			return resp, nil
		}

		if IsRateLimited(err) {
			cooldown := 60 * time.Second
			if ra := RetryAfterSeconds(err); ra > 0 {
				cooldown = time.Duration(ra) * time.Second
			}
			f.quotaTracker.MarkExhausted(cooldown)
			log.Printf("[fallback] primary rate limited, switching to fallback for %s", cooldown)
		} else if IsFatalAPIError(err) {
			log.Printf("[fallback] primary fatal error (misconfiguration?): %v", err)
		} else {
			log.Printf("[fallback] primary error, trying fallback: %v", err)
		}
	} else {
		log.Printf("[fallback] primary quota exhausted, using fallback directly")
	}

	return f.fallback.Complete(ctx, req)
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestFallbackClient -v`
Expected: All 6 tests PASS.

- [ ] **Step 3: Run full llm package tests**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -v`
Expected: All tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/llm/fallback.go internal/llm/fallback_test.go
git commit -m "feat: add FallbackClient with QuotaTracker for provider-level switching"
```

---

### Task 5: ToolSchemas — Failing Tests

**Files:**
- Create: `internal/llm/toolschemas_test.go`

- [ ] **Step 1: Write schema lookup and validity tests**

```go
package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestToolSchemaFor_ReturnsSchemaForLLMRoles(t *testing.T) {
	roles := []agent.Role{
		agent.RoleTechLead,
		agent.RoleSupervisor,
		agent.RoleManager,
	}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			schema := llm.ToolSchemaFor(role)
			if schema == nil {
				t.Fatalf("expected non-nil schema for role %s", role)
			}
			if schema.Name == "" {
				t.Error("schema name is empty")
			}
		})
	}
}

func TestToolSchemaFor_ReturnsNilForCLIRoles(t *testing.T) {
	roles := []agent.Role{
		agent.RoleJunior,
		agent.RoleIntermediate,
		agent.RoleSenior,
		agent.RoleQA,
	}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			schema := llm.ToolSchemaFor(role)
			if schema != nil {
				t.Errorf("expected nil schema for CLI role %s, got %+v", role, schema)
			}
		})
	}
}

func TestToolSchemaFor_SchemasAreValidJSON(t *testing.T) {
	roles := []agent.Role{
		agent.RoleTechLead,
		agent.RoleSupervisor,
		agent.RoleManager,
	}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			schema := llm.ToolSchemaFor(role)
			if schema == nil {
				t.Skip("no schema")
			}
			data, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("schema is not JSON-serializable: %v", err)
			}
			if len(data) == 0 {
				t.Error("serialized schema is empty")
			}
		})
	}
}

func TestToolSchemaFor_TechLeadHasCreateStories(t *testing.T) {
	schema := llm.ToolSchemaFor(agent.RoleTechLead)
	if schema == nil {
		t.Fatal("expected non-nil schema for TechLead")
	}
	if schema.Name != "create_stories" {
		t.Errorf("expected name 'create_stories', got %q", schema.Name)
	}
}

func TestToolSchemaFor_SupervisorHasReportStatus(t *testing.T) {
	schema := llm.ToolSchemaFor(agent.RoleSupervisor)
	if schema == nil {
		t.Fatal("expected non-nil schema for Supervisor")
	}
	if schema.Name != "report_status" {
		t.Errorf("expected name 'report_status', got %q", schema.Name)
	}
}

func TestToolSchemaFor_ManagerHasDiagnoseFailure(t *testing.T) {
	schema := llm.ToolSchemaFor(agent.RoleManager)
	if schema == nil {
		t.Fatal("expected non-nil schema for Manager")
	}
	if schema.Name != "diagnose_failure" {
		t.Errorf("expected name 'diagnose_failure', got %q", schema.Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestToolSchema -v`
Expected: Compilation failure — `llm.ToolSchemaFor` not defined.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/llm/toolschemas_test.go
git commit -m "test: add failing tests for per-role tool schemas"
```

---

### Task 6: ToolSchemas — Implementation

**Files:**
- Create: `internal/llm/toolschemas.go`

- [ ] **Step 1: Implement ToolSchema type and per-role schemas**

```go
package llm

import "github.com/tzone85/vortex-dispatch/internal/agent"

// ToolSchema defines a function-calling tool that can be injected into
// Gemma model prompts. The Parameters field uses JSON Schema format.
type ToolSchema struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Parameters  SchemaObject `json:"parameters"`
}

// SchemaObject represents a JSON Schema object type.
type SchemaObject struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// SchemaProperty represents a single property in a JSON Schema object.
type SchemaProperty struct {
	Type        string                    `json:"type"`
	Description string                    `json:"description,omitempty"`
	Items       *SchemaProperty           `json:"items,omitempty"`
	Properties  map[string]SchemaProperty `json:"properties,omitempty"`
	Enum        []string                  `json:"enum,omitempty"`
}

// ToolSchemaFor returns the tool schema for the given role, or nil if the
// role does not make direct LLM calls (e.g., Junior/Intermediate/Senior use
// CLI runtimes, QA uses local commands).
func ToolSchemaFor(role agent.Role) *ToolSchema {
	switch role {
	case agent.RoleTechLead:
		return techLeadSchema()
	case agent.RoleSupervisor:
		return supervisorSchema()
	case agent.RoleManager:
		return managerSchema()
	default:
		return nil
	}
}

func techLeadSchema() *ToolSchema {
	return &ToolSchema{
		Name:        "create_stories",
		Description: "Decompose a requirement into implementable stories with dependency ordering",
		Parameters: SchemaObject{
			Type: "object",
			Properties: map[string]SchemaProperty{
				"stories": {
					Type:        "array",
					Description: "Array of decomposed stories",
					Items: &SchemaProperty{
						Type: "object",
						Properties: map[string]SchemaProperty{
							"id":                  {Type: "string", Description: "Short identifier (e.g., s-001)"},
							"title":               {Type: "string", Description: "Brief title"},
							"description":         {Type: "string", Description: "What to implement, including exact file paths"},
							"acceptance_criteria":  {Type: "string", Description: "How to verify it's done"},
							"complexity":           {Type: "integer", Description: "Fibonacci score (1, 2, 3, 5, 8, 13)"},
							"depends_on":           {Type: "array", Description: "Story IDs this depends on", Items: &SchemaProperty{Type: "string"}},
							"owned_files":          {Type: "array", Description: "Exact file paths this story creates or modifies", Items: &SchemaProperty{Type: "string"}},
							"wave_hint":            {Type: "string", Description: "sequential or parallel", Enum: []string{"sequential", "parallel"}},
						},
					},
				},
			},
			Required: []string{"stories"},
		},
	}
}

func supervisorSchema() *ToolSchema {
	return &ToolSchema{
		Name:        "report_status",
		Description: "Report on whether stories are on track to fulfill the requirement",
		Parameters: SchemaObject{
			Type: "object",
			Properties: map[string]SchemaProperty{
				"on_track":     {Type: "boolean", Description: "Whether stories are on track"},
				"concerns":     {Type: "array", Description: "List of concerns", Items: &SchemaProperty{Type: "string"}},
				"reprioritize": {Type: "array", Description: "Story IDs to reprioritize", Items: &SchemaProperty{Type: "string"}},
			},
			Required: []string{"on_track", "concerns", "reprioritize"},
		},
	}
}

func managerSchema() *ToolSchema {
	return &ToolSchema{
		Name:        "diagnose_failure",
		Description: "Diagnose why a story failed and choose a corrective action",
		Parameters: SchemaObject{
			Type: "object",
			Properties: map[string]SchemaProperty{
				"diagnosis": {Type: "string", Description: "Human-readable explanation of failure"},
				"category":  {Type: "string", Description: "Failure category", Enum: []string{"environment", "structural", "complexity", "transient", "unknown"}},
				"action":    {Type: "string", Description: "Corrective action", Enum: []string{"retry", "rewrite", "split", "escalate_to_techlead"}},
				"retry_config": {
					Type:        "object",
					Description: "Config for retry action",
					Properties: map[string]SchemaProperty{
						"target_role":    {Type: "string"},
						"reset_tier":     {Type: "integer"},
						"worktree_reset": {Type: "boolean"},
						"env_fixes":      {Type: "array", Items: &SchemaProperty{Type: "string"}},
					},
				},
				"rewrite_config": {
					Type:        "object",
					Description: "Config for rewrite action",
					Properties: map[string]SchemaProperty{
						"title":               {Type: "string"},
						"description":         {Type: "string"},
						"acceptance_criteria":  {Type: "string"},
						"complexity":           {Type: "integer"},
						"owned_files":          {Type: "array", Items: &SchemaProperty{Type: "string"}},
					},
				},
				"split_config": {
					Type:        "object",
					Description: "Config for split action",
					Properties: map[string]SchemaProperty{
						"children":         {Type: "array", Description: "Child story definitions"},
						"dependency_edges": {Type: "array", Description: "Dependency pairs"},
					},
				},
			},
			Required: []string{"diagnosis", "category", "action"},
		},
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestToolSchema -v`
Expected: All 6 tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/llm/toolschemas.go internal/llm/toolschemas_test.go
git commit -m "feat: add per-role tool schemas for structured function calling"
```

---

### Task 7: ToolCallAdapter — Failing Tests

**Files:**
- Create: `internal/llm/toolcall_test.go`

- [ ] **Step 1: Write interface compliance and passthrough test**

```go
package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/llm"
)

func TestToolCallAdapter_ImplementsClientInterface(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: "ok"})
	var _ llm.Client = llm.NewToolCallAdapter(inner, nil)
}

func TestToolCallAdapter_PassthroughForNonGemmaModel(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: "response"})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleTechLead))

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model:  "claude-sonnet-4-20250514",
		System: "You are a tech lead",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Plan this"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "response" {
		t.Errorf("expected 'response', got %q", resp.Content)
	}

	// Verify system prompt was NOT modified
	recorded := inner.CallAt(0)
	if strings.Contains(recorded.System, "<tools>") {
		t.Error("system prompt should not contain tool schema for non-Gemma model")
	}
}
```

- [ ] **Step 2: Write tool schema injection test (Gemma model)**

```go
func TestToolCallAdapter_InjectsSchemaForGemmaModel(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: `{"on_track":true,"concerns":[],"reprioritize":[]}`})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleSupervisor))

	_, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model:  "gemma-4-27b-it",
		System: "You are a Supervisor",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Review progress"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	recorded := inner.CallAt(0)
	if !strings.Contains(recorded.System, "<tools>") {
		t.Error("system prompt should contain <tools> for Gemma model")
	}
	if !strings.Contains(recorded.System, "report_status") {
		t.Error("system prompt should contain tool name 'report_status'")
	}
	// Original system prompt should be preserved as prefix
	if !strings.HasPrefix(recorded.System, "You are a Supervisor") {
		t.Error("original system prompt should be preserved as prefix")
	}
}
```

- [ ] **Step 3: Write tool-call token parsing test**

```go
func TestToolCallAdapter_ParsesToolCallTokens(t *testing.T) {
	toolCallResponse := "<|tool_call|>\nreport_status\n{\"on_track\":true,\"concerns\":[\"slow progress\"],\"reprioritize\":[]}\n<|end_tool_call|>"

	inner := llm.NewReplayClient(llm.CompletionResponse{Content: toolCallResponse})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleSupervisor))

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should extract the JSON from between tool-call tokens
	if !strings.Contains(resp.Content, `"on_track":true`) {
		t.Errorf("expected parsed JSON in content, got %q", resp.Content)
	}
	// Should NOT contain the tool-call tokens
	if strings.Contains(resp.Content, "<|tool_call|>") {
		t.Error("content should not contain raw tool-call tokens")
	}
}
```

- [ ] **Step 4: Write graceful degradation test (free-text JSON from Gemma)**

```go
func TestToolCallAdapter_GracefulDegradationFreeTextJSON(t *testing.T) {
	// Gemma responds with free-text JSON instead of tool-call tokens
	freeTextJSON := `{"on_track":false,"concerns":["drift detected"],"reprioritize":["s-003"]}`
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: freeTextJSON})
	adapter := llm.NewToolCallAdapter(inner, llm.ToolSchemaFor(agent.RoleSupervisor))

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model: "gemma-4-27b-it",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should pass through the free-text JSON unchanged
	if resp.Content != freeTextJSON {
		t.Errorf("expected passthrough of free-text JSON, got %q", resp.Content)
	}
}
```

- [ ] **Step 5: Write nil schema passthrough test**

```go
func TestToolCallAdapter_NilSchemaPassthrough(t *testing.T) {
	inner := llm.NewReplayClient(llm.CompletionResponse{Content: "ok"})
	adapter := llm.NewToolCallAdapter(inner, nil)

	resp, err := adapter.Complete(context.Background(), llm.CompletionRequest{
		Model:  "gemma-4-27b-it",
		System: "Original prompt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}

	// System prompt should be unchanged
	recorded := inner.CallAt(0)
	if recorded.System != "Original prompt" {
		t.Errorf("expected unmodified system prompt, got %q", recorded.System)
	}
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestToolCallAdapter -v`
Expected: Compilation failure — `llm.NewToolCallAdapter` not defined.

- [ ] **Step 7: Commit the failing tests**

```bash
git add internal/llm/toolcall_test.go
git commit -m "test: add failing tests for ToolCallAdapter"
```

---

### Task 8: ToolCallAdapter — Implementation

**Files:**
- Create: `internal/llm/toolcall.go`

- [ ] **Step 1: Implement ToolCallAdapter**

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCallAdapter wraps a Client to inject tool schemas into system prompts
// for Gemma models and parse tool-call tokens from responses. For non-Gemma
// models or when schema is nil, requests pass through unchanged.
type ToolCallAdapter struct {
	inner      Client
	toolSchema *ToolSchema
}

// NewToolCallAdapter creates an adapter. If schema is nil, all requests
// pass through unchanged regardless of model.
func NewToolCallAdapter(inner Client, schema *ToolSchema) *ToolCallAdapter {
	return &ToolCallAdapter{
		inner:      inner,
		toolSchema: schema,
	}
}

// Complete augments the request with tool schemas for Gemma models, then
// parses tool-call tokens from the response. Non-Gemma models pass through.
func (a *ToolCallAdapter) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if !a.shouldInjectTools(req.Model) {
		return a.inner.Complete(ctx, req)
	}

	augmented := a.augmentRequest(req)
	resp, err := a.inner.Complete(ctx, augmented)
	if err != nil {
		return resp, err
	}

	resp.Content = a.parseToolCallResponse(resp.Content)
	return resp, nil
}

// shouldInjectTools returns true if the model is a Gemma variant and a
// schema is configured.
func (a *ToolCallAdapter) shouldInjectTools(model string) bool {
	return a.toolSchema != nil && strings.Contains(strings.ToLower(model), "gemma")
}

// augmentRequest appends tool definitions to the system prompt.
func (a *ToolCallAdapter) augmentRequest(req CompletionRequest) CompletionRequest {
	schemaJSON, err := json.Marshal([]ToolSchema{*a.toolSchema})
	if err != nil {
		return req
	}

	toolBlock := fmt.Sprintf("\n\nAvailable tools:\n<tools>\n%s\n</tools>\n\nWhen calling a tool, use this format:\n<|tool_call|>\ntool_name\n{json_arguments}\n<|end_tool_call|>", string(schemaJSON))

	return CompletionRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System + toolBlock,
	}
}

// parseToolCallResponse extracts JSON from tool-call tokens if present.
// If no tool-call tokens are found, returns the content unchanged for
// graceful degradation to free-text JSON parsing.
func (a *ToolCallAdapter) parseToolCallResponse(content string) string {
	startToken := "<|tool_call|>"
	endToken := "<|end_tool_call|>"

	startIdx := strings.Index(content, startToken)
	if startIdx == -1 {
		return content
	}

	endIdx := strings.Index(content, endToken)
	if endIdx == -1 {
		return content
	}

	inner := content[startIdx+len(startToken) : endIdx]
	inner = strings.TrimSpace(inner)

	// The format is: tool_name\n{json}
	// Find the first { or [ to locate the JSON payload
	jsonStart := strings.IndexAny(inner, "{[")
	if jsonStart == -1 {
		return content
	}

	return strings.TrimSpace(inner[jsonStart:])
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -run TestToolCallAdapter -v`
Expected: All 5 tests PASS.

- [ ] **Step 3: Run full llm package tests**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/llm/ -v`
Expected: All tests PASS (no regressions).

- [ ] **Step 4: Commit**

```bash
git add internal/llm/toolcall.go internal/llm/toolcall_test.go
git commit -m "feat: add ToolCallAdapter for Gemma structured function calling"
```

---

### Task 9: Config Defaults — Update and Test

**Files:**
- Modify: `internal/config/loader.go:22-25` (Junior, Intermediate, Supervisor defaults)
- Modify: `internal/config/config_test.go` (if existing tests validate defaults)

- [ ] **Step 1: Read current config test to understand what's tested**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/config/ -v`
Expected: See what tests exist and whether they'll break.

- [ ] **Step 2: Update DefaultConfig() in loader.go**

Change the three execution-tier roles from their current providers to Google/Gemma:

In `internal/config/loader.go`, replace:

```go
		Intermediate: ModelConfig{Provider: "anthropic", Model: "claude-haiku-4-5-20251001", MaxTokens: 4000},
```

with:

```go
		Intermediate: ModelConfig{Provider: "google", Model: "gemma-4-27b-it", MaxTokens: 4000},
```

Replace:

```go
		Junior:       ModelConfig{Provider: "openai", Model: "gpt-4o-mini", MaxTokens: 4000},
```

with:

```go
		Junior:       ModelConfig{Provider: "google", Model: "gemma-4-27b-it", MaxTokens: 4000},
```

Replace:

```go
		Supervisor:   ModelConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514", MaxTokens: 4000},
```

with:

```go
		Supervisor:   ModelConfig{Provider: "google", Model: "gemma-4-27b-it", MaxTokens: 4000},
```

- [ ] **Step 3: Update any tests that assert on default model values**

If `config_test.go` validates default provider/model strings, update the expected values to match:
- Junior: provider `"google"`, model `"gemma-4-27b-it"`
- Intermediate: provider `"google"`, model `"gemma-4-27b-it"`
- Supervisor: provider `"google"`, model `"gemma-4-27b-it"`

- [ ] **Step 4: Run config and full test suite**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./internal/config/ -v && go test ./... 2>&1 | tail -30`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/loader.go internal/config/config_test.go
git commit -m "feat: update default config — Junior, Intermediate, Supervisor use Google/Gemma 4"
```

---

### Task 10: Provider Wiring — Add Google to buildLLMClient

**Files:**
- Modify: `internal/cli/req.go:180-251` (buildLLMClient and buildPlanningClient)

- [ ] **Step 1: Add agent import to req.go**

In `internal/cli/req.go`, add `"github.com/tzone85/vortex-dispatch/internal/agent"` to the import block if not already present.

- [ ] **Step 2: Update buildLLMClient to accept schema parameter**

Replace the function signature:

```go
func buildLLMClient(provider string, godmode ...bool) (llm.Client, error) {
	skipPerms := len(godmode) > 0 && godmode[0]
```

with:

```go
func buildLLMClient(provider string, schema *llm.ToolSchema, godmode ...bool) (llm.Client, error) {
	skipPerms := len(godmode) > 0 && godmode[0]
```

- [ ] **Step 3: Add the "google" case to buildLLMClient**

Add before the `default` case:

```go
	case "google":
		apiKey := os.Getenv("GOOGLE_AI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("GOOGLE_AI_API_KEY environment variable is required")
		}
		google := llm.NewToolCallAdapter(llm.NewGoogleAIClient(apiKey), schema)
		primary := llm.NewRetryClient(google, 2)

		// Build fallback: Claude CLI first, then Anthropic API
		var fallback llm.Client
		if _, err := exec.LookPath("claude"); err == nil {
			c := llm.NewClaudeCLIClient()
			if skipPerms {
				c = c.WithSkipPermissions()
			}
			fallback = c
		} else if ak := os.Getenv("ANTHROPIC_API_KEY"); ak != "" {
			fallback = llm.NewRetryClient(llm.NewAnthropicClient(ak), 3)
		}

		if fallback != nil {
			return llm.NewFallbackClient(primary, fallback), nil
		}
		return primary, nil
```

- [ ] **Step 4: Update all existing callers to pass nil for schema**

Search for all calls to `buildLLMClient(` in the codebase. Each existing call like `buildLLMClient(provider)` or `buildLLMClient(provider, godmode)` becomes `buildLLMClient(provider, nil)` or `buildLLMClient(provider, nil, godmode)`.

Check these files:
- `internal/cli/req.go` — internal calls
- `internal/cli/resume.go` — reviewer/supervisor/manager client construction

For `resume.go` callers that build clients for specific roles, pass the role's schema:
```go
// For reviewer (Senior role — no schema needed, stays on Anthropic)
buildLLMClient(cfg.Models.Senior.Provider, nil)

// For supervisor
buildLLMClient(cfg.Models.Supervisor.Provider, llm.ToolSchemaFor(agent.RoleSupervisor))

// For manager
buildLLMClient(cfg.Models.Manager.Provider, llm.ToolSchemaFor(agent.RoleManager))
```

- [ ] **Step 5: Add "google" case to buildPlanningClient**

In `buildPlanningClient`, add before the `default` case:

```go
	case "google":
		if apiKey := os.Getenv("GOOGLE_AI_API_KEY"); apiKey != "" {
			google := llm.NewToolCallAdapter(llm.NewGoogleAIClient(apiKey), llm.ToolSchemaFor(agent.RoleTechLead))
			apiClient = llm.NewRetryClient(google, 2)
		}
		// Also try CLI as fallback
		if _, err := exec.LookPath("claude"); err == nil {
			c := llm.NewClaudeCLIClient()
			if godmode {
				c = c.WithSkipPermissions()
			}
			cliClient = c
		}
```

- [ ] **Step 6: Build to verify compilation**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go build ./...`
Expected: Clean build, no errors.

- [ ] **Step 7: Run full test suite**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./... 2>&1 | tail -40`
Expected: All tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/req.go internal/cli/resume.go
git commit -m "feat: wire Google AI provider into buildLLMClient with fallback chain"
```

---

### Task 11: Documentation — Gemma 4 Guide

**Files:**
- Create: `docs/gemma-4-guide.md`

- [ ] **Step 1: Write the Gemma 4 integration guide**

Create `docs/gemma-4-guide.md` with the following content:

```markdown
# Gemma 4 Integration Guide

VXD uses Google's Gemma 4 (27B, Mixture-of-Experts) via Google AI Studio's free tier as a cost-optimization layer for execution roles.

## Quick Start

1. Get a free API key from [Google AI Studio](https://aistudio.google.com/apikey)
2. Export it:
   ```bash
   export GOOGLE_AI_API_KEY="your-key-here"
   ```
3. Run VXD as normal — Junior, Intermediate, and Supervisor roles will automatically use Gemma 4.

## How VXD Uses Gemma 4

VXD splits roles into two tiers:

| Tier | Roles | Provider | Purpose |
|------|-------|----------|---------|
| **Execution** | Junior, Intermediate, Supervisor | Google AI (Gemma 4) | Bulk code generation, periodic checks |
| **Verification** | TechLead, Senior, QA, Manager | Anthropic (Claude) | Planning, review, QA, failure diagnosis |

Execution roles handle the high-volume work (code generation for each story). Verification roles are quality gates that need strong reasoning. This split maximizes free-tier usage while maintaining quality.

## Fallback Behavior

When Google AI's free-tier quota is exhausted (1,500 requests/day or 10 requests/minute), VXD automatically falls back to your configured Anthropic or OpenAI provider. No manual intervention needed.

The fallback chain for a Google-configured role:
1. **Google AI Studio** (free) — primary
2. **Claude Code CLI** (uses your subscription) — first fallback
3. **Anthropic API** (uses ANTHROPIC_API_KEY) — second fallback

VXD tracks quota usage and preemptively switches to fallback at 90% of limits to avoid failed requests.

## Google AI Free Tier Limits

| Limit | Value |
|-------|-------|
| Requests per minute | 10 |
| Requests per day | 1,500 |
| Input tokens per request | 32,000 |
| Output tokens per request | 8,192 |

For a typical VXD run with 5-10 stories, expect ~50-100 requests across all stages. Well within daily quota for several runs per day.

## Cost Comparison

| Provider | Cost (approximate) | VXD Usage |
|----------|-------------------|-----------|
| Google AI (Gemma 4) | Free | Junior, Intermediate, Supervisor |
| Anthropic Claude Sonnet | ~$3/$15 per 1M tokens | Senior, QA, Manager |
| Anthropic Claude Opus | ~$15/$75 per 1M tokens | TechLead |
| Claude Code CLI | Included in Max subscription | Fallback for all roles |

## Structured Function Calling

When Gemma 4 is used for LLM-facing roles (Supervisor, or TechLead/Manager if reconfigured), VXD uses Gemma's native tool-call protocol for more reliable structured output. This is transparent — no configuration needed. If a Gemma model responds with free-text JSON instead of tool-call tokens, VXD's existing JSON parser handles it seamlessly.

## Configuration

To override the defaults, edit `vxd.yaml`:

```yaml
models:
  # Use Gemma 4 for all execution roles (default)
  junior:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
  intermediate:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
  supervisor:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000

  # Use Claude for all verification roles (default)
  tech_lead:
    provider: anthropic
    model: claude-opus-4-20250514
    max_tokens: 16000
  qa:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000
```

### All-Anthropic Setup (no Google API key needed)

```yaml
models:
  junior:
    provider: anthropic
    model: claude-haiku-4-5-20251001
    max_tokens: 4000
  intermediate:
    provider: anthropic
    model: claude-haiku-4-5-20251001
    max_tokens: 4000
  supervisor:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 4000
```

### All-Google Setup (maximize free tier)

```yaml
models:
  tech_lead:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 8000
  senior:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
  qa:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
  manager:
    provider: google
    model: gemma-4-27b-it
    max_tokens: 4000
```

> **Note:** All-Google puts verification roles on a weaker model. QA may miss subtle plan-vs-execution drift. Recommended only for cost-sensitive or experimental use.

## Environment Variables

| Variable | Required | Purpose |
|----------|----------|---------|
| `GOOGLE_AI_API_KEY` | For Google roles | Google AI Studio API key |
| `ANTHROPIC_API_KEY` | For Anthropic roles | Anthropic API key (or use Claude CLI) |
| `OPENAI_API_KEY` | For OpenAI roles | OpenAI API key |
```

- [ ] **Step 2: Commit**

```bash
git add docs/gemma-4-guide.md
git commit -m "docs: add Gemma 4 integration guide"
```

---

### Task 12: Documentation — Model Selection Guide

**Files:**
- Create: `docs/model-selection.md`

- [ ] **Step 1: Write the model selection guide**

Create `docs/model-selection.md` with the following content:

```markdown
# Model Selection Guide

VXD supports multiple LLM providers and lets you assign different models to different roles based on your cost, quality, and availability requirements.

## Providers

| Provider | Key | Models | Cost |
|----------|-----|--------|------|
| `anthropic` | `ANTHROPIC_API_KEY` or Claude CLI | Claude Opus/Sonnet/Haiku | Paid (or subscription) |
| `openai` | `OPENAI_API_KEY` | GPT-4o, GPT-4o-mini, o3 | Paid |
| `google` | `GOOGLE_AI_API_KEY` | Gemma 4 27B | Free tier (1500 req/day) |

## Execution vs Verification Tiers

VXD roles fall into two tiers with different quality requirements:

**Execution Tier** — High volume, code generation focus:
- Junior (simple stories, 1-3 complexity)
- Intermediate (medium stories, 3-5 complexity)
- Supervisor (periodic drift checks)

These roles benefit from fast, cheap models. Gemma 4 on Google AI's free tier is ideal.

**Verification Tier** — Low volume, reasoning focus:
- TechLead (requirement decomposition, planning)
- Senior (complex stories, code review, escalations)
- QA (plan-vs-execution verification, quality gate)
- Manager (failure diagnosis, corrective actions)

These roles need strong reasoning to catch issues. Claude Sonnet/Opus recommended.

## Default Configuration

VXD ships with a hybrid setup that maximizes free-tier usage:

| Role | Provider | Model | Tier |
|------|----------|-------|------|
| TechLead | anthropic | claude-opus-4 | Verification |
| Senior | anthropic | claude-sonnet-4 | Verification |
| Intermediate | google | gemma-4-27b-it | Execution |
| Junior | google | gemma-4-27b-it | Execution |
| QA | anthropic | claude-sonnet-4 | Verification |
| Supervisor | google | gemma-4-27b-it | Execution |
| Manager | anthropic | claude-sonnet-4 | Verification |

## Choosing Models

**Budget-conscious:** Use the defaults. Execution roles are free, verification roles use your Anthropic subscription or API key.

**Maximum quality:** Put all roles on Claude Sonnet or Opus. Higher cost but strongest reasoning across the board.

**Fully free:** Put all roles on Google/Gemma 4. Works for smaller projects but QA and Manager may miss subtle issues.

**OpenAI alternative:** Replace Anthropic roles with OpenAI equivalents (GPT-4o for verification, GPT-4o-mini for execution).

## Fallback Behavior

When a Google-configured role hits free-tier quota limits, VXD automatically falls back to Anthropic (Claude CLI or API). This is transparent — no manual intervention needed. See the [Gemma 4 Guide](gemma-4-guide.md) for details on quota limits and fallback chain.
```

- [ ] **Step 2: Commit**

```bash
git add docs/model-selection.md
git commit -m "docs: add model selection guide with execution/verification tiers"
```

---

### Task 13: Documentation — Update Getting Started

**Files:**
- Modify: `docs/getting-started.md:32-46` (authentication section)

- [ ] **Step 1: Add GOOGLE_AI_API_KEY to the authentication section**

In `docs/getting-started.md`, find the bash block with API key exports (around line 32-44) and add the Google AI key. Replace:

```bash
# API key for VXD's internal planner/reviewer/QA calls
export ANTHROPIC_API_KEY="sk-ant-..."

# For OpenAI models (Codex runtime, or if using OpenAI for planner)
export OPENAI_API_KEY="sk-..."
```

with:

```bash
# API key for VXD's internal planner/reviewer/QA calls
export ANTHROPIC_API_KEY="sk-ant-..."

# For OpenAI models (Codex runtime, or if using OpenAI for planner)
export OPENAI_API_KEY="sk-..."

# For Google AI Studio (free tier — used by default for Junior/Intermediate/Supervisor)
export GOOGLE_AI_API_KEY="your-key-here"
```

- [ ] **Step 2: Add a note after the cost note**

After the existing cost note blockquote (around line 46), add:

```markdown
> **Gemma 4 default:** VXD uses Google AI Studio's free tier for execution roles (Junior, Intermediate, Supervisor) by default. Get a free API key from [Google AI Studio](https://aistudio.google.com/apikey). If no `GOOGLE_AI_API_KEY` is set, configure these roles to use `anthropic` or `openai` in `vxd.yaml`. See the [Model Selection Guide](model-selection.md) for details.
```

- [ ] **Step 3: Commit**

```bash
git add docs/getting-started.md
git commit -m "docs: add GOOGLE_AI_API_KEY to getting started guide"
```

---

### Task 14: Final Verification

**Files:** None (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go test ./... -v 2>&1 | tail -60`
Expected: All tests PASS.

- [ ] **Step 2: Run go vet**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go vet ./...`
Expected: No issues.

- [ ] **Step 3: Build the binary**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && go build -o ~/.local/bin/vxd ./cmd/vxd`
Expected: Clean build, binary at `~/.local/bin/vxd`.

- [ ] **Step 4: Verify vxd runs**

Run: `~/.local/bin/vxd --help`
Expected: Help output, no errors.

- [ ] **Step 5: Verify new files exist**

Run: `ls -la internal/llm/google.go internal/llm/fallback.go internal/llm/toolcall.go internal/llm/toolschemas.go docs/gemma-4-guide.md docs/model-selection.md`
Expected: All 6 files exist.

- [ ] **Step 6: Review git log**

Run: `cd /Users/mncedimini/Sites/misc/vortex-dispatch && git log --oneline -15`
Expected: ~12 commits covering tests + implementation + config + docs.
