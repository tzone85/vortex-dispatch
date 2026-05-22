package state

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Project — various event types for fuller coverage
// ---------------------------------------------------------------------------

func TestProject_StoryProgress_Ignored(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	// Progress events should be silently ignored
	evt := NewEvent(EventStoryProgress, "agent-1", "S1", map[string]any{"status": "50%"})
	if err := s.Project(evt); err != nil {
		t.Errorf("expected nil error for progress event, got: %v", err)
	}
}

func TestProject_FullStoryLifecycle(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	// Create requirement
	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-LC1", "title": "Lifecycle", "repo_path": "/tmp/repo",
	}))

	// Create story
	s.Project(NewEvent(EventStoryCreated, "", "S-LC1", map[string]any{
		"id": "S-LC1", "req_id": "REQ-LC1", "title": "Story 1",
		"complexity": 5, "acceptance_criteria": "Tests pass",
	}))

	// Assign
	s.Project(NewEvent(EventStoryAssigned, "agent-1", "S-LC1", map[string]any{
		"agent_id": "agent-1", "wave": 1,
	}))
	story, _ := s.GetStory("S-LC1")
	if story.Status != "assigned" {
		t.Errorf("expected assigned, got %s", story.Status)
	}

	// Start
	s.Project(NewEvent(EventStoryStarted, "agent-1", "S-LC1", nil))
	story, _ = s.GetStory("S-LC1")
	if story.Status != "in_progress" {
		t.Errorf("expected in_progress, got %s", story.Status)
	}

	// Complete
	s.Project(NewEvent(EventStoryCompleted, "agent-1", "S-LC1", nil))
	story, _ = s.GetStory("S-LC1")
	if story.Status != "review" {
		t.Errorf("expected review, got %s", story.Status)
	}

	// Review requested
	s.Project(NewEvent(EventStoryReviewRequested, "reviewer", "S-LC1", nil))
	story, _ = s.GetStory("S-LC1")
	if story.Status != "review" {
		t.Errorf("expected review, got %s", story.Status)
	}

	// Review passed
	s.Project(NewEvent(EventStoryReviewPassed, "reviewer", "S-LC1", nil))
	story, _ = s.GetStory("S-LC1")
	if story.Status != "qa" {
		t.Errorf("expected qa, got %s", story.Status)
	}

	// QA started
	s.Project(NewEvent(EventStoryQAStarted, "", "S-LC1", nil))

	// QA passed
	s.Project(NewEvent(EventStoryQAPassed, "", "S-LC1", nil))
	story, _ = s.GetStory("S-LC1")
	if story.Status != "pr_submitted" {
		t.Errorf("expected pr_submitted, got %s", story.Status)
	}
}

func TestProject_ReviewFailed_ResetsStory(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-RF1", "title": "Review Fail"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-RF1", map[string]any{
		"id": "S-RF1", "req_id": "REQ-RF1", "title": "Story", "complexity": 3,
	}))
	s.Project(NewEvent(EventStoryAssigned, "", "S-RF1", map[string]any{"agent_id": "a1"}))
	s.Project(NewEvent(EventStoryStarted, "", "S-RF1", nil))
	s.Project(NewEvent(EventStoryCompleted, "", "S-RF1", nil))

	// Review failed
	s.Project(NewEvent(EventStoryReviewFailed, "", "S-RF1", nil))
	story, _ := s.GetStory("S-RF1")
	if story.Status != "draft" {
		t.Errorf("expected draft after review fail, got %s", story.Status)
	}
}

func TestProject_QAFailed_ResetsStory(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-QF1", "title": "QA Fail"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-QF1", map[string]any{
		"id": "S-QF1", "req_id": "REQ-QF1", "title": "Story", "complexity": 3,
	}))

	// Skip to QA and fail
	s.Project(NewEvent(EventStoryQAStarted, "", "S-QF1", nil))
	s.Project(NewEvent(EventStoryQAFailed, "", "S-QF1", nil))
	story, _ := s.GetStory("S-QF1")
	if story.Status != "draft" {
		t.Errorf("expected draft after QA fail, got %s", story.Status)
	}
}

