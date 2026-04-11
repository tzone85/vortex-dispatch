package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateCriteria_OutputContains(t *testing.T) {
	criteria := []Criterion{
		{Kind: CriterionOutputContains, Value: "PASS"},
	}
	results := EvaluateCriteria(criteria, "", "All tests PASS")
	if !AllPassed(results) {
		t.Error("output_contains should pass when value is present")
	}

	results = EvaluateCriteria(criteria, "", "All tests FAIL")
	if AllPassed(results) {
		t.Error("output_contains should fail when value is absent")
	}
}

func TestEvaluateCriteria_OutputNotContains(t *testing.T) {
	criteria := []Criterion{
		{Kind: CriterionOutputNotContains, Value: "FATAL"},
	}
	results := EvaluateCriteria(criteria, "", "All good")
	if !AllPassed(results) {
		t.Error("output_not_contains should pass when value is absent")
	}

	results = EvaluateCriteria(criteria, "", "FATAL error occurred")
	if AllPassed(results) {
		t.Error("output_not_contains should fail when value is present")
	}
}

func TestEvaluateCriteria_FileExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "coverage.html"), []byte("<html>"), 0644); err != nil {
		t.Fatalf("setup: write file: %v", err)
	}

	criteria := []Criterion{
		{Kind: CriterionFileExists, Path: "coverage.html"},
	}
	results := EvaluateCriteria(criteria, dir, "")
	if !AllPassed(results) {
		t.Error("file_exists should pass when file exists")
	}

	criteria = []Criterion{
		{Kind: CriterionFileExists, Path: "missing.txt"},
	}
	results = EvaluateCriteria(criteria, dir, "")
	if AllPassed(results) {
		t.Error("file_exists should fail when file is missing")
	}
}

func TestEvaluateCriteria_FileContains(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "output.log"), []byte("Build succeeded"), 0644); err != nil {
		t.Fatalf("setup: write file: %v", err)
	}

	criteria := []Criterion{
		{Kind: CriterionFileContains, Path: "output.log", Value: "succeeded"},
	}
	results := EvaluateCriteria(criteria, dir, "")
	if !AllPassed(results) {
		t.Error("file_contains should pass when file contains value")
	}

	criteria = []Criterion{
		{Kind: CriterionFileContains, Path: "output.log", Value: "failed"},
	}
	results = EvaluateCriteria(criteria, dir, "")
	if AllPassed(results) {
		t.Error("file_contains should fail when file does not contain value")
	}
}

func TestEvaluateCriteria_FileContains_MissingFile(t *testing.T) {
	dir := t.TempDir()

	criteria := []Criterion{
		{Kind: CriterionFileContains, Path: "nonexistent.log", Value: "anything"},
	}
	results := EvaluateCriteria(criteria, dir, "")
	if AllPassed(results) {
		t.Error("file_contains should fail when file does not exist")
	}
	if !strings.Contains(results[0].Detail, "cannot read") {
		t.Errorf("expected 'cannot read' in detail, got: %s", results[0].Detail)
	}
}

