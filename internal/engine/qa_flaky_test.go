package engine_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// qa_flaky_test.go — F5 flaky-QA detection + auto-retry.
//
// Contract (feat/flaky-qa): a FAILED test step is re-run up to qa.flaky_retries
// times; passing on retry keeps QA green but emits STORY_QA_FLAKY (step,
// attempts) so flakiness stays visible. Lint/build failures are deterministic
// and are NEVER retried. flaky_retries=0 disables retrying entirely.

// sequenceRunner scripts per-command results consumed in call order; the last
// entry repeats once the script is exhausted (so "always fails" is a
// one-entry script). Keys follow the mockRunner scheme: "<cmd> <first-arg>".
type sequenceRunner struct {
	responses map[string][]mockRunResult
	calls     map[string]int
}

func newSequenceRunner(responses map[string][]mockRunResult) *sequenceRunner {
	return &sequenceRunner{responses: responses, calls: map[string]int{}}
}

func (s *sequenceRunner) Run(_ context.Context, _, name string, args ...string) (string, error) {
	key := name
	if len(args) > 0 {
		key = name + " " + args[0]
	}
	script, ok := s.responses[key]
	if !ok {
		script, ok = s.responses[name]
		if !ok {
			return "", errors.New("unexpected command: " + key)
		}
	}
	s.calls[key]++
	if key != name {
		s.calls[name]++
	}
	i := s.calls[key] - 1
	if i >= len(script) {
		i = len(script) - 1
	}
	return script[i].output, script[i].err
}

func (s *sequenceRunner) callCount(key string) int { return s.calls[key] }

// flakyTestStory projects a minimal story row so QA events have a target.
func flakyTestStory(t *testing.T, ps state.ProjectionStore, storyID string) {
	t.Helper()
	err := ps.Project(state.NewEvent(state.EventStoryCreated, "tech-lead", storyID, map[string]any{
		"id": storyID, "req_id": "r-flaky", "title": "Flaky story", "description": "d", "complexity": 2,
	}))
	if err != nil {
		t.Fatalf("project story created: %v", err)
	}
}

func TestQA_FlakyRetryPassesSecondAttempt(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()
	flakyTestStory(t, ps, "s-flaky-pass")

	runner := newSequenceRunner(map[string][]mockRunResult{
		"golangci-lint": {{output: "ok"}},
		"go build":      {{output: "ok"}},
		"go test": {
			{output: "--- FAIL: TestThing", err: errors.New("exit status 1")},
			{output: "ok  \\tpkg", err: nil},
		},
	})

	qa := engine.NewQA(engine.QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
		FlakyRetries: 1,
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-flaky-pass", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected QA green after retry, got %#v", result.Checks)
	}
	if runner.callCount("go test") != 2 {
		t.Fatalf("test step executed %d times, want 2 (fail once, pass on retry)", runner.callCount("go test"))
	}
	if len(result.FlakySteps) != 1 || result.FlakySteps[0].Step != "test" || result.FlakySteps[0].Attempts != 2 {
		t.Fatalf("expected one flaky step {test attempts=2}, got %+v", result.FlakySteps)
	}

	flakyEvts, err := es.List(state.EventFilter{Type: state.EventStoryQAFlaky})
	if err != nil {
		t.Fatalf("list flaky events: %v", err)
	}
	if len(flakyEvts) != 1 {
		t.Fatalf("expected 1 STORY_QA_FLAKY event, got %d", len(flakyEvts))
	}
	payload := state.DecodePayload(flakyEvts[0].Payload)
	if payload["step"] != "test" {
		t.Errorf("flaky event step = %v, want test", payload["step"])
	}
	if attempts, ok := payload["attempts"].(float64); !ok || attempts != 2 {
		t.Errorf("flaky event attempts = %v, want 2", payload["attempts"])
	}

	passed, err := es.List(state.EventFilter{Type: state.EventStoryQAPassed})
	if err != nil {
		t.Fatalf("list passed events: %v", err)
	}
	if len(passed) != 1 {
		t.Fatalf("expected 1 STORY_QA_PASSED event, got %d", len(passed))
	}

	story, err := ps.GetStory("s-flaky-pass")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Status != "pr_submitted" {
		t.Errorf("story status = %q, want pr_submitted (flaky pass still proceeds)", story.Status)
	}
}

func TestQA_RealFailureStillFails(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()
	flakyTestStory(t, ps, "s-flaky-real")

	runner := newSequenceRunner(map[string][]mockRunResult{
		"golangci-lint": {{output: "ok"}},
		"go build":      {{output: "ok"}},
		"go test":       {{output: "--- FAIL: TestThing", err: errors.New("exit status 1")}},
	})

	qa := engine.NewQA(engine.QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
		FlakyRetries: 2,
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-flaky-real", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Fatal("expected QA to stay red when every attempt fails")
	}
	if runner.callCount("go test") != 3 {
		t.Fatalf("test step executed %d times, want 3 (initial + 2 retries)", runner.callCount("go test"))
	}
	if len(result.FlakySteps) != 0 {
		t.Fatalf("expected no flaky steps, got %+v", result.FlakySteps)
	}

	flakyEvts, _ := es.List(state.EventFilter{Type: state.EventStoryQAFlaky})
	if len(flakyEvts) != 0 {
		t.Fatalf("expected 0 STORY_QA_FLAKY events for a real failure, got %d", len(flakyEvts))
	}
	failed, err := es.List(state.EventFilter{Type: state.EventStoryQAFailed})
	if err != nil {
		t.Fatalf("list failed events: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 STORY_QA_FAILED event, got %d", len(failed))
	}
}