func TestProject_StoryAwaitingApproval(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-AA1", "title": "Await"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-AA1", map[string]any{
		"id": "S-AA1", "req_id": "REQ-AA1", "title": "Story", "complexity": 3,
	}))

	s.Project(NewEvent(EventStoryAwaitingApproval, "", "S-AA1", nil))
	story, _ := s.GetStory("S-AA1")
	if story.Status != "awaiting_approval" {
		t.Errorf("expected awaiting_approval, got %s", story.Status)
	}
}

func TestProject_StoryRejected_ResetsStory(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-RJ1", "title": "Reject"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-RJ1", map[string]any{
		"id": "S-RJ1", "req_id": "REQ-RJ1", "title": "Story", "complexity": 3,
	}))
	s.Project(NewEvent(EventStoryAwaitingApproval, "", "S-RJ1", nil))
	s.Project(NewEvent(EventStoryRejected, "", "S-RJ1", nil))

	story, _ := s.GetStory("S-RJ1")
	if story.Status != "draft" {
		t.Errorf("expected draft after rejection, got %s", story.Status)
	}
}

func TestProject_StoryReset_ResetsStory(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-RS1", "title": "Reset"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-RS1", map[string]any{
		"id": "S-RS1", "req_id": "REQ-RS1", "title": "Story", "complexity": 3,
	}))
	s.Project(NewEvent(EventStoryStarted, "", "S-RS1", nil))
	s.Project(NewEvent(EventStoryReset, "", "S-RS1", nil))

	story, _ := s.GetStory("S-RS1")
	if story.Status != "draft" {
		t.Errorf("expected draft after reset, got %s", story.Status)
	}
}

func TestProject_ReqAnalyzed(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-AN1", "title": "Analyze"}))
	s.Project(NewEvent(EventReqAnalyzed, "", "", map[string]any{"id": "REQ-AN1"}))

	req, _ := s.GetRequirement("REQ-AN1")
	if req.Status != "analyzed" {
		t.Errorf("expected analyzed, got %s", req.Status)
	}
}

func TestProject_ReqCompleted(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-CO1", "title": "Complete"}))
	s.Project(NewEvent(EventReqCompleted, "", "", map[string]any{"id": "REQ-CO1"}))

	req, _ := s.GetRequirement("REQ-CO1")
	if req.Status != "completed" {
		t.Errorf("expected completed, got %s", req.Status)
	}
}

// ---------------------------------------------------------------------------
// ListStories — various filters
// ---------------------------------------------------------------------------

func TestListStories_ByReqID(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-LS1", "title": "R1"}))
	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-LS2", "title": "R2"}))

	s.Project(NewEvent(EventStoryCreated, "", "S-LS1", map[string]any{
		"id": "S-LS1", "req_id": "REQ-LS1", "title": "S1", "complexity": 3,
	}))
	s.Project(NewEvent(EventStoryCreated, "", "S-LS2", map[string]any{
		"id": "S-LS2", "req_id": "REQ-LS2", "title": "S2", "complexity": 3,
	}))

	stories, err := s.ListStories(StoryFilter{ReqID: "REQ-LS1"})
	if err != nil {
		t.Fatalf("list stories: %v", err)
	}
	if len(stories) != 1 {
		t.Errorf("expected 1 story for REQ-LS1, got %d", len(stories))
	}
}

func TestListStories_NoFilter(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-NF1", "title": "R1"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-NF1", map[string]any{
		"id": "S-NF1", "req_id": "REQ-NF1", "title": "S1", "complexity": 3,
	}))
	s.Project(NewEvent(EventStoryCreated, "", "S-NF2", map[string]any{
		"id": "S-NF2", "req_id": "REQ-NF1", "title": "S2", "complexity": 5,
	}))

	stories, err := s.ListStories(StoryFilter{})
	if err != nil {
		t.Fatalf("list stories: %v", err)
	}
	if len(stories) < 2 {
		t.Errorf("expected at least 2 stories, got %d", len(stories))
	}
}

