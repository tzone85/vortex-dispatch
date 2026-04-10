package engine_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// setupReportStores creates a pair of in-memory/file stores pre-populated with
// a requirement, two stories, and a set of representative domain events.
// It returns the stores and a cleanup func. The requirement ID is "req-001".
func setupReportStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
	t.Helper()

	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}

	cleanup := func() {
		es.Close()
		ps.Close()
	}

	reqID := "req-001"

	// Emit and project REQ_SUBMITTED
	reqEvt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          reqID,
		"title":       "Add user authentication",
		"description": "Implement OAuth2 login with Google and GitHub",
		"repo_path":   "/tmp/test-repo",
	})
	if err := es.Append(reqEvt); err != nil {
		t.Fatalf("append req submitted: %v", err)
	}
	if err := ps.Project(reqEvt); err != nil {
		t.Fatalf("project req submitted: %v", err)
	}

	// Story 1: merged successfully
	s1ID := "s-001"
	story1Evt := state.NewEvent(state.EventStoryCreated, "tech-lead", s1ID, map[string]any{
		"id":          s1ID,
		"req_id":      reqID,
		"title":       "Setup OAuth middleware",
		"description": "Create middleware",
		"complexity":  3,
	})
	if err := es.Append(story1Evt); err != nil {
		t.Fatalf("append story1 created: %v", err)
	}
	if err := ps.Project(story1Evt); err != nil {
		t.Fatalf("project story1 created: %v", err)
	}

	// Story 1: assigned, started, PR created, merged
	for _, evtType := range []state.EventType{
		state.EventStoryAssigned,
		state.EventStoryStarted,
	} {
		evt := state.NewEvent(evtType, "agent-1", s1ID, map[string]any{
			"agent_id": "agent-1",
			"wave":     1,
		})
		es.Append(evt)
		ps.Project(evt)
	}

	prEvt := state.NewEvent(state.EventStoryPRCreated, "agent-1", s1ID, map[string]any{
		"pr_number": 42,
		"pr_url":    "https://github.com/org/repo/pull/42",
	})
	es.Append(prEvt)
	ps.Project(prEvt)

	mergedEvt := state.NewEvent(state.EventStoryMerged, "merger", s1ID, nil)
	// Set a fixed merged time slightly after creation
	mergedEvt.Timestamp = story1Evt.Timestamp.Add(2 * time.Hour)
	es.Append(mergedEvt)
	ps.Project(mergedEvt)

	// Story 2: escalated, then merged
	s2ID := "s-002"
	story2Evt := state.NewEvent(state.EventStoryCreated, "tech-lead", s2ID, map[string]any{
		"id":          s2ID,
		"req_id":      reqID,
		"title":       "Add Google provider",
		"description": "Integrate Google OAuth",
		"complexity":  5,
	})
	es.Append(story2Evt)
	ps.Project(story2Evt)

	// Story 2: one escalation
	escalEvt := state.NewEvent(state.EventStoryEscalated, "agent-2", s2ID, map[string]any{
		"from_tier": 0,
		"to_tier":   1,
		"reason":    "implementation too complex",
	})
	es.Append(escalEvt)
	ps.Project(escalEvt)

	// Story 2: one QA failure then pass
	qaFailEvt := state.NewEvent(state.EventStoryQAFailed, "qa-agent", s2ID, map[string]any{
		"reason": "tests missing",
	})
	es.Append(qaFailEvt)
	ps.Project(qaFailEvt)

	qaPassEvt := state.NewEvent(state.EventStoryQAPassed, "qa-agent", s2ID, nil)
	es.Append(qaPassEvt)
	ps.Project(qaPassEvt)

	pr2Evt := state.NewEvent(state.EventStoryPRCreated, "agent-2", s2ID, map[string]any{
		"pr_number": 43,
		"pr_url":    "https://github.com/org/repo/pull/43",
	})
	es.Append(pr2Evt)
	ps.Project(pr2Evt)

	merged2Evt := state.NewEvent(state.EventStoryMerged, "merger", s2ID, nil)
	merged2Evt.Timestamp = story2Evt.Timestamp.Add(4 * time.Hour)
	es.Append(merged2Evt)
	ps.Project(merged2Evt)

	// Emit REQ_COMPLETED
	reqCompEvt := state.NewEvent(state.EventReqCompleted, "system", "", map[string]any{
		"id": reqID,
	})
	es.Append(reqCompEvt)
	ps.Project(reqCompEvt)

	return es, ps, cleanup
}

