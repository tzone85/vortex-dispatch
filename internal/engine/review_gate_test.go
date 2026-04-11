package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// newTestStore creates a FileStore in the test's temp directory and registers
// cleanup so the store is always closed.
func newTestStore(t *testing.T) state.EventStore {
	t.Helper()
	es, err := state.NewFileStore(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(func() { _ = es.Close() })
	return es
}

// --- ResolveMode tests ---

func TestReviewGate_ResolveMode_FromEvent(t *testing.T) {
	es := newTestStore(t)

	if err := es.Append(state.NewEvent(state.EventReviewModeSet, "system", "", map[string]any{
		"req_id": "r-001", "mode": "manual",
	})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	gate := engine.NewReviewGate(es)
	cfg := config.DefaultConfig()
	mode := gate.ResolveMode("r-001", cfg.Merge)
	if mode != "manual" {
		t.Fatalf("expected manual from event, got %s", mode)
	}
}

func TestReviewGate_ResolveMode_FromConfig(t *testing.T) {
	es := newTestStore(t)

	gate := engine.NewReviewGate(es)
	cfg := config.DefaultConfig()
	cfg.Merge.ReviewMode = "plan_only"
	mode := gate.ResolveMode("r-001", cfg.Merge)
	if mode != "plan_only" {
		t.Fatalf("expected plan_only from config, got %s", mode)
	}
}

func TestReviewGate_ResolveMode_FallbackAutoMergeFalse(t *testing.T) {
	es := newTestStore(t)

	gate := engine.NewReviewGate(es)
	cfg := config.DefaultConfig()
	cfg.Merge.ReviewMode = ""
	cfg.Merge.AutoMerge = false
	mode := gate.ResolveMode("r-001", cfg.Merge)
	if mode != "manual" {
		t.Fatalf("expected manual from fallback, got %s", mode)
	}
}

func TestReviewGate_ResolveMode_FallbackAutoMergeTrue(t *testing.T) {
	es := newTestStore(t)

	gate := engine.NewReviewGate(es)
	cfg := config.DefaultConfig()
	cfg.Merge.ReviewMode = ""
	cfg.Merge.AutoMerge = true
	mode := gate.ResolveMode("r-001", cfg.Merge)
	if mode != "auto" {
		t.Fatalf("expected auto from fallback, got %s", mode)
	}
}

// Event-set mode takes precedence over a non-empty cfg.ReviewMode.
func TestReviewGate_ResolveMode_EventOverridesConfig(t *testing.T) {
	es := newTestStore(t)

	if err := es.Append(state.NewEvent(state.EventReviewModeSet, "system", "", map[string]any{
		"req_id": "r-001", "mode": "auto",
	})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	gate := engine.NewReviewGate(es)
	cfg := config.DefaultConfig()
	cfg.Merge.ReviewMode = "plan_only"
	mode := gate.ResolveMode("r-001", cfg.Merge)
	if mode != "auto" {
		t.Fatalf("expected auto (from event override), got %s", mode)
	}
}

// Only events for the matching req_id should be used.
func TestReviewGate_ResolveMode_EventWrongReqIgnored(t *testing.T) {
	es := newTestStore(t)

	if err := es.Append(state.NewEvent(state.EventReviewModeSet, "system", "", map[string]any{
		"req_id": "r-other", "mode": "auto",
	})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	gate := engine.NewReviewGate(es)
	cfg := config.DefaultConfig()
	cfg.Merge.ReviewMode = "plan_only"
	mode := gate.ResolveMode("r-001", cfg.Merge)
	if mode != "plan_only" {
		t.Fatalf("expected plan_only (config fallback), got %s", mode)
	}
}

// --- PlanApproved tests ---

func TestReviewGate_PlanApproved_BeforeEvent(t *testing.T) {
	es := newTestStore(t)
	gate := engine.NewReviewGate(es)

	if gate.PlanApproved("r-001") {
		t.Fatal("expected not approved before event")
	}
}

func TestReviewGate_PlanApproved_AfterEvent(t *testing.T) {
	es := newTestStore(t)
	gate := engine.NewReviewGate(es)

	if err := es.Append(state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "r-001",
	})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if !gate.PlanApproved("r-001") {
		t.Fatal("expected approved after event")
	}
}

func TestReviewGate_PlanApproved_DifferentReq(t *testing.T) {
	es := newTestStore(t)
	gate := engine.NewReviewGate(es)

	if err := es.Append(state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "r-001",
	})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if gate.PlanApproved("r-002") {
		t.Fatal("r-001 approval should not apply to r-002")
	}
}

// --- StoryApproved tests ---

func TestReviewGate_StoryApproved_BeforeEvent(t *testing.T) {
	es := newTestStore(t)
	gate := engine.NewReviewGate(es)

	if gate.StoryApproved("s-001") {
		t.Fatal("expected not approved before event")
	}
}

func TestReviewGate_StoryApproved_AfterEvent(t *testing.T) {
	es := newTestStore(t)
	gate := engine.NewReviewGate(es)

	if err := es.Append(state.NewEvent(state.EventStoryApproved, "human", "s-001", map[string]any{
		"story_id": "s-001",
	})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if !gate.StoryApproved("s-001") {
		t.Fatal("expected approved after event")
	}
}

func TestReviewGate_StoryApproved_DifferentStory(t *testing.T) {
	es := newTestStore(t)
	gate := engine.NewReviewGate(es)

	if err := es.Append(state.NewEvent(state.EventStoryApproved, "human", "s-001", map[string]any{
		"story_id": "s-001",
	})); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if gate.StoryApproved("s-002") {
		t.Fatal("s-001 approval should not apply to s-002")
	}
}