// ---------------------------------------------------------------------------
// ListEscalations
// ---------------------------------------------------------------------------

func TestListEscalations_Empty(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	escs, err := s.ListEscalations()
	if err != nil {
		t.Fatalf("list escalations: %v", err)
	}
	if len(escs) != 0 {
		t.Errorf("expected 0 escalations, got %d", len(escs))
	}
}

// ---------------------------------------------------------------------------
// BackfillAcceptanceCriteria — with events
// ---------------------------------------------------------------------------

func TestBackfillAcceptanceCriteria_Updates(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{"id": "REQ-BF1", "title": "Backfill"}))
	s.Project(NewEvent(EventStoryCreated, "", "S-BF1", map[string]any{
		"id": "S-BF1", "req_id": "REQ-BF1", "title": "Story",
		"complexity": 3, "acceptance_criteria": "Tests pass",
	}))

	// Backfill with the creation event
	events := []Event{
		NewEvent(EventStoryCreated, "", "S-BF1", map[string]any{
			"id":                  "S-BF1",
			"acceptance_criteria": "Tests pass and coverage > 80%",
		}),
	}
	s.BackfillAcceptanceCriteria(events)

	story, _ := s.GetStory("S-BF1")
	if story.AcceptanceCriteria == "" {
		t.Error("expected non-empty acceptance criteria after backfill")
	}
}

// ---------------------------------------------------------------------------
// State machine guard — STORY_STARTED for terminal stories
// ---------------------------------------------------------------------------

// TestProject_StoryStarted_IgnoredForTerminalStatuses verifies that a
// STORY_STARTED event does not regress a story that is already in a terminal
// state (merged, pr_submitted, awaiting_approval, split). This is
// defense-in-depth: the auto-resume fix in PR #40 prevents the event from
// being emitted, but the projection layer now also rejects such transitions so
// that hand-edits or future bugs surface immediately rather than silently
// corrupting state.
func TestProject_StoryStarted_IgnoredForTerminalStatuses(t *testing.T) {
	terminalStatuses := []struct {
		name          string
		setupEvent    EventType
		setupPayload  map[string]any
		expectedFinal string
	}{
		{
			name:          "awaiting_approval",
			setupEvent:    EventStoryAwaitingApproval,
			expectedFinal: "awaiting_approval",
		},
		{
			name:          "merged",
			setupEvent:    EventStoryMerged,
			expectedFinal: "merged",
		},
		{
			name:          "pr_submitted",
			setupEvent:    EventStoryPRCreated,
			setupPayload:  map[string]any{"pr_number": 1, "pr_url": "https://github.com/test/repo/pull/1"},
			expectedFinal: "pr_submitted",
		},
		{
			name:          "split",
			setupEvent:    EventStorySplit,
			expectedFinal: "split",
		},
	}

	for _, tc := range terminalStatuses {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatalf("create store: %v", err)
			}
			defer s.Close()

			reqID := "REQ-SM-" + tc.name
			storyID := "S-SM-" + tc.name

			// Setup: create requirement + story, then transition to terminal state.
			s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{
				"id": reqID, "title": "State machine guard test",
			}))
			s.Project(NewEvent(EventStoryCreated, "", storyID, map[string]any{
				"id": storyID, "req_id": reqID, "title": "Guard test story", "complexity": 1,
			}))

			// Transition to terminal state.
			pl := tc.setupPayload
			if pl == nil {
				pl = map[string]any{}
			}
			if err := s.Project(NewEvent(tc.setupEvent, "", storyID, pl)); err != nil {
				t.Fatalf("setup terminal event %s: %v", tc.setupEvent, err)
			}

			before, err := s.GetStory(storyID)
			if err != nil {
				t.Fatalf("get story: %v", err)
			}
			if before.Status != tc.expectedFinal {
				t.Fatalf("expected status %q after setup, got %q", tc.expectedFinal, before.Status)
			}

			// Now emit STORY_STARTED — it should be rejected, leaving status unchanged.
			if err := s.Project(NewEvent(EventStoryStarted, "agent-x", storyID, map[string]any{})); err != nil {
				t.Errorf("guardedStartStory returned unexpected error: %v", err)
			}

			after, err := s.GetStory(storyID)
			if err != nil {
				t.Fatalf("get story after guard: %v", err)
			}
			if after.Status != tc.expectedFinal {
				t.Errorf("STORY_STARTED regressed status from %q to %q — guard did not fire",
					tc.expectedFinal, after.Status)
			}
		})
	}
}