func TestEvaluateCriteria_FileNotEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"ok": true}`), 0644); err != nil {
		t.Fatalf("setup: write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.json"), []byte(""), 0644); err != nil {
		t.Fatalf("setup: write file: %v", err)
	}

	criteria := []Criterion{
		{Kind: CriterionFileNotEmpty, Path: "data.json"},
	}
	results := EvaluateCriteria(criteria, dir, "")
	if !AllPassed(results) {
		t.Error("file_not_empty should pass when file has content")
	}

	criteria = []Criterion{
		{Kind: CriterionFileNotEmpty, Path: "empty.json"},
	}
	results = EvaluateCriteria(criteria, dir, "")
	if AllPassed(results) {
		t.Error("file_not_empty should fail when file is empty")
	}
}

func TestEvaluateCriteria_FileNotEmpty_MissingFile(t *testing.T) {
	dir := t.TempDir()

	criteria := []Criterion{
		{Kind: CriterionFileNotEmpty, Path: "missing.json"},
	}
	results := EvaluateCriteria(criteria, dir, "")
	if AllPassed(results) {
		t.Error("file_not_empty should fail when file does not exist")
	}
}

func TestEvaluateCriteria_ExitCodeZero(t *testing.T) {
	criteria := []Criterion{
		{Kind: CriterionExitCodeZero},
	}
	results := EvaluateCriteria(criteria, "", "")
	if !AllPassed(results) {
		t.Error("exit_code_zero should pass (delegated to QA runner)")
	}
}

func TestEvaluateCriteria_CustomMessage(t *testing.T) {
	criteria := []Criterion{
		{Kind: CriterionOutputContains, Value: "LGTM", Message: "Code review must pass"},
	}
	results := EvaluateCriteria(criteria, "", "rejected")
	summary := CriteriaFailureSummary(results)
	if summary == "" {
		t.Fatal("expected failure summary")
	}
	if !strings.Contains(summary, "Code review must pass") {
		t.Errorf("summary should use custom message, got: %s", summary)
	}
}

func TestEvaluateCriteria_MultipleMixed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "result.txt"), []byte("success"), 0644); err != nil {
		t.Fatalf("setup: write file: %v", err)
	}

	criteria := []Criterion{
		{Kind: CriterionOutputContains, Value: "PASS"},
		{Kind: CriterionFileExists, Path: "result.txt"},
		{Kind: CriterionFileContains, Path: "result.txt", Value: "success"},
	}
	results := EvaluateCriteria(criteria, dir, "All tests PASS")
	if !AllPassed(results) {
		t.Error("all criteria should pass")
	}
}

func TestEvaluateCriteria_UnknownKind(t *testing.T) {
	criteria := []Criterion{
		{Kind: "unknown_check"},
	}
	results := EvaluateCriteria(criteria, "", "")
	if AllPassed(results) {
		t.Error("unknown criterion kind should fail")
	}
	if !strings.Contains(results[0].Detail, "unknown criterion kind") {
		t.Errorf("expected 'unknown criterion kind' in detail, got: %s", results[0].Detail)
	}
}

func TestEvaluateCriteria_EmptyCriteria(t *testing.T) {
	results := EvaluateCriteria(nil, "", "")
	if !AllPassed(results) {
		t.Error("empty criteria should pass (vacuously true)")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil criteria, got %d", len(results))
	}
}

func TestCriteriaFailureSummary_AllPassed(t *testing.T) {
	results := []CriterionResult{
		{Criterion: Criterion{Kind: CriterionOutputContains, Value: "ok"}, Passed: true, Detail: "output contains ok"},
	}
	summary := CriteriaFailureSummary(results)
	if summary != "" {
		t.Errorf("expected empty summary when all passed, got: %s", summary)
	}
}

func TestCriteriaFailureSummary_MultipleFailures(t *testing.T) {
	results := []CriterionResult{
		{Criterion: Criterion{Kind: CriterionOutputContains, Value: "ok"}, Passed: true, Detail: "passed"},
		{Criterion: Criterion{Kind: CriterionFileExists, Path: "a.txt"}, Passed: false, Detail: "file a.txt not found"},
		{Criterion: Criterion{Kind: CriterionFileExists, Path: "b.txt"}, Passed: false, Detail: "file b.txt not found"},
	}
	summary := CriteriaFailureSummary(results)
	if !strings.Contains(summary, "file a.txt not found") {
		t.Errorf("summary should contain first failure detail, got: %s", summary)
	}
	if !strings.Contains(summary, "file b.txt not found") {
		t.Errorf("summary should contain second failure detail, got: %s", summary)
	}
}

func TestResolvePath_Absolute(t *testing.T) {
	got := resolvePath("/work", "/absolute/path.txt")
	if got != "/absolute/path.txt" {
		t.Errorf("absolute path should be unchanged, got: %s", got)
	}
}

func TestResolvePath_Relative(t *testing.T) {
	got := resolvePath("/work", "relative/path.txt")
	expected := filepath.Join("/work", "relative/path.txt")
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}
