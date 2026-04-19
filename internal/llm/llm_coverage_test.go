package llm

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- StripCodeFences coverage (currently 30%) ---

func TestStripCodeFences_NoFences(t *testing.T) {
	input := "plain text content"
	got := StripCodeFences(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestStripCodeFences_LanguageFence(t *testing.T) {
	input := "```go\npackage main\n\nfunc main() {}\n```"
	got := StripCodeFences(input)
	if strings.Contains(got, "```") {
		t.Error("expected fences to be stripped")
	}
	if !strings.Contains(got, "package main") {
		t.Error("expected content to be preserved")
	}
}

func TestStripCodeFences_PlainFence(t *testing.T) {
	input := "```\nsome content\n```"
	got := StripCodeFences(input)
	if strings.Contains(got, "```") {
		t.Error("expected fences to be stripped")
	}
	if !strings.Contains(got, "some content") {
		t.Error("expected content to be preserved")
	}
}

func TestStripCodeFences_FenceWithoutClosing(t *testing.T) {
	input := "```json\n{\"key\":\"value\"}"
	got := StripCodeFences(input)
	if !strings.Contains(got, "key") {
		t.Error("expected content preserved")
	}
}

func TestStripCodeFences_WhitespaceAround(t *testing.T) {
	input := "  ```python\nprint('hello')\n```  "
	got := StripCodeFences(input)
	if strings.HasPrefix(got, "```") {
		t.Error("expected opening fence stripped")
	}
}

func TestStripCodeFences_EmptyInput(t *testing.T) {
	got := StripCodeFences("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestStripCodeFences_OnlyFences(t *testing.T) {
	input := "```\n```"
	got := StripCodeFences(input)
	if got == input {
		t.Error("expected fences to be processed")
	}
}

// --- IsOverloaded coverage (currently 0%) ---

func TestIsOverloaded_529Status(t *testing.T) {
	err := &APIError{StatusCode: 529, Message: "overloaded"}
	if !IsOverloaded(err) {
		t.Error("expected true for 529")
	}
}

func TestIsOverloaded_200Status(t *testing.T) {
	err := &APIError{StatusCode: 200, Message: "ok"}
	if IsOverloaded(err) {
		t.Error("expected false for 200")
	}
}

func TestIsOverloaded_NonAPIError(t *testing.T) {
	err := context.DeadlineExceeded
	if IsOverloaded(err) {
		t.Error("expected false for non-API error")
	}
}

// --- classifyCLIError coverage (currently 66.7%) ---

func TestClassifyCLIError_CreditBalance(t *testing.T) {
	err := classifyCLIError(context.DeadlineExceeded, []byte("Your credit balance is too low"))
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatal("expected APIError")
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected 400, got %d", apiErr.StatusCode)
	}
	if apiErr.Retryable {
		t.Error("credit balance errors should not be retryable")
	}
}

func TestClassifyCLIError_Authentication(t *testing.T) {
	err := classifyCLIError(context.DeadlineExceeded, []byte("Authentication failed"))
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatal("expected APIError")
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("expected 401, got %d", apiErr.StatusCode)
	}
}

func TestClassifyCLIError_RateLimit(t *testing.T) {
	err := classifyCLIError(context.DeadlineExceeded, []byte("rate limit exceeded"))
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatal("expected APIError")
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("expected 429, got %d", apiErr.StatusCode)
	}
	if !apiErr.Retryable {
		t.Error("rate limit errors should be retryable")
	}
}

func TestClassifyCLIError_GenericError(t *testing.T) {
	err := classifyCLIError(context.DeadlineExceeded, []byte("something else went wrong"))
	var apiErr *APIError
	if asAPIError(err, &apiErr) {
		t.Error("generic errors should not be APIError")
	}
	if err == nil {
		t.Error("expected non-nil error")
	}
}

func TestClassifyCLIError_BillingKeyword(t *testing.T) {
	err := classifyCLIError(context.DeadlineExceeded, []byte("billing issue detected"))
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatal("expected APIError")
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected 400, got %d", apiErr.StatusCode)
	}
}

func TestClassifyCLIError_Unauthorized(t *testing.T) {
	err := classifyCLIError(context.DeadlineExceeded, []byte("unauthorized access"))
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatal("expected APIError")
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("expected 401, got %d", apiErr.StatusCode)
	}
}

func TestClassifyCLIError_TooManyRequests(t *testing.T) {
	err := classifyCLIError(context.DeadlineExceeded, []byte("too many requests"))
	var apiErr *APIError
	if !asAPIError(err, &apiErr) {
		t.Fatal("expected APIError")
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("expected 429, got %d", apiErr.StatusCode)
	}
}

