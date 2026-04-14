package engine

import (
	"testing"
	"time"
)

func TestComputeQualityScore_AllPassed(t *testing.T) {
	result := QAResult{
		Passed: true,
		Checks: []QACheckResult{
			{Name: "lint", Passed: true},
			{Name: "build", Passed: true},
			{Name: "test", Passed: true},
		},
	}
	got := computeQualityScore(result)
	if got != 5 {
		t.Errorf("expected 5 for all passed, got %d", got)
	}
}

func TestComputeQualityScore_Failed(t *testing.T) {
	result := QAResult{
		Passed: false,
		Checks: []QACheckResult{
			{Name: "lint", Passed: false},
		},
	}
	got := computeQualityScore(result)
	if got != 1 {
		t.Errorf("expected 1 for failed result, got %d", got)
	}
}

func TestComputeQualityScore_NoChecks(t *testing.T) {
	result := QAResult{Passed: true, Checks: nil}
	got := computeQualityScore(result)
	if got != 3 {
		t.Errorf("expected 3 for no checks, got %d", got)
	}
}

func TestComputeQualityScore_80PercentRatio(t *testing.T) {
	result := QAResult{
		Passed: true,
		Checks: []QACheckResult{
			{Name: "lint", Passed: true},
			{Name: "build", Passed: true},
			{Name: "test", Passed: true},
			{Name: "criterion:output_contains", Passed: true},
			{Name: "criterion:file_exists", Passed: false},
		},
	}
	got := computeQualityScore(result)
	if got != 4 {
		t.Errorf("expected 4 for 80%% ratio, got %d", got)
	}
}

func TestComputeQualityScore_60PercentRatio(t *testing.T) {
	result := QAResult{
		Passed: true,
		Checks: []QACheckResult{
			{Name: "lint", Passed: true},
			{Name: "build", Passed: true},
			{Name: "test", Passed: true},
			{Name: "c1", Passed: false},
			{Name: "c2", Passed: false},
		},
	}
	got := computeQualityScore(result)
	if got != 3 {
		t.Errorf("expected 3 for 60%% ratio, got %d", got)
	}
}

func TestComputeQualityScore_40PercentRatio(t *testing.T) {
	result := QAResult{
		Passed: true,
		Checks: []QACheckResult{
			{Name: "lint", Passed: true},
			{Name: "build", Passed: true},
			{Name: "test", Passed: false},
			{Name: "c1", Passed: false},
			{Name: "c2", Passed: false},
		},
	}
	got := computeQualityScore(result)
	if got != 2 {
		t.Errorf("expected 2 for 40%% ratio, got %d", got)
	}
}

func TestTotalDuration_SumsAll(t *testing.T) {
	result := QAResult{
		Checks: []QACheckResult{
			{Name: "lint", Elapsed: 2 * time.Second},
			{Name: "build", Elapsed: 3 * time.Second},
			{Name: "test", Elapsed: 5 * time.Second},
		},
	}
	got := totalDuration(result)
	if got != 10 {
		t.Errorf("expected 10 seconds, got %d", got)
	}
}

func TestTotalDuration_Empty(t *testing.T) {
	result := QAResult{Checks: nil}
	got := totalDuration(result)
	if got != 0 {
		t.Errorf("expected 0 seconds for no checks, got %d", got)
	}
}

func TestQAResult_FailureSummary_AllPassed(t *testing.T) {
	result := QAResult{
		Passed: true,
		Checks: []QACheckResult{
			{Name: "lint", Passed: true},
		},
	}
	got := result.FailureSummary()
	if got != "" {
		t.Errorf("expected empty summary for passed result, got %q", got)
	}
}

func TestQAResult_FailureSummary_WithFailures(t *testing.T) {
	result := QAResult{
		Passed: false,
		Checks: []QACheckResult{
			{Name: "lint", Passed: false, Output: "unused variable x"},
			{Name: "build", Passed: true, Output: "ok"},
			{Name: "test", Passed: false, Output: "FAIL: TestFoo"},
		},
	}
	got := result.FailureSummary()
	if got == "" {
		t.Fatal("expected non-empty failure summary")
	}
	if !contains(got, "[LINT FAILED]") {
		t.Error("expected lint failure in summary")
	}
	if !contains(got, "[TEST FAILED]") {
		t.Error("expected test failure in summary")
	}
	if contains(got, "[BUILD FAILED]") {
		t.Error("did not expect build failure in summary")
	}
}

func TestQAResult_FailureSummary_TruncatesLongOutput(t *testing.T) {
	longOutput := ""
	for i := 0; i < 600; i++ {
		longOutput += "x"
	}
	result := QAResult{
		Passed: false,
		Checks: []QACheckResult{
			{Name: "test", Passed: false, Output: longOutput},
		},
	}
	got := result.FailureSummary()
	if !contains(got, "(truncated)") {
		t.Error("expected truncation marker for long output")
	}
}

func TestQAResult_FailureSummary_NoDetails(t *testing.T) {
	result := QAResult{
		Passed: false,
		Checks: []QACheckResult{},
	}
	got := result.FailureSummary()
	if got != "QA checks failed (no details available)" {
		t.Errorf("expected fallback message, got %q", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