func TestQA_LintBuildNeverRetried(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()
	flakyTestStory(t, ps, "s-flaky-lint")

	runner := newSequenceRunner(map[string][]mockRunResult{
		"golangci-lint": {{output: "unused variable", err: errors.New("exit status 1")}},
		"go build":      {{output: "undefined: Foo", err: errors.New("exit status 1")}},
		"go test":       {{output: "ok"}},
	})

	qa := engine.NewQA(engine.QAConfig{
		LintCommand:  "golangci-lint run",
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
		FlakyRetries: 3,
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-flaky-lint", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Fatal("expected QA red when lint/build fail")
	}
	if got := runner.callCount("golangci-lint"); got != 1 {
		t.Errorf("lint executed %d times, want 1 (deterministic — never retried)", got)
	}
	if got := runner.callCount("go build"); got != 1 {
		t.Errorf("build executed %d times, want 1 (deterministic — never retried)", got)
	}
	if got := runner.callCount("go test"); got != 1 {
		t.Errorf("test executed %d times, want 1 (passed first try)", got)
	}
	flakyEvts, _ := es.List(state.EventFilter{Type: state.EventStoryQAFlaky})
	if len(flakyEvts) != 0 {
		t.Fatalf("expected 0 STORY_QA_FLAKY events, got %d", len(flakyEvts))
	}
}

func TestQA_FlakyDisabled(t *testing.T) {
	es, ps, cleanup := newTestStores(t)
	defer cleanup()
	flakyTestStory(t, ps, "s-flaky-off")

	runner := newSequenceRunner(map[string][]mockRunResult{
		"go test": {
			{output: "--- FAIL: TestThing", err: errors.New("exit status 1")},
			{output: "ok", err: nil}, // would pass on retry — must NOT be reached
		},
	})

	qa := engine.NewQA(engine.QAConfig{
		TestCommand:  "go test ./...",
		FlakyRetries: 0,
	}, runner, es, ps)

	result, err := qa.Run(context.Background(), "s-flaky-off", "/tmp/worktree")
	if err != nil {
		t.Fatalf("qa run: %v", err)
	}
	if result.Passed {
		t.Fatal("expected QA red with flaky_retries=0 despite a passing retry script")
	}
	if got := runner.callCount("go test"); got != 1 {
		t.Fatalf("test executed %d times, want 1 (retrying disabled)", got)
	}
	flakyEvts, _ := es.List(state.EventFilter{Type: state.EventStoryQAFlaky})
	if len(flakyEvts) != 0 {
		t.Fatalf("expected 0 STORY_QA_FLAKY events, got %d", len(flakyEvts))
	}
}

func TestMetrics_CountsFlakyPassesPerRequirement(t *testing.T) {
	es, _, cleanup := newTestStores(t)
	defer cleanup()

	dir := t.TempDir()
	sqlite, err := state.NewSQLiteStore(filepath.Join(dir, "metrics.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	defer sqlite.Close()

	for _, evt := range []state.Event{
		state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
			"id": "r-flaky-metrics", "title": "Flaky metrics req",
		}),
		state.NewEvent(state.EventStoryCreated, "tech-lead", "s-flaky-metrics", map[string]any{
			"id": "s-flaky-metrics", "req_id": "r-flaky-metrics", "title": "story", "complexity": 2,
		}),
	} {
		if err := sqlite.Project(evt); err != nil {
			t.Fatalf("project setup event: %v", err)
		}
	}

	if err := es.Append(state.NewEvent(state.EventStoryQAPassed, "qa", "s-flaky-metrics", map[string]any{"passed": true})); err != nil {
		t.Fatalf("append qa passed: %v", err)
	}
	if err := es.Append(state.NewEvent(state.EventStoryQAFlaky, "qa", "s-flaky-metrics", map[string]any{"step": "test", "attempts": 2})); err != nil {
		t.Fatalf("append qa flaky: %v", err)
	}

	m, err := engine.ComputeMetrics(es, sqlite, 10, "")
	if err != nil {
		t.Fatalf("compute metrics: %v", err)
	}
	if m.FlakyQAPasses != 1 {
		t.Errorf("FlakyQAPasses = %d, want 1", m.FlakyQAPasses)
	}
	if len(m.RequirementStats) != 1 {
		t.Fatalf("expected 1 requirement stat, got %d", len(m.RequirementStats))
	}
	if m.RequirementStats[0].FlakyQAPasses != 1 {
		t.Errorf("per-requirement FlakyQAPasses = %d, want 1", m.RequirementStats[0].FlakyQAPasses)
	}

	out := engine.FormatMetrics(m)
	if !strings.Contains(out, "Flaky QA passes: 1") {
		t.Errorf("FormatMetrics output missing flaky count:\n%s", out)
	}
}
