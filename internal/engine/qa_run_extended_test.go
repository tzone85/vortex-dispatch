package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestQA_Run_LintFails verifies QA correctly reports lint failures while build/test pass.
func TestQA_Run_LintFails(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-lint-001", map[string]any{
		"id": "s-lint-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"golangci-lint": {output: "main.go:10: unused variable 'x'", err: fmt.Errorf("exit 1")},
		"go":            {output: "ok", err: nil},
	}}

	qa := NewQA(QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-lint-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Error("expected QA to fail when lint fails")
	}

	// Verify exactly 3 checks were run.
	if len(result.Checks) != 3 {
		t.Errorf("expected 3 checks, got %d", len(result.Checks))
	}

	// Lint should be the failed one.
	lintCheck := result.Checks[0]
	if lintCheck.Passed {
		t.Error("expected lint check to fail")
	}
	if lintCheck.Name != "lint" {
		t.Errorf("expected first check to be 'lint', got %q", lintCheck.Name)
	}

	// Build and test should pass.
	if !result.Checks[1].Passed {
		t.Error("expected build check to pass")
	}
	if !result.Checks[2].Passed {
		t.Error("expected test check to pass")
	}

	// QA_FAILED event should capture the failed check names.
	failedEvents, _ := es.List(state.EventFilter{Type: state.EventStoryQAFailed, StoryID: "s-lint-001"})
	if len(failedEvents) != 1 {
		t.Fatalf("expected 1 QA_FAILED event, got %d", len(failedEvents))
	}

	payload := state.DecodePayload(failedEvents[0].Payload)
	failedChecks, ok := payload["failed_checks"].([]any)
	if !ok {
		t.Fatal("expected failed_checks array in payload")
	}
	if len(failedChecks) != 1 || failedChecks[0] != "lint" {
		t.Errorf("expected [lint] in failed checks, got %v", failedChecks)
	}
}

// TestQA_Run_AllChecksFail verifies behavior when all QA checks fail.
func TestQA_Run_AllChecksFail(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-all-fail", map[string]any{
		"id": "s-all-fail", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"golangci-lint": {output: "lint error", err: fmt.Errorf("exit 1")},
		"go":            {output: "build error", err: fmt.Errorf("exit 1")},
	}}

	qa := NewQA(QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-all-fail", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Error("expected QA to fail when all checks fail")
	}

	// All 3 checks should be recorded, all failed.
	failedCount := 0
	for _, c := range result.Checks {
		if !c.Passed {
			failedCount++
		}
	}
	if failedCount != 3 {
		t.Errorf("expected 3 failed checks, got %d", failedCount)
	}

	// Quality score should be 1 (worst).
	score := computeQualityScore(result)
	if score != 1 {
		t.Errorf("expected quality score 1, got %d", score)
	}
}

// TestQA_Run_WithSuccessCriteria_FileExists verifies file_exists criterion.
func TestQA_Run_WithSuccessCriteria_FileExists(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-crit-001", map[string]any{
		"id": "s-crit-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	worktreeDir := t.TempDir()
	// Create the expected file.
	os.WriteFile(filepath.Join(worktreeDir, "coverage.html"), []byte("<html>coverage</html>"), 0o644)

	runner := &internalMockRunner{results: map[string]mockResult{
		"go": {output: "ok", err: nil},
	}}

	qa := NewQA(QAConfig{
		BuildCommand: "go build ./...",
		SuccessCriteria: []Criterion{
			{Kind: "file_exists", Path: "coverage.html"},
		},
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-crit-001", worktreeDir)
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if !result.Passed {
		t.Error("expected QA to pass when file exists")
	}

	// Should have build check + criterion check.
	if len(result.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(result.Checks))
	}
}

// TestQA_Run_WithSuccessCriteria_FileNotExists verifies failure when file missing.
func TestQA_Run_WithSuccessCriteria_FileNotExists(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-crit-002", map[string]any{
		"id": "s-crit-002", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	worktreeDir := t.TempDir()
	// Do NOT create coverage.html.

	runner := &internalMockRunner{results: map[string]mockResult{
		"go": {output: "ok", err: nil},
	}}

	qa := NewQA(QAConfig{
		BuildCommand: "go build ./...",
		SuccessCriteria: []Criterion{
			{Kind: "file_exists", Path: "coverage.html"},
		},
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-crit-002", worktreeDir)
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Error("expected QA to fail when required file is missing")
	}
}

// TestQA_Run_PartialCommands verifies that only configured commands run.
func TestQA_Run_PartialCommands(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-partial-001", map[string]any{
		"id": "s-partial-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"go": {output: "ok", err: nil},
	}}

	// Only build command configured, no lint or test.
	qa := NewQA(QAConfig{
		BuildCommand: "go build ./...",
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-partial-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if !result.Passed {
		t.Error("expected QA to pass with only build command")
	}
	if len(result.Checks) != 1 {
		t.Errorf("expected 1 check (build only), got %d", len(result.Checks))
	}
	if result.Checks[0].Name != "build" {
		t.Errorf("expected check name 'build', got %q", result.Checks[0].Name)
	}
}

// TestQA_Run_FailureSummary_Integration verifies that FailureSummary works
// correctly on a real QA result.
func TestQA_Run_FailureSummary_Integration(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-fs-001", map[string]any{
		"id": "s-fs-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"golangci-lint": {output: "ok", err: nil},
		"go build":      {output: "compilation error: undefined foo", err: fmt.Errorf("exit 1")},
	}}

	qa := NewQA(QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-fs-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}

	summary := result.FailureSummary()
	if summary == "" {
		t.Fatal("expected non-empty failure summary")
	}
	if !contains(summary, "[BUILD FAILED]") {
		t.Error("expected build failure in summary")
	}
}

