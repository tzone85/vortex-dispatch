package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ctxCapturingClient records whether the context it received was already
// cancelled at the moment Complete was invoked, then signals via done.
type ctxCapturingClient struct {
	done           chan struct{}
	ctxErrAtInvoke error
}

func (c *ctxCapturingClient) Complete(ctx context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	c.ctxErrAtInvoke = ctx.Err()
	close(c.done)
	return llm.CompletionResponse{Content: "fix description", Model: "test"}, nil
}

// TestDispatchIntegrationFix_DetachesFromCancelledParent verifies that the
// fire-and-forget fixer goroutine does not inherit cancellation from the
// caller's context. postExecutionPipeline cancels its pipelineCtx immediately
// after dispatch, so a fixer derived from it would never run.
func TestDispatchIntegrationFix_DetachesFromCancelledParent(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer es.Close()
	ps, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer ps.Close()

	// Seed the trigger story so the fixer's lookups succeed.
	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-FIX-1", map[string]any{
		"id":     "STR-FIX-1",
		"req_id": "REQ-FIX",
		"title":  "Trigger story",
	})
	if err := es.Append(storyEvt); err != nil {
		t.Fatal(err)
	}
	if err := ps.Project(storyEvt); err != nil {
		t.Fatal(err)
	}

	client := &ctxCapturingClient{done: make(chan struct{})}
	fixer := NewTechLeadFixer(client, "test-model", 1024, es, ps)

	// Pass an already-cancelled parent context — mimicking the pipelineCtx
	// being cancelled the instant the pipeline returns.
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	fixer.DispatchIntegrationFix(parentCtx, "STR-FIX-1", t.TempDir(), "build failed: undefined symbol")

	select {
	case <-client.done:
		// Good — the LLM was invoked despite the cancelled parent.
	case <-time.After(5 * time.Second):
		t.Fatal("LLM Complete was never called — fixer did not detach from cancelled parent context")
	}

	if client.ctxErrAtInvoke != nil {
		t.Errorf("fixer context was already cancelled at LLM invocation: %v (expected detached, non-cancelled context)", client.ctxErrAtInvoke)
	}
}