// TestProject_StoryStarted_AllowedFromNonTerminalStatuses verifies that the
// guard does not block the normal path: a story in draft/assigned transitions
// to in_progress when STORY_STARTED is received.
func TestProject_StoryStarted_AllowedFromNonTerminalStatuses(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	s.Project(NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-SM-NR1", "title": "Normal start",
	}))
	s.Project(NewEvent(EventStoryCreated, "", "S-SM-NR1", map[string]any{
		"id": "S-SM-NR1", "req_id": "REQ-SM-NR1", "title": "Normal start story", "complexity": 1,
	}))

	// Story is in draft — STORY_STARTED should transition it to in_progress.
	if err := s.Project(NewEvent(EventStoryStarted, "agent-1", "S-SM-NR1", map[string]any{})); err != nil {
		t.Fatalf("Project STORY_STARTED: %v", err)
	}

	story, err := s.GetStory("S-SM-NR1")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Status != "in_progress" {
		t.Errorf("expected in_progress after STORY_STARTED from draft, got %q", story.Status)
	}
}

// ---------------------------------------------------------------------------
// story_databases projection tests
// ---------------------------------------------------------------------------

func TestProject_StoryDBCreated_InsertsRow(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	data, _ := json.Marshal(map[string]any{
		"db_id":            "abc123",
		"db_name":          "vxd-test-s1",
		"provider":         "docker",
		"template":         "tpl",
		"conn_string_hash": "sha256:deadbeef",
	})
	evt := Event{
		Type:      EventStoryDBCreated,
		StoryID:   "s1",
		Timestamp: time.Now(),
		Payload:   data,
	}
	if err := s.Project(evt); err != nil {
		t.Fatalf("Project: %v", err)
	}
	var status string
	if err := s.db.QueryRow(
		`SELECT status FROM story_databases WHERE story_id='s1' AND db_id='abc123'`,
	).Scan(&status); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "created" {
		t.Errorf("status = %q, want created", status)
	}
}

func TestProject_StoryDBDeleted_UpdatesRow(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	createdData, _ := json.Marshal(map[string]any{
		"db_id":   "abc123",
		"db_name": "vxd-test-s1",
	})
	if err := s.Project(Event{
		Type:      EventStoryDBCreated,
		StoryID:   "s1",
		Timestamp: time.Now(),
		Payload:   createdData,
	}); err != nil {
		t.Fatal(err)
	}

	deletedData, _ := json.Marshal(map[string]any{
		"db_id":            "abc123",
		"duration_seconds": 12.5,
		"status":           "deleted",
	})
	if err := s.Project(Event{
		Type:      EventStoryDBDeleted,
		StoryID:   "s1",
		Timestamp: time.Now(),
		Payload:   deletedData,
	}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.db.QueryRow(
		`SELECT status FROM story_databases WHERE db_id='abc123'`,
	).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "deleted" {
		t.Errorf("status = %q, want deleted", status)
	}
}

func TestProject_StoryDBFailed_InsertsRow(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer s.Close()

	data, _ := json.Marshal(map[string]any{
		"db_id":    "abc-failed",
		"db_name":  "vxd-test-s1",
		"provider": "docker",
		"error":    "docker daemon unreachable",
	})
	if err := s.Project(Event{
		Type:      EventStoryDBFailed,
		StoryID:   "s1",
		Timestamp: time.Now(),
		Payload:   data,
	}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.db.QueryRow(
		`SELECT status FROM story_databases WHERE story_id='s1'`,
	).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
}
