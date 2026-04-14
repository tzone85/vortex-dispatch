package agent

import (
	"strings"
	"testing"
)

// TestRenderAttemptHistory_SingleAttempt verifies rendering of a single attempt.
func TestRenderAttemptHistory_SingleAttempt(t *testing.T) {
	attempts := []AttemptSummary{
		{Number: 1, Role: "junior", Outcome: "qa_failed", Error: "build failed"},
	}
	result := renderAttemptHistory(attempts)
	if !strings.Contains(result, "Prior Attempts") {
		t.Error("should contain Prior Attempts header")
	}
	if !strings.Contains(result, "1 time(s)") {
		t.Error("should indicate 1 prior attempt")
	}
	if !strings.Contains(result, "Attempt 1") {
		t.Error("should contain Attempt 1")
	}
	if !strings.Contains(result, "junior") {
		t.Error("should contain role 'junior'")
	}
	if !strings.Contains(result, "build failed") {
		t.Error("should contain error message")
	}
	if !strings.Contains(result, "DIFFERENT approach") {
		t.Error("should instruct different approach")
	}
}

// TestRenderAttemptHistory_MultipleAttempts verifies multiple attempts render.
func TestRenderAttemptHistory_MultipleAttempts(t *testing.T) {
	attempts := []AttemptSummary{
		{Number: 1, Role: "junior", Outcome: "qa_failed", Error: "test failed"},
		{Number: 2, Role: "intermediate", Outcome: "review_failed", Error: "missing validation"},
		{Number: 3, Role: "senior", Outcome: "qa_failed", Error: "race condition"},
	}
	result := renderAttemptHistory(attempts)
	if !strings.Contains(result, "3 time(s)") {
		t.Error("should indicate 3 prior attempts")
	}
	if !strings.Contains(result, "Attempt 1") {
		t.Error("should contain Attempt 1")
	}
	if !strings.Contains(result, "Attempt 2") {
		t.Error("should contain Attempt 2")
	}
	if !strings.Contains(result, "Attempt 3") {
		t.Error("should contain Attempt 3")
	}
	if !strings.Contains(result, "race condition") {
		t.Error("should contain last error")
	}
}

// TestRenderAttemptHistory_AttemptWithoutError verifies attempt with empty error.
func TestRenderAttemptHistory_AttemptWithoutError(t *testing.T) {
	attempts := []AttemptSummary{
		{Number: 1, Role: "junior", Outcome: "stuck", Error: ""},
	}
	result := renderAttemptHistory(attempts)
	if !strings.Contains(result, "Outcome: stuck") {
		t.Error("should contain outcome")
	}
	// No "Error:" line should appear for empty error
	if strings.Contains(result, "Error:") {
		t.Error("should not contain Error: line when error is empty")
	}
}

// TestRenderGoalWithAttempts_HighComplexity_UseSeniorRole verifies complexity
// routing in RenderGoalWithAttempts.
func TestRenderGoalWithAttempts_HighComplexity_UseSeniorRole(t *testing.T) {
	ctx := TemplateContext{
		StoryID:            "s-010",
		StoryTitle:         "Complex refactor",
		StoryDescription:   "Major architecture change",
		AcceptanceCriteria: "- Everything works",
		Complexity:         8,
		IsRetry:            false,
	}
	result := RenderGoalWithAttempts(ctx)
	if !strings.Contains(result, "s-010") {
		t.Error("should contain story ID")
	}
}

// TestRenderGoalWithAttempts_MedComplexity verifies intermediate routing.
func TestRenderGoalWithAttempts_MedComplexity(t *testing.T) {
	ctx := TemplateContext{
		StoryID:            "s-011",
		StoryTitle:         "Add cache",
		StoryDescription:   "Implement redis cache",
		AcceptanceCriteria: "- Cache works",
		Complexity:         3, // routes to intermediate
	}
	result := RenderGoalWithAttempts(ctx)
	if !strings.Contains(result, "s-011") {
		t.Error("should contain story ID")
	}
}