// --- WithSkipPermissions coverage (currently 0%) ---

func TestWithSkipPermissions(t *testing.T) {
	c := NewClaudeCLIClient()
	c2 := c.WithSkipPermissions()
	if c2 == nil {
		t.Error("expected non-nil client")
	}
}

// --- RetryAfterSeconds edge cases ---

func TestRetryAfterSeconds_WithValue(t *testing.T) {
	err := &APIError{StatusCode: 429, Message: "rate limited", RetryAfter: 30}
	got := RetryAfterSeconds(err)
	if got != 30 {
		t.Errorf("expected 30, got %d", got)
	}
}

func TestRetryAfterSeconds_ZeroValue(t *testing.T) {
	err := &APIError{StatusCode: 429, Message: "rate limited", RetryAfter: 0}
	got := RetryAfterSeconds(err)
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// --- QuotaTracker maybeReset ---

func TestQuotaTracker_IsExhausted_WithCooldown(t *testing.T) {
	qt := NewQuotaTracker(10, 100)
	qt.MarkExhausted(1 * time.Second)
	if !qt.IsExhausted() {
		t.Error("expected exhausted during cooldown")
	}
	time.Sleep(1100 * time.Millisecond)
	if qt.IsExhausted() {
		t.Error("expected not exhausted after cooldown expires")
	}
}

func TestQuotaTracker_RecordAndExhaust(t *testing.T) {
	qt := NewQuotaTracker(2, 100)
	// Record requests up to 90% of limit (2 * 0.9 = 1.8, so 2 records should trigger)
	qt.RecordRequest()
	qt.RecordRequest()
	if !qt.IsExhausted() {
		t.Error("expected exhausted at 100% of rpm limit")
	}
}

// --- buildCLIPrompt coverage ---

func TestBuildCLIPrompt_WithSystemPrompt(t *testing.T) {
	req := CompletionRequest{
		System: "You are a helpful assistant",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
		},
	}
	prompt := buildCLIPrompt(req)
	if !strings.Contains(prompt, "helpful assistant") {
		t.Error("expected system prompt in output")
	}
	if !strings.Contains(prompt, "Hello") {
		t.Error("expected user message in output")
	}
	// buildCLIPrompt concatenates system + user content, no injection text
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
}

func TestBuildCLIPrompt_NoSystemPrompt(t *testing.T) {
	req := CompletionRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "Question?"},
		},
	}
	prompt := buildCLIPrompt(req)
	if !strings.Contains(prompt, "Question?") {
		t.Error("expected user message")
	}
}

func TestBuildCLIPrompt_MultipleMessages(t *testing.T) {
	req := CompletionRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "First"},
			{Role: "assistant", Content: "Reply"},
			{Role: RoleUser, Content: "Second"},
		},
	}
	prompt := buildCLIPrompt(req)
	if !strings.Contains(prompt, "First") {
		t.Error("expected first user message")
	}
	if !strings.Contains(prompt, "Second") {
		t.Error("expected second user message")
	}
	// Assistant messages should be filtered out
	if strings.Contains(prompt, "Reply") {
		t.Error("assistant messages should not be included")
	}
}

// --- DryRunClient planningResponse edge cases ---

func TestDryRunClient_PlanningLongRequirement(t *testing.T) {
	c := NewDryRunClient(0)
	resp, err := c.Complete(context.Background(), CompletionRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("a", 200)},
		},
		System: "You are a tech_lead",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty response")
	}
}

// --- trimCodeFences (alias) ---

func TestTrimCodeFences_IsSameAsStripCodeFences(t *testing.T) {
	input := "```json\n{\"key\":\"value\"}\n```"
	got := trimCodeFences(input)
	want := StripCodeFences(input)
	if got != want {
		t.Errorf("trimCodeFences and StripCodeFences should produce same result")
	}
}

// --- IsFatalAPIError edge cases ---

func TestIsFatalAPIError_403(t *testing.T) {
	err := &APIError{StatusCode: 403, Message: "forbidden"}
	if !IsFatalAPIError(err) {
		t.Error("expected 403 to be fatal")
	}
}

func TestIsFatalAPIError_500(t *testing.T) {
	err := &APIError{StatusCode: 500, Message: "server error"}
	if IsFatalAPIError(err) {
		t.Error("expected 500 to not be fatal")
	}
}

// helper
func asAPIError(err error, target **APIError) bool {
	if ae, ok := err.(*APIError); ok {
		*target = ae
		return true
	}
	return false
}
