package web

import (
	"encoding/json"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestMapStatusToBucket(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"draft", "planned"},
		{"estimated", "planned"},
		{"planned", "planned"},
		{"assigned", "planned"},
		{"in_progress", "in_progress"},
		{"review", "review"},
		{"qa", "qa"},
		{"qa_started", "qa"},
		{"qa_failed", "qa"},
		{"pr_submitted", "pr_submitted"},
		{"merged", "merged"},
		{"split", "split"},
		{"unknown_status", "planned"},
		{"", "planned"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := mapStatusToBucket(tt.status)
			if got != tt.want {
				t.Errorf("mapStatusToBucket(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestBuildSnapshot_Empty(t *testing.T) {
	s := newTestServer(t)

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if len(snap.Stories) != 0 {
		t.Errorf("expected 0 stories, got %d", len(snap.Stories))
	}
	if len(snap.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(snap.Events))
	}
	if snap.Pipeline.Planned != 0 {
		t.Errorf("expected 0 planned, got %d", snap.Pipeline.Planned)
	}
	if snap.DAG != nil {
		t.Errorf("expected nil DAG, got %v", snap.DAG)
	}
}

func TestBuildSnapshot_WithRequirementAndStories(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)

	// Seed two stories — both start in "draft" (maps to "planned")
	seedStory(t, s, reqID)
	seedStoryWithID(t, s, reqID, "story-002")

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if len(snap.Requirements) != 1 {
		t.Errorf("expected 1 requirement, got %d", len(snap.Requirements))
	}
	if len(snap.Stories) != 2 {
		t.Errorf("expected 2 stories, got %d", len(snap.Stories))
	}
	// Both stories are in "draft" status, which maps to "planned"
	if snap.Pipeline.Planned != 2 {
		t.Errorf("expected 2 planned, got %d", snap.Pipeline.Planned)
	}
}

func TestBuildSnapshot_PipelineCounts(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	// Transition story to "in_progress" via event
	startEvt := state.NewEvent(state.EventStoryStarted, "agent-1", storyID, map[string]any{
		"id":   storyID,
		"tier": 0,
		"role": "junior",
	})
	if err := s.eventStore.Append(startEvt); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.projStore.Project(startEvt); err != nil {
		t.Fatalf("project: %v", err)
	}

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if snap.Pipeline.InProgress != 1 {
		t.Errorf("expected 1 in_progress, got %d", snap.Pipeline.InProgress)
	}
}

func TestBuildSnapshot_AllPipelineBuckets(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)

	// Create stories and transition them to different statuses
	storyReview := seedStoryWithID(t, s, reqID, "s-review")
	storyQA := seedStoryWithID(t, s, reqID, "s-qa")
	storyPR := seedStoryWithID(t, s, reqID, "s-pr")
	storyMerged := seedStoryWithID(t, s, reqID, "s-merged")
	storySplit := seedStoryWithID(t, s, reqID, "s-split")

	// Transition to review
	emitAndProject(t, s, state.EventStoryCompleted, "agent-1", storyReview, nil)

	// Transition to qa
	emitAndProject(t, s, state.EventStoryReviewPassed, "agent-1", storyQA, nil)

	// Transition to pr_submitted
	emitAndProject(t, s, state.EventStoryQAPassed, "agent-1", storyPR, nil)

	// Transition to merged
	emitAndProject(t, s, state.EventStoryMerged, "agent-1", storyMerged, nil)

	// Transition to split
	emitAndProject(t, s, state.EventStorySplit, "agent-1", storySplit, nil)

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if snap.Pipeline.Review != 1 {
		t.Errorf("expected 1 review, got %d", snap.Pipeline.Review)
	}
	if snap.Pipeline.QA != 1 {
		t.Errorf("expected 1 qa, got %d", snap.Pipeline.QA)
	}
	if snap.Pipeline.PR != 1 {
		t.Errorf("expected 1 pr, got %d", snap.Pipeline.PR)
	}
	if snap.Pipeline.Merged != 1 {
		t.Errorf("expected 1 merged, got %d", snap.Pipeline.Merged)
	}
	if snap.Pipeline.Split != 1 {
		t.Errorf("expected 1 split, got %d", snap.Pipeline.Split)
	}
}

