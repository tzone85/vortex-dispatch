package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// internalMockRunner is a test double for CommandRunner used in internal tests.
type internalMockRunner struct {
	results map[string]mockResult
}

type mockResult struct {
	output string
	err    error
}

func (r *internalMockRunner) Run(_ context.Context, _, name string, args ...string) (string, error) {
	key := name
	if len(args) > 0 {
		key = name + " " + args[0]
	}
	if res, ok := r.results[key]; ok {
		return res.output, res.err
	}
	if res, ok := r.results[name]; ok {
		return res.output, res.err
	}
	return "", nil
}

func newQATestStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "proj.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	cleanup := func() {
		es.Close()
		ps.Close()
	}
	return es, ps, cleanup
}

func TestQA_Run_AllPassInternal(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	// Pre-populate story
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
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

	result, err := qa.Run(context.Background(), "s-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if !result.Passed {
		t.Error("expected QA to pass")
	}
	if len(result.Checks) != 3 {
		t.Errorf("expected 3 checks, got %d", len(result.Checks))
	}

	// Verify events
	started, _ := es.List(state.EventFilter{Type: state.EventStoryQAStarted})
	if len(started) != 1 {
		t.Errorf("expected 1 QA_STARTED event, got %d", len(started))
	}
	passed, _ := es.List(state.EventFilter{Type: state.EventStoryQAPassed})
	if len(passed) != 1 {
		t.Errorf("expected 1 QA_PASSED event, got %d", len(passed))
	}
}

func TestQA_Run_BuildFails(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"golangci-lint": {output: "ok", err: nil},
		"go":            {output: "build error: undefined", err: fmt.Errorf("exit 1")},
	}}

	qa := NewQA(QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Error("expected QA to fail when build fails")
	}

	// Verify QA_FAILED event
	failed, _ := es.List(state.EventFilter{Type: state.EventStoryQAFailed})
	if len(failed) != 1 {
		t.Errorf("expected 1 QA_FAILED event, got %d", len(failed))
	}
}

func TestQA_Run_EmptyCommands(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{}}

	qa := NewQA(QAConfig{
		// All commands empty — should skip
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if !result.Passed {
		t.Error("expected QA to pass with no commands")
	}
	if len(result.Checks) != 0 {
		t.Errorf("expected 0 checks, got %d", len(result.Checks))
	}
}

func TestQA_Run_WithSuccessCriteria(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	runner := &internalMockRunner{results: map[string]mockResult{
		"go": {output: "ok", err: nil},
	}}

	qa := NewQA(QAConfig{
		BuildCommand: "go build ./...",
		SuccessCriteria: []Criterion{
			{Kind: "output_contains", Value: "NOTFOUND"},
		},
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-001", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Error("expected QA to fail due to unmet success criterion")
	}

	// Should have build check + criterion check
	if len(result.Checks) < 2 {
		t.Errorf("expected at least 2 checks, got %d", len(result.Checks))
	}
}

func TestQA_RunCheck_EmptyCommand(t *testing.T) {
	es, ps, cleanup := newQATestStores(t)
	defer cleanup()

	runner := &internalMockRunner{results: map[string]mockResult{}}
	qa := NewQA(QAConfig{}, runner, es, ps)

	result := qa.runCheck(context.Background(), "/tmp", "test", "")
	if result.Passed {
		t.Error("expected failure for empty command")
	}
	if result.Output != "empty command" {
		t.Errorf("expected 'empty command', got %q", result.Output)
	}
}