func TestReportBuilder_Build_BasicFields(t *testing.T) {
	es, ps, cleanup := setupReportStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	rb := engine.NewReportBuilder(es, ps, cfg)

	report, err := rb.Build("req-001")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if report.RequirementID != "req-001" {
		t.Errorf("RequirementID: want req-001, got %s", report.RequirementID)
	}
	if report.Title != "Add user authentication" {
		t.Errorf("Title: want 'Add user authentication', got %q", report.Title)
	}
	if report.ReqStatus != "completed" {
		t.Errorf("ReqStatus: want completed, got %s", report.ReqStatus)
	}
	if len(report.Stories) != 2 {
		t.Fatalf("Stories: want 2, got %d", len(report.Stories))
	}
}

func TestReportBuilder_Build_StoryDetails(t *testing.T) {
	es, ps, cleanup := setupReportStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	rb := engine.NewReportBuilder(es, ps, cfg)

	report, err := rb.Build("req-001")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Find story 1 (s-001)
	var s1, s2 *engine.ReportStory
	for i := range report.Stories {
		switch report.Stories[i].ID {
		case "s-001":
			s1 = &report.Stories[i]
		case "s-002":
			s2 = &report.Stories[i]
		}
	}
	if s1 == nil {
		t.Fatal("story s-001 not found in report")
	}
	if s2 == nil {
		t.Fatal("story s-002 not found in report")
	}

	if s1.Title != "Setup OAuth middleware" {
		t.Errorf("s1.Title: want 'Setup OAuth middleware', got %q", s1.Title)
	}
	if s1.PRUrl != "https://github.com/org/repo/pull/42" {
		t.Errorf("s1.PRUrl: want pr url, got %q", s1.PRUrl)
	}
	if s1.EscalationCount != 0 {
		t.Errorf("s1.EscalationCount: want 0, got %d", s1.EscalationCount)
	}
	if s1.RetryCount != 0 {
		t.Errorf("s1.RetryCount: want 0, got %d", s1.RetryCount)
	}

	// Story 2 should have 1 escalation and 1 QA failure (retry)
	if s2.EscalationCount != 1 {
		t.Errorf("s2.EscalationCount: want 1, got %d", s2.EscalationCount)
	}
	if s2.RetryCount != 1 {
		t.Errorf("s2.RetryCount: want 1, got %d", s2.RetryCount)
	}
}

func TestReportBuilder_Build_EffortAndCost(t *testing.T) {
	es, ps, cleanup := setupReportStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	rb := engine.NewReportBuilder(es, ps, cfg)

	report, err := rb.Build("req-001")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// With complexities 3 and 5 and default config, hours should be > 0
	if report.Effort.Summary.HoursLow <= 0 {
		t.Errorf("Effort.Summary.HoursLow should be > 0, got %f", report.Effort.Summary.HoursLow)
	}
	if report.Effort.Summary.QuoteLow <= 0 {
		t.Errorf("Effort.Summary.QuoteLow should be > 0, got %f", report.Effort.Summary.QuoteLow)
	}
	if report.Effort.Summary.StoryCount != 2 {
		t.Errorf("Effort.Summary.StoryCount: want 2, got %d", report.Effort.Summary.StoryCount)
	}
}

func TestReportBuilder_Build_StatusClassification(t *testing.T) {
	es, ps, cleanup := setupReportStores(t)
	defer cleanup()

	cfg := config.DefaultConfig()
	rb := engine.NewReportBuilder(es, ps, cfg)

	report, err := rb.Build("req-001")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Both stories are merged and req is completed — status should be DONE or DONE_WITH_CONCERNS
	// Story 2 had escalations/retries, so DONE_WITH_CONCERNS
	validStatuses := map[engine.ReportStatus]bool{
		engine.ReportStatusDone:             true,
		engine.ReportStatusDoneWithConcerns: true,
	}
	if !validStatuses[report.Status] {
		t.Errorf("Status: want DONE or DONE_WITH_CONCERNS, got %s", report.Status)
	}
}

func TestReportBuilder_Build_NotFound(t *testing.T) {
	dir := t.TempDir()
	es, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	ps, _ := state.NewSQLiteStore(":memory:")
	defer es.Close()
	defer ps.Close()

	cfg := config.DefaultConfig()
	rb := engine.NewReportBuilder(es, ps, cfg)

	_, err := rb.Build("nonexistent-req")
	if err == nil {
		t.Fatal("expected error for nonexistent requirement, got nil")
	}
}