func TestBuildSnapshot_IncludesEvents(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	seedStory(t, s, reqID)

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	// We seeded 2 events (req + story)
	if len(snap.Events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(snap.Events))
	}

	// Verify event summary fields
	found := false
	for _, evt := range snap.Events {
		if evt.Type == string(state.EventReqSubmitted) {
			found = true
			if evt.Timestamp == "" {
				t.Error("event timestamp should not be empty")
			}
		}
	}
	if !found {
		t.Error("expected to find EventReqSubmitted in events")
	}
}

func TestBuildSnapshot_IncludesProjectedAgents(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)
	agentID := seedAgent(t, s, "test-session")
	emitAndProject(t, s, state.EventStoryAssigned, agentID, storyID, map[string]any{
		"agent_id": agentID,
		"wave":     1,
	})

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if len(snap.Agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(snap.Agents))
	}
	if snap.Agents[0].ID != agentID {
		t.Fatalf("agent id = %q, want %q", snap.Agents[0].ID, agentID)
	}
}

func TestBuildSnapshot_IncludesDAG(t *testing.T) {
	s := newTestServer(t)

	dag := &graph.DAGExport{
		Nodes: []graph.NodeExport{{ID: "s1", Wave: 0}},
		Edges: []graph.EdgeExport{{From: "s1", To: "s2"}},
		Waves: [][]string{{"s1"}, {"s2"}},
	}
	s.SetDAG(dag)

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if snap.DAG == nil {
		t.Fatal("expected DAG to be set")
	}
	if len(snap.DAG.Nodes) != 1 {
		t.Errorf("expected 1 DAG node, got %d", len(snap.DAG.Nodes))
	}
}