// TestQA_Run_QualityScoreInEvent verifies the quality_score is correct in the
// emitted QA event payload.
func TestQA_Run_QualityScoreInEvent(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-qs-001", map[string]any{
		"id": "s-qs-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"golangci-lint": {output: "ok", err: nil},
		"go":            {output: "ok", err: nil},
	}}

	qa := NewQA(QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
	}, runner, es, ps)

	_, err := qa.Run(context.Background(), "s-qs-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}

	passedEvents, _ := es.List(state.EventFilter{Type: state.EventStoryQAPassed, StoryID: "s-qs-001"})
	if len(passedEvents) != 1 {
		t.Fatalf("expected 1 QA_PASSED event, got %d", len(passedEvents))
	}

	payload := state.DecodePayload(passedEvents[0].Payload)
	score, ok := payload["quality_score"].(float64)
	if !ok {
		t.Fatal("expected quality_score in payload")
	}
	if int(score) != 5 {
		t.Errorf("expected quality score 5 for all passed, got %d", int(score))
	}

	totalChecks, ok := payload["total_checks"].(float64)
	if !ok || int(totalChecks) != 3 {
		t.Errorf("expected total_checks 3, got %v", payload["total_checks"])
	}
}

// TestQA_Run_SkipsEmptyCommands verifies that empty command strings are skipped
// and don't produce check results.
func TestQA_Run_OnlyLintAndTest(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-lt-001", map[string]any{
		"id": "s-lt-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"golangci-lint": {output: "ok", err: nil},
		"go":            {output: "ok", err: nil},
	}}

	// No build command.
	qa := NewQA(QAConfig{
		LintCommand: "golangci-lint run",
		TestCommand: "go test ./...",
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-lt-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if !result.Passed {
		t.Error("expected QA to pass")
	}
	// Should have lint + test only, not build.
	if len(result.Checks) != 2 {
		t.Errorf("expected 2 checks (lint+test), got %d", len(result.Checks))
	}
}

// TestQA_Run_MultipleCriteria verifies multiple success criteria evaluated together.
func TestQA_Run_MultipleCriteria_MixedResults(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mc-001", map[string]any{
		"id": "s-mc-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	worktreeDir := t.TempDir()
	// Create one file but not the other.
	os.WriteFile(filepath.Join(worktreeDir, "report.html"), []byte("report"), 0o644)

	runner := &internalMockRunner{results: map[string]mockResult{}}

	qa := NewQA(QAConfig{
		SuccessCriteria: []Criterion{
			{Kind: "file_exists", Path: "report.html"},
			{Kind: "file_exists", Path: "missing.html"},
		},
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-mc-001", worktreeDir)
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Error("expected QA to fail when one criterion is unmet")
	}

	// Should have 2 criterion checks.
	if len(result.Checks) != 2 {
		t.Errorf("expected 2 criterion checks, got %d", len(result.Checks))
	}

	passedCount := 0
	for _, c := range result.Checks {
		if c.Passed {
			passedCount++
		}
	}
	if passedCount != 1 {
		t.Errorf("expected 1 passed criterion, got %d", passedCount)
	}
}
