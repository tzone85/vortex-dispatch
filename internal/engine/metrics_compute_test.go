package engine

import (
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// newMetricsStores creates FileStore + SQLiteStore suitable for metrics tests.
func newMetricsStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
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

func TestComputeMetrics_NoRequirements(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	m, err := ComputeMetrics(es, ps, 0, "")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}
	if m.TotalRequirements != 0 {
		t.Errorf("expected 0 requirements, got %d", m.TotalRequirements)
	}
	if m.TotalStories != 0 {
		t.Errorf("expected 0 stories, got %d", m.TotalStories)
	}
}

func TestComputeMetrics_SingleRequirementWithStories(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	// Create a requirement
	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id":          "r-001",
		"title":       "Add login",
		"description": "Implement login flow",
		"repo_path":   "/tmp/repo",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	// Create stories
	s1Evt := state.NewEvent(state.EventStoryCreated, "tech-lead", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "Create model", "description": "desc", "complexity": 2,
	})
	es.Append(s1Evt)
	ps.Project(s1Evt)

	s2Evt := state.NewEvent(state.EventStoryCreated, "tech-lead", "r001-s2", map[string]any{
		"id": "r001-s2", "req_id": "r-001", "title": "Create handler", "description": "desc", "complexity": 3,
	})
	es.Append(s2Evt)
	ps.Project(s2Evt)

	// Mark s1 as merged (no review/QA failures = first pass)
	mergeEvt := state.NewEvent(state.EventStoryMerged, "merger", "r001-s1", map[string]any{
		"pr_url": "https://github.com/test/pr/1",
	})
	es.Append(mergeEvt)
	ps.Project(mergeEvt)

	m, err := ComputeMetrics(es, ps, 0, "")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}

	if m.TotalRequirements != 1 {
		t.Errorf("expected 1 requirement, got %d", m.TotalRequirements)
	}
	if m.TotalStories != 2 {
		t.Errorf("expected 2 stories, got %d", m.TotalStories)
	}
	if m.StoriesMerged != 1 {
		t.Errorf("expected 1 merged story, got %d", m.StoriesMerged)
	}
	if m.StoriesPassed != 1 {
		t.Errorf("expected 1 first-pass story, got %d", m.StoriesPassed)
	}
}

func TestComputeMetrics_WithLimit(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	// Create two requirements
	for _, id := range []string{"r-001", "r-002"} {
		evt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
			"id": id, "title": "Req " + id, "description": "desc", "repo_path": "/tmp",
		})
		es.Append(evt)
		ps.Project(evt)
	}

	m, err := ComputeMetrics(es, ps, 1, "")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}

	if m.TotalRequirements != 1 {
		t.Errorf("expected 1 requirement with limit=1, got %d", m.TotalRequirements)
	}
}

func TestComputeMetrics_EscalationTracking(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Escalate to tier 1
	escEvt := state.NewEvent(state.EventStoryEscalated, "monitor", "r001-s1", map[string]any{
		"from_tier": 0, "to_tier": 1,
	})
	es.Append(escEvt)
	ps.Project(escEvt)

	m, err := ComputeMetrics(es, ps, 0, "")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}

	if m.StoriesEscalated != 1 {
		t.Errorf("expected 1 escalated story, got %d", m.StoriesEscalated)
	}
	if m.EscalationsPerTier[1] != 1 {
		t.Errorf("expected 1 escalation to tier 1, got %d", m.EscalationsPerTier[1])
	}
}

func TestComputeMetrics_CompletedRequirement(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	completeEvt := state.NewEvent(state.EventReqCompleted, "monitor", "", map[string]any{
		"id": "r-001",
	})
	es.Append(completeEvt)
	ps.Project(completeEvt)

	m, err := ComputeMetrics(es, ps, 0, "")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}

	if m.CompletedRequirements != 1 {
		t.Errorf("expected 1 completed requirement, got %d", m.CompletedRequirements)
	}
}

