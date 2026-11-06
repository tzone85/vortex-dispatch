package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ctxHonoringClient returns ctx.Err() if the context is already cancelled by
// the time Complete is called, otherwise a canned response. This lets a test
// distinguish "the fix ran on a live context" from "the fix ran on the
// caller's already-cancelled context".
type ctxHonoringClient struct{}

func (ctxHonoringClient) Complete(ctx context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return llm.CompletionResponse{}, err
	}
	return llm.CompletionResponse{Content: "reconcile handler.Handler signature"}, nil
}

// lockedEventStore is a minimal thread-safe EventStore recorder for the
// goroutine-based dispatch test.
type lockedEventStore struct {
	mu     sync.Mutex
	events []state.Event
}

func (s *lockedEventStore) Append(e state.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}
func (s *lockedEventStore) List(state.EventFilter) ([]state.Event, error) { return nil, nil }
func (s *lockedEventStore) Count(state.EventFilter) (int, error)          { return 0, nil }
func (s *lockedEventStore) Close() error                                  { return nil }
func (s *lockedEventStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}
func (s *lockedEventStore) first() state.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events[0]
}

// TestRunIntegrationFix_CompletesAndAppendsEvent verifies the fix work path
// runs to completion (LLM call succeeds, STORY_INTEGRATION_FAILED persisted).
func TestRunIntegrationFix_CompletesAndAppendsEvent(t *testing.T) {
	es := &lockedEventStore{}
	f := NewTechLeadFixer(ctxHonoringClient{}, "test-model", 500, es, &MockProjectionStore{})

	f.runIntegrationFix("test-req-s-002", t.TempDir(), "main.go:1:1: undefined: X")

	if es.count() != 1 {
		t.Fatalf("expected 1 appended event, got %d", es.count())
	}
	evt := es.first()
	if evt.Type != state.EventStoryIntegrationFailed {
		t.Fatalf("event type = %v, want STORY_INTEGRATION_FAILED", evt.Type)
	}
	if !strings.Contains(string(evt.Payload), "reconcile handler.Handler signature") {
		t.Errorf("event payload missing LLM fix hint; got %s", evt.Payload)
	}
}

// TestDispatchIntegrationFix_RunsDespiteCancelledCallerContext is the
// regression pin for the context-lifetime bug: DispatchIntegrationFix is called
// from postExecutionPipeline with a pipelineCtx that the caller's deferred
// cancel() tears down the instant the function returns. If the detached fix
// goroutine derives its LLM-call context from that ctx (the original bug), the
// call is cancelled before it runs and no fix hint is ever produced. The
// goroutine must root a fresh context so the fix still runs.
func TestDispatchIntegrationFix_RunsDespiteCancelledCallerContext(t *testing.T) {
	es := &lockedEventStore{}
	f := NewTechLeadFixer(ctxHonoringClient{}, "test-model", 500, es, &MockProjectionStore{})

	// Simulate the caller: a context that is already cancelled by the time the
	// detached goroutine runs (postExecutionPipeline's deferred cancel()).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f.DispatchIntegrationFix(ctx, "test-req-s-002", t.TempDir(), "main.go:1:1: undefined: X")

	// Poll for the appended event (goroutine is async). Generous bound.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if es.count() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if es.count() != 1 {
		t.Fatal("integration fix did not complete on a cancelled caller context: no event appended (LLM call was cancelled before running)")
	}
}

// TestTechLeadFixer_BuildPrompt_ContainsRequiredSections verifies that the
// prompt produced by buildPrompt contains the build error, a stories section,
// and instruction to produce a fix story.
func TestTechLeadFixer_BuildPrompt_ContainsRequiredSections(t *testing.T) {
	fixer := &TechLeadFixer{model: "claude-opus-4-8"}

	stories := []state.Story{
		{ID: "abc12345-s-001", Title: "Add HTTP handler"},
		{ID: "abc12345-s-002", Title: "Wire handler to server"},
	}
	buildErr := "cmd/server/main.go:12:15: undefined: handler.Handler"

	prompt := fixer.buildPrompt("abc12345-s-002", buildErr, stories)

	// Must contain the build error.
	if !strings.Contains(prompt, buildErr) {
		t.Errorf("prompt missing build error\ngot:\n%s", prompt)
	}
	// Must list recently merged stories.
	if !strings.Contains(prompt, "Add HTTP handler") {
		t.Errorf("prompt missing story title 'Add HTTP handler'")
	}
	if !strings.Contains(prompt, "Wire handler to server") {
		t.Errorf("prompt missing story title 'Wire handler to server'")
	}
	// Must ask for a fix story.
	if !strings.Contains(prompt, "fix") && !strings.Contains(prompt, "reconcil") {
		t.Errorf("prompt does not ask for fix/reconciliation")
	}
}

// TestTechLeadFixer_BuildPrompt_EmptyStories verifies that buildPrompt handles
// an empty stories slice without panicking.
func TestTechLeadFixer_BuildPrompt_EmptyStories(t *testing.T) {
	fixer := &TechLeadFixer{model: "claude-opus-4-8"}
	prompt := fixer.buildPrompt("story-001", "build failed", nil)
	if len(prompt) == 0 {
		t.Error("expected non-empty prompt even with no stories")
	}
}