func TestSnapshotJSON_ReturnsValidJSON(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	seedStory(t, s, reqID)

	data, err := s.SnapshotJSON()
	if err != nil {
		t.Fatalf("SnapshotJSON: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}

	var snap StateSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(snap.Requirements) != 1 {
		t.Errorf("expected 1 requirement in JSON, got %d", len(snap.Requirements))
	}
}

func TestSnapshotJSON_EmptyState(t *testing.T) {
	s := newTestServer(t)

	data, err := s.SnapshotJSON()
	if err != nil {
		t.Fatalf("SnapshotJSON: %v", err)
	}

	var snap StateSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if snap.DAG != nil {
		t.Error("expected nil DAG in empty snapshot")
	}
}

func TestSetDAG(t *testing.T) {
	s := newTestServer(t)

	if s.dagExport != nil {
		t.Fatal("expected nil dagExport initially")
	}

	dag := &graph.DAGExport{
		Nodes: []graph.NodeExport{{ID: "story-1", Wave: 0}},
	}
	s.SetDAG(dag)

	if s.dagExport == nil {
		t.Fatal("expected dagExport to be set after SetDAG")
	}
	if s.dagExport.Nodes[0].ID != "story-1" {
		t.Errorf("expected node ID=story-1, got %q", s.dagExport.Nodes[0].ID)
	}
}

func TestBuildSnapshot_EscalationsReturned(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	// Emit an escalation event
	emitAndProject(t, s, state.EventStoryEscalated, "dashboard", storyID, map[string]any{
		"from_tier": 0,
		"to_tier":   1,
		"reason":    "test escalation",
	})

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	// Escalations should include the one we created
	if len(snap.Escalations) != 1 {
		t.Errorf("expected 1 escalation, got %d", len(snap.Escalations))
	}
}

func TestBuildSnapshot_ScopesStoriesEscalationsAndEvents(t *testing.T) {
	s := newTestServer(t)
	s.reqFilter = state.ReqFilter{RepoPath: "/repo/alpha"}

	alphaReq := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "req-alpha",
		"title":       "Alpha",
		"description": "Alpha",
		"repo_path":   "/repo/alpha",
	})
	betaReq := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "req-beta",
		"title":       "Beta",
		"description": "Beta",
		"repo_path":   "/repo/beta",
	})
	for _, evt := range []state.Event{alphaReq, betaReq} {
		if err := s.eventStore.Append(evt); err != nil {
			t.Fatalf("append %s: %v", evt.Type, err)
		}
		if err := s.projStore.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evt.Type, err)
		}
	}

	alphaStory := seedStoryWithID(t, s, "req-alpha", "story-alpha")
	betaStory := seedStoryWithID(t, s, "req-beta", "story-beta")

	emitAndProject(t, s, state.EventStoryEscalated, "dashboard", betaStory, map[string]any{
		"from_tier": 0,
		"to_tier":   1,
		"reason":    "beta escalation",
	})
	emitAndProject(t, s, state.EventStoryEscalated, "dashboard", alphaStory, map[string]any{
		"from_tier": 0,
		"to_tier":   2,
		"reason":    "alpha escalation",
	})

	if err := s.eventStore.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", alphaStory, map[string]any{"source": "test"})); err != nil {
		t.Fatalf("append alpha review failed: %v", err)
	}
	if err := s.eventStore.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", betaStory, map[string]any{"source": "test"})); err != nil {
		t.Fatalf("append beta review failed: %v", err)
	}
	if err := s.eventStore.Append(state.NewEvent(state.EventStoryCompleted, "reviewer", alphaStory, map[string]any{"source": "test"})); err != nil {
		t.Fatalf("append alpha completed: %v", err)
	}

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	if len(snap.Requirements) != 1 || snap.Requirements[0].ID != "req-alpha" {
		t.Fatalf("requirements = %+v, want only req-alpha", snap.Requirements)
	}
	if len(snap.Stories) != 1 || snap.Stories[0].ID != "story-alpha" {
		t.Fatalf("stories = %+v, want only story-alpha", snap.Stories)
	}
	if len(snap.Escalations) != 1 || snap.Escalations[0].StoryID != "story-alpha" {
		t.Fatalf("escalations = %+v, want only story-alpha", snap.Escalations)
	}
	if len(snap.Events) == 0 {
		t.Fatal("expected visible events for req-alpha")
	}
	for _, evt := range snap.Events {
		if evt.StoryID == "story-beta" {
			t.Fatalf("events = %+v, should not include beta story events", snap.Events)
		}
	}
}

func TestSetDAG_NilClearsDAG(t *testing.T) {
	s := newTestServer(t)
	s.SetDAG(&graph.DAGExport{})
	s.SetDAG(nil)

	if s.dagExport != nil {
		t.Error("expected dagExport to be nil after SetDAG(nil)")
	}
}

// emitAndProject creates and projects an event.
func emitAndProject(t *testing.T, s *Server, evtType state.EventType, agentID, storyID string, payload map[string]any) {
	t.Helper()
	if payload == nil {
		payload = map[string]any{}
	}
	evt := state.NewEvent(evtType, agentID, storyID, payload)
	if err := s.eventStore.Append(evt); err != nil {
		t.Fatalf("emit %s: %v", evtType, err)
	}
	if err := s.projStore.Project(evt); err != nil {
		t.Fatalf("project %s: %v", evtType, err)
	}
}

// seedStoryWithID is a variant of seedStory that allows specifying a custom story ID.
func seedStoryWithID(t *testing.T, s *Server, reqID, storyID string) string {
	t.Helper()
	evt := state.NewEvent(state.EventStoryCreated, "system", storyID, map[string]any{
		"id":                  storyID,
		"req_id":              reqID,
		"title":               "Test Story " + storyID,
		"description":         "A test story",
		"acceptance_criteria": "It works",
		"complexity":          2,
	})
	if err := s.eventStore.Append(evt); err != nil {
		t.Fatalf("seed story append: %v", err)
	}
	if err := s.projStore.Project(evt); err != nil {
		t.Fatalf("seed story project: %v", err)
	}
	return storyID
}