// TestRenderGoalWithAttempts_RetryWithNoAttempts verifies that IsRetry true
// but empty PriorAttempts does not add attempt section.
func TestRenderGoalWithAttempts_RetryWithNoAttempts(t *testing.T) {
	ctx := TemplateContext{
		StoryID:            "s-012",
		StoryTitle:         "Retry story",
		StoryDescription:   "Some task",
		AcceptanceCriteria: "- Done",
		IsRetry:            true,
		PriorAttempts:      nil,
	}
	result := RenderGoalWithAttempts(ctx)
	if strings.Contains(result, "Prior Attempts") {
		t.Error("should not add attempt section when PriorAttempts is empty")
	}
}

// TestRenderGoalWithAttempts_WithExistingCodebase verifies existing codebase
// flag is preserved through RenderGoalWithAttempts.
func TestRenderGoalWithAttempts_WithExistingCodebase(t *testing.T) {
	ctx := TemplateContext{
		StoryID:            "s-013",
		StoryTitle:         "Legacy fix",
		StoryDescription:   "Fix old code",
		AcceptanceCriteria: "- Passes tests",
		IsExistingCodebase: true,
		Complexity:         2,
	}
	result := RenderGoalWithAttempts(ctx)
	if !strings.Contains(result, "EXISTING CODEBASE") {
		t.Error("should include existing codebase workflow")
	}
}

// TestRenderGoalWithAttempts_WithWaveContextAndRetry verifies both wave
// context and attempt history are included.
func TestRenderGoalWithAttempts_WithWaveContextAndRetry(t *testing.T) {
	ctx := TemplateContext{
		StoryID:            "s-014",
		StoryTitle:         "Follow-up fix",
		StoryDescription:   "Fix based on prior work",
		AcceptanceCriteria: "- Works",
		WaveContext:        "Story s-013 created the utils package.",
		IsRetry:            true,
		RetryNumber:        2,
		PriorAttempts: []AttemptSummary{
			{Number: 1, Role: "junior", Outcome: "qa_failed", Error: "missing test"},
		},
		Complexity: 5,
	}
	result := RenderGoalWithAttempts(ctx)
	if !strings.Contains(result, "What Prior Stories Built") {
		t.Error("should contain wave context")
	}
	if !strings.Contains(result, "Prior Attempts") {
		t.Error("should contain attempt history")
	}
	if !strings.Contains(result, "missing test") {
		t.Error("should contain error from prior attempt")
	}
}

// TestRenderTemplate_MissingField verifies template renders with zero-value
// when a field is not set.
func TestRenderTemplate_MissingField(t *testing.T) {
	tmpl := "Complexity: {{.Complexity}}"
	ctx := TemplateContext{} // Complexity defaults to 0
	result := RenderTemplate(tmpl, ctx)
	if result != "Complexity: 0" {
		t.Errorf("expected 'Complexity: 0', got %q", result)
	}
}

// TestRenderTemplate_ExecutionError verifies fallback on template execution error.
func TestRenderTemplate_ExecutionError(t *testing.T) {
	// Use a template that calls a method that doesn't exist on the struct
	tmpl := "{{.NonExistentMethod}}"
	result := RenderTemplate(tmpl, TemplateContext{})
	// Should fall back to raw template string
	if result != tmpl {
		t.Errorf("expected raw template on execution error, got %q", result)
	}
}

// TestRenderGoalWithAttempts_ReviewFeedback verifies review feedback is passed through.
func TestRenderGoalWithAttempts_ReviewFeedback(t *testing.T) {
	ctx := TemplateContext{
		StoryID:            "s-015",
		StoryTitle:         "Fix review issues",
		StoryDescription:   "Address feedback",
		AcceptanceCriteria: "- Done",
		ReviewFeedback:     "Missing error handling on line 42",
	}
	result := RenderGoalWithAttempts(ctx)
	if !strings.Contains(result, "Previous Review Feedback") {
		t.Error("should contain review feedback section")
	}
	if !strings.Contains(result, "line 42") {
		t.Error("should contain actual review feedback")
	}
}
