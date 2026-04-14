package engine

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func newReportStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
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

func TestReportBuilder_Build_Complete(t *testing.T) {
	es, ps, cleanup := newReportStores(t)
	defer cleanup()

	// Create req
	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Add auth", "description": "Implement authentication", "repo_path": "/tmp/repo",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	// Create story
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "Create login handler", "description": "handler", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Story started + merged
	startEvt := state.NewEvent(state.EventStoryStarted, "agent-1", "r001-s1", map[string]any{
		"tier": 0, "role": "junior",
	})
	es.Append(startEvt)
	ps.Project(startEvt)

	mergeEvt := state.NewEvent(state.EventStoryMerged, "merger", "r001-s1", map[string]any{
		"pr_url": "https://github.com/pr/1", "pr_number": 1,
	})
	es.Append(mergeEvt)
	ps.Project(mergeEvt)

	// Complete requirement
	completeEvt := state.NewEvent(state.EventReqCompleted, "monitor", "", map[string]any{
		"id": "r-001",
	})
	es.Append(completeEvt)
	ps.Project(completeEvt)

	cfg := config.DefaultConfig()
	rb := NewReportBuilder(es, ps, cfg)

	report, err := rb.Build("r-001")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if report.RequirementID != "r-001" {
		t.Errorf("expected req ID r-001, got %s", report.RequirementID)
	}
	if report.Title != "Add auth" {
		t.Errorf("expected title 'Add auth', got %q", report.Title)
	}
	if report.Status != ReportStatusDone {
		t.Errorf("expected DONE status, got %s", report.Status)
	}
	if len(report.Stories) != 1 {
		t.Fatalf("expected 1 story, got %d", len(report.Stories))
	}
	if report.Stories[0].Title != "Create login handler" {
		t.Errorf("expected story title, got %q", report.Stories[0].Title)
	}
	if len(report.Timeline) == 0 {
		t.Error("expected non-empty timeline")
	}
}

func TestReportBuilder_Build_WithRetriesAndEscalation(t *testing.T) {
	es, ps, cleanup := newReportStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Feature", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "Task", "description": "d", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Review failure (retry)
	rfEvt := state.NewEvent(state.EventStoryReviewFailed, "reviewer", "r001-s1", map[string]any{
		"summary": "bad code",
	})
	es.Append(rfEvt)
	ps.Project(rfEvt)

	// Escalation
	escEvt := state.NewEvent(state.EventStoryEscalated, "monitor", "r001-s1", map[string]any{
		"from_tier": 0, "to_tier": 1,
	})
	es.Append(escEvt)
	ps.Project(escEvt)

	// Merge
	mergeEvt := state.NewEvent(state.EventStoryMerged, "merger", "r001-s1", map[string]any{
		"pr_url": "https://github.com/pr/2",
	})
	es.Append(mergeEvt)
	ps.Project(mergeEvt)

	// Complete
	completeEvt := state.NewEvent(state.EventReqCompleted, "monitor", "", map[string]any{
		"id": "r-001",
	})
	es.Append(completeEvt)
	ps.Project(completeEvt)

	cfg := config.DefaultConfig()
	rb := NewReportBuilder(es, ps, cfg)

	report, err := rb.Build("r-001")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if report.Status != ReportStatusDoneWithConcerns {
		t.Errorf("expected DONE_WITH_CONCERNS, got %s", report.Status)
	}
	if report.Stories[0].EscalationCount != 1 {
		t.Errorf("expected 1 escalation, got %d", report.Stories[0].EscalationCount)
	}
	if report.Stories[0].RetryCount != 1 {
		t.Errorf("expected 1 retry, got %d", report.Stories[0].RetryCount)
	}
}

func TestReportBuilder_Build_UnknownReq(t *testing.T) {
	es, ps, cleanup := newReportStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	rb := NewReportBuilder(es, ps, cfg)

	_, err := rb.Build("nonexistent")
	if err == nil {
		t.Error("expected error for unknown requirement")
	}
}

func TestCountRetries_NoFailures(t *testing.T) {
	es, ps, cleanup := newReportStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	rb := NewReportBuilder(es, ps, cfg)

	count, err := rb.countRetries("s-001")
	if err != nil {
		t.Fatalf("countRetries: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestCountRetries_MixedFailures(t *testing.T) {
	es, ps, cleanup := newReportStores(t)
	defer cleanup()

	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", map[string]any{
		"summary": "fail",
	}))
	es.Append(state.NewEvent(state.EventStoryQAFailed, "qa", "s-001", map[string]any{
		"failed_checks": []string{"test"},
	}))
	es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-001", map[string]any{
		"summary": "fail again",
	}))

	cfg := config.DefaultConfig()
	rb := NewReportBuilder(es, ps, cfg)

	count, err := rb.countRetries("s-001")
	if err != nil {
		t.Fatalf("countRetries: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}