func TestComputeMetrics_FirstPassRate(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	// Story 1: merged, no failures (first pass)
	s1 := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "S1", "description": "d", "complexity": 1,
	})
	es.Append(s1)
	ps.Project(s1)
	m1 := state.NewEvent(state.EventStoryMerged, "merger", "r001-s1", map[string]any{"pr_url": "u"})
	es.Append(m1)
	ps.Project(m1)

	// Story 2: merged but had a review failure (not first pass)
	s2 := state.NewEvent(state.EventStoryCreated, "tl", "r001-s2", map[string]any{
		"id": "r001-s2", "req_id": "r-001", "title": "S2", "description": "d", "complexity": 1,
	})
	es.Append(s2)
	ps.Project(s2)
	rf := state.NewEvent(state.EventStoryReviewFailed, "reviewer", "r001-s2", map[string]any{"reason": "bad"})
	es.Append(rf)
	ps.Project(rf)
	m2 := state.NewEvent(state.EventStoryMerged, "merger", "r001-s2", map[string]any{"pr_url": "u"})
	es.Append(m2)
	ps.Project(m2)

	m, err := ComputeMetrics(es, ps, 0, "")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}

	if m.TotalStories != 2 {
		t.Errorf("expected 2 stories, got %d", m.TotalStories)
	}
	if m.StoriesPassed != 1 {
		t.Errorf("expected 1 first-pass story, got %d", m.StoriesPassed)
	}
	if m.FirstPassRate != 50.0 {
		t.Errorf("expected 50%% first pass rate, got %.1f%%", m.FirstPassRate)
	}
}

func TestComputeMetrics_TraceAnalysis(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "Req", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "S1", "description": "d", "complexity": 1,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Non-existent log dir should gracefully skip trace analysis
	m, err := ComputeMetrics(es, ps, 0, "/nonexistent/logs")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}

	// Should have zero trace data (no log files found)
	if m.TotalToolCalls != 0 {
		t.Errorf("expected 0 tool calls with missing logs, got %d", m.TotalToolCalls)
	}
}

func TestComputeMetrics_PerRequirementStats(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-001", "title": "My Feature", "description": "desc", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "r001-s1", map[string]any{
		"id": "r001-s1", "req_id": "r-001", "title": "S1", "description": "d", "complexity": 1,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	m, err := ComputeMetrics(es, ps, 0, "")
	if err != nil {
		t.Fatalf("ComputeMetrics: %v", err)
	}

	if len(m.RequirementStats) != 1 {
		t.Fatalf("expected 1 requirement stat, got %d", len(m.RequirementStats))
	}

	rs := m.RequirementStats[0]
	if rs.ReqID != "r-001" {
		t.Errorf("expected req ID r-001, got %s", rs.ReqID)
	}
	if rs.Title != "My Feature" {
		t.Errorf("expected title 'My Feature', got %q", rs.Title)
	}
	if rs.StoryCount != 1 {
		t.Errorf("expected 1 story count, got %d", rs.StoryCount)
	}
}

func TestComputeMetrics_TracksSLABreaches(t *testing.T) {
	es, ps, cleanup := newMetricsStores(t)
	defer cleanup()

	// Create 1 requirement with 3 stories, 2 of which breach SLA
	reqEvt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id": "req-001", "title": "Test", "description": "x", "repo_path": "/tmp",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyIDs := []string{"s-001", "s-002", "s-003"}
	for i, sid := range storyIDs {
		created := state.NewEvent(state.EventStoryCreated, "system", sid, map[string]any{
			"id": sid, "req_id": "req-001", "title": "x",
			"description": "x", "complexity": 3,
		})
		es.Append(created)
		ps.Project(created)

		// First two stories breach SLA
		if i < 2 {
			breach := state.NewEvent(state.EventStorySLABreached, "agent", sid, map[string]any{
				"complexity": 3, "elapsed_seconds": 18000, "max_minutes": 240,
			})
			es.Append(breach)
		}
	}

	m, err := ComputeMetrics(es, ps, 10, "")
	if err != nil {
		t.Fatal(err)
	}

	if m.SLABreaches != 2 {
		t.Errorf("SLABreaches = %d, want 2", m.SLABreaches)
	}
	if m.TotalStories != 3 {
		t.Errorf("TotalStories = %d, want 3", m.TotalStories)
	}
	wantRate := 2.0 / 3.0
	if m.SLABreachRate < wantRate-0.01 || m.SLABreachRate > wantRate+0.01 {
		t.Errorf("SLABreachRate = %f, want ~%f", m.SLABreachRate, wantRate)
	}

	// Verify per-requirement breakdown
	if len(m.RequirementStats) != 1 {
		t.Fatalf("expected 1 RequirementStat, got %d", len(m.RequirementStats))
	}
	if m.RequirementStats[0].SLABreaches != 2 {
		t.Errorf("RequirementStat.SLABreaches = %d, want 2", m.RequirementStats[0].SLABreaches)
	}
}
