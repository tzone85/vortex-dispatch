//go:build e2e

package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// mockGitHubOps is a test double for the GitHubOps interface.
type mockGitHubOps struct {
	createPR  engine.PRCreationResult
	createErr error
	pushErr   error
	mergeErr  error
}

func (m *mockGitHubOps) PushBranch(_, _ string) error {
	return m.pushErr
}

func (m *mockGitHubOps) CreatePR(_, _, _, _, _ string) (engine.PRCreationResult, error) {
	return m.createPR, m.createErr
}

func (m *mockGitHubOps) MergePR(_ string, _ int) error {
	return m.mergeErr
}

// mockRunner is a test double for CommandRunner that always succeeds.
type mockRunner struct{}

func (m *mockRunner) Run(_ context.Context, _, _ string, _ ...string) (string, error) {
	return "ok", nil
}

// newStores creates real FileStore + SQLiteStore in a temp directory.
func newStores(t *testing.T) (*state.FileStore, *state.SQLiteStore, func()) {
	t.Helper()

	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	es, err := state.NewFileStore(filepath.Join(resolved, "events.jsonl"))
	if err != nil {
		t.Fatalf("create file store: %v", err)
	}

	ps, err := state.NewSQLiteStore(filepath.Join(resolved, "projections.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}

	cleanup := func() {
		es.Close()
		ps.Close()
	}
	return es, ps, cleanup
}

// TestE2E_FullPipeline simulates the complete VXD lifecycle:
// init -> req -> plan -> dispatch -> execute -> review -> QA -> merge
//
// This test uses real file-backed stores and ReplayClient (no live LLM calls).
func TestE2E_FullPipeline(t *testing.T) {
	es, ps, cleanup := newStores(t)
	defer cleanup()

	// Simulate `vxd init` — create a project directory with a go.mod.
	projectDir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	projectDir = resolved

	if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module e2e-project"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// Pre-record LLM responses for the entire pipeline.
	planResponse := `[
		{
			"id": "s-e2e-1",
			"title": "Create data models",
			"description": "Define core data structures",
			"acceptance_criteria": "All models defined with validation",
			"complexity": 2,
			"depends_on": []
		},
		{
			"id": "s-e2e-2",
			"title": "Build API endpoints",
			"description": "REST handlers for all operations",
			"acceptance_criteria": "CRUD endpoints return correct responses",
			"complexity": 5,
			"depends_on": ["s-e2e-1"]
		},
		{
			"id": "s-e2e-3",
			"title": "Add authentication",
			"description": "JWT-based auth middleware",
			"acceptance_criteria": "Protected endpoints require valid JWT",
			"complexity": 3,
			"depends_on": ["s-e2e-1"]
		}
	]`

	reviewPassResponse := `{
		"passed": true,
		"comments": [],
		"summary": "All acceptance criteria met, code is clean"
	}`

	replayClient := llm.NewReplayClient(
		// Call 1: Planner
		llm.CompletionResponse{Content: planResponse, Model: "claude-opus-4"},
		// Call 2: Review for s-e2e-1
		llm.CompletionResponse{Content: reviewPassResponse, Model: "claude-sonnet-4"},
		// Call 3: Review for s-e2e-2
		llm.CompletionResponse{Content: reviewPassResponse, Model: "claude-sonnet-4"},
		// Call 4: Review for s-e2e-3
		llm.CompletionResponse{Content: reviewPassResponse, Model: "claude-sonnet-4"},
	)

	cfg := config.DefaultConfig()
	reqID := "r-e2e-001"

	// ==========================================
	// Phase 1: Plan (simulates `vxd req`)
	// ==========================================
	planner := engine.NewPlanner(replayClient, cfg, es, ps)
	planResult, err := planner.Plan(context.Background(), reqID, "Build a complete user management API", projectDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if len(planResult.Stories) != 3 {
		t.Fatalf("expected 3 stories, got %d", len(planResult.Stories))
	}

	// Verify requirement exists in projection.
	req, err := ps.GetRequirement(reqID)
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Title != "Build a complete user management API" {
		t.Fatalf("unexpected requirement title: %q", req.Title)
	}

	// ==========================================
	// Phase 2: Dispatch and process all waves
	// ==========================================
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	reviewer := engine.NewReviewer(replayClient, cfg.Models.Senior.Model, cfg.Models.Senior.MaxTokens, es, ps)
	runner := &mockRunner{}
	qa := engine.NewQA(engine.QAConfig{
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
	}, runner, es, ps)
	ghOps := &mockGitHubOps{
		createPR: engine.PRCreationResult{Number: 100, URL: "https://github.com/test/repo/pull/100"},
	}
	merger := engine.NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)

	completed := make(map[string]bool)
	prCounter := 0

	// Process waves until all stories are done.
	for wave := 0; wave < 10; wave++ { // safety limit
		assignments, err := dispatcher.DispatchWave(planResult.Graph, completed, reqID, planResult.Stories, wave)
		if err != nil {
			t.Fatalf("dispatch wave %d: %v", wave, err)
		}

		if len(assignments) == 0 {
			break
		}

		t.Logf("Wave %d: dispatching %d stories", wave, len(assignments))

		for _, assignment := range assignments {
			storyID := assignment.StoryID
			agentID := assignment.AgentID

			// Simulate: agent starts work.
			startEvt := state.NewEvent(state.EventStoryStarted, agentID, storyID, nil)
			if err := es.Append(startEvt); err != nil {
				t.Fatalf("append start: %v", err)
			}
			if err := ps.Project(startEvt); err != nil {
				t.Fatalf("project start: %v", err)
			}

			// Simulate: agent completes work.
			completeEvt := state.NewEvent(state.EventStoryCompleted, agentID, storyID, nil)
			if err := es.Append(completeEvt); err != nil {
				t.Fatalf("append complete: %v", err)
			}
			if err := ps.Project(completeEvt); err != nil {
				t.Fatalf("project complete: %v", err)
			}

			// Review the story.
			story, err := ps.GetStory(storyID)
			if err != nil {
				t.Fatalf("get story %s: %v", storyID, err)
			}

			reviewResult, err := reviewer.Review(
				context.Background(),
				storyID,
				story.Title,
				"Acceptance criteria met",
				"diff --git a/feature.go\n+func Feature() {}",
			)
			if err != nil {
				t.Fatalf("review %s: %v", storyID, err)
			}
			if !reviewResult.Passed {
				t.Fatalf("review for %s should pass", storyID)
			}

			// QA the story.
			qaResult, err := qa.Run(context.Background(), storyID, projectDir)
			if err != nil {
				t.Fatalf("qa %s: %v", storyID, err)
			}
			if !qaResult.Passed {
				t.Fatalf("QA for %s should pass", storyID)
			}

			// Merge the story.
			prCounter++
			ghOps.createPR = engine.PRCreationResult{
				Number: prCounter,
				URL:    "https://github.com/test/repo/pull/" + string(rune('0'+prCounter)),
			}

			mergeResult, err := merger.Merge(storyID, story.Title, projectDir, assignment.Branch)
			if err != nil {
				t.Fatalf("merge %s: %v", storyID, err)
			}
			if !mergeResult.Merged {
				t.Fatalf("expected %s to be merged", storyID)
			}

			completed[storyID] = true
		}
	}

	// ==========================================
	// Phase 3: Verify end state
	// ==========================================

	// All 3 stories should be completed.
	if len(completed) != 3 {
		t.Fatalf("expected 3 completed stories, got %d", len(completed))
	}

	// Every story should have status = "merged".
	// Use the actual prefixed IDs from planResult (planner prefixes with reqID[:8]).
	for _, ps0 := range planResult.Stories {
		story, err := ps.GetStory(ps0.ID)
		if err != nil {
			t.Fatalf("get story %s: %v", ps0.ID, err)
		}
		if story.Status != "merged" {
			t.Fatalf("expected story %s status 'merged', got %q", ps0.ID, story.Status)
		}
	}

	// Mark requirement as completed.
	reqCompleteEvt := state.NewEvent(state.EventReqCompleted, "system", "", map[string]any{
		"id": reqID,
	})
	if err := es.Append(reqCompleteEvt); err != nil {
		t.Fatalf("append req complete: %v", err)
	}
	if err := ps.Project(reqCompleteEvt); err != nil {
		t.Fatalf("project req complete: %v", err)
	}

	req, err = ps.GetRequirement(reqID)
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "completed" {
		t.Fatalf("expected requirement status 'completed', got %q", req.Status)
	}

	// Verify event store has expected events.
	allEvents, err := es.List(state.EventFilter{})
	if err != nil {
		t.Fatalf("list all events: %v", err)
	}
	t.Logf("Total events emitted: %d", len(allEvents))

	// Verify key event counts.
	verifyCount := func(evtType state.EventType, expected int) {
		t.Helper()
		events, err := es.List(state.EventFilter{Type: evtType})
		if err != nil {
			t.Fatalf("list %s: %v", evtType, err)
		}
		if len(events) != expected {
			t.Fatalf("expected %d %s events, got %d", expected, evtType, len(events))
		}
	}

	verifyCount(state.EventReqSubmitted, 1)
	verifyCount(state.EventStoryCreated, 3)
	verifyCount(state.EventAgentSpawned, 3)
	verifyCount(state.EventStoryAssigned, 3)
	verifyCount(state.EventStoryStarted, 3)
	verifyCount(state.EventStoryCompleted, 3)
	verifyCount(state.EventStoryReviewPassed, 3)
	verifyCount(state.EventStoryQAStarted, 3)
	verifyCount(state.EventStoryQAPassed, 3)
	verifyCount(state.EventStoryPRCreated, 3)
	verifyCount(state.EventStoryMerged, 3)
	verifyCount(state.EventReqCompleted, 1)

	// Verify LLM was called 4 times: 1 plan + 3 reviews.
	if replayClient.CallCount() != 4 {
		t.Fatalf("expected 4 LLM calls, got %d", replayClient.CallCount())
	}

	// Verify dependency ordering: the first story (no deps) must have been
	// dispatched before the others. Use the actual prefixed ID.
	assignEvents, err := es.List(state.EventFilter{Type: state.EventStoryAssigned})
	if err != nil {
		t.Fatalf("list assign events: %v", err)
	}
	if len(assignEvents) != 3 {
		t.Fatalf("expected 3 assign events, got %d", len(assignEvents))
	}
	firstStoryID := planResult.Stories[0].ID // the story with no dependencies
	if assignEvents[0].StoryID != firstStoryID {
		t.Fatalf("expected first assigned story to be %s, got %s", firstStoryID, assignEvents[0].StoryID)
	}
}

// TestE2E_SingleStoryFastPath tests the simplest possible pipeline: one
// requirement producing a single story that flows through to merge.
func TestE2E_SingleStoryFastPath(t *testing.T) {
	es, ps, cleanup := newStores(t)
	defer cleanup()

	projectDir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	projectDir = resolved
	_ = os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module fast-path"), 0644)

	planResponse := `[
		{
			"id": "s-fast-1",
			"title": "Quick fix",
			"description": "Fix the bug",
			"acceptance_criteria": "Bug is fixed",
			"complexity": 1,
			"depends_on": []
		}
	]`

	reviewResponse := `{
		"passed": true,
		"comments": [],
		"summary": "LGTM"
	}`

	replayClient := llm.NewReplayClient(
		llm.CompletionResponse{Content: planResponse},
		llm.CompletionResponse{Content: reviewResponse},
	)

	cfg := config.DefaultConfig()

	// Plan.
	planner := engine.NewPlanner(replayClient, cfg, es, ps)
	planResult, err := planner.Plan(context.Background(), "r-fast", "Fix the critical bug", projectDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Dispatch.
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	assignments, err := dispatcher.DispatchWave(planResult.Graph, map[string]bool{}, "r-fast", planResult.Stories, 0)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}

	storyID := assignments[0].StoryID

	// Simulate work.
	for _, evtType := range []state.EventType{state.EventStoryStarted, state.EventStoryCompleted} {
		evt := state.NewEvent(evtType, assignments[0].AgentID, storyID, nil)
		if err := es.Append(evt); err != nil {
			t.Fatalf("append: %v", err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project: %v", err)
		}
	}

	// Review.
	reviewer := engine.NewReviewer(replayClient, cfg.Models.Senior.Model, cfg.Models.Senior.MaxTokens, es, ps)
	_, err = reviewer.Review(context.Background(), storyID, "Quick fix", "Bug is fixed", "diff\n+fix")
	if err != nil {
		t.Fatalf("review: %v", err)
	}

	// QA.
	qa := engine.NewQA(engine.QAConfig{BuildCommand: "go build"}, &mockRunner{}, es, ps)
	_, err = qa.Run(context.Background(), storyID, projectDir)
	if err != nil {
		t.Fatalf("qa: %v", err)
	}

	// Merge.
	ghOps := &mockGitHubOps{
		createPR: engine.PRCreationResult{Number: 1, URL: "https://github.com/test/repo/pull/1"},
	}
	merger := engine.NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)
	mergeResult, err := merger.Merge(storyID, "Quick fix", projectDir, assignments[0].Branch)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !mergeResult.Merged {
		t.Fatal("expected merged")
	}

	// Mark requirement complete.
	reqCompleteEvt := state.NewEvent(state.EventReqCompleted, "system", "", map[string]any{"id": "r-fast"})
	es.Append(reqCompleteEvt)
	ps.Project(reqCompleteEvt)

	// Verify final state.
	story, _ := ps.GetStory(storyID)
	if story.Status != "merged" {
		t.Fatalf("expected 'merged', got %q", story.Status)
	}

	req, _ := ps.GetRequirement("r-fast")
	if req.Status != "completed" {
		t.Fatalf("expected requirement 'completed', got %q", req.Status)
	}
}

// TestE2E_DryRunPipeline exercises the full pipeline with DryRunClient:
// plan → dispatch → simulate agent failure → escalation → retry → QA with criteria → metrics.
func TestE2E_DryRunPipeline(t *testing.T) {
	es, ps, cleanup := newStores(t)
	defer cleanup()

	projectDir := t.TempDir()
	resolved, _ := filepath.EvalSymlinks(projectDir)
	projectDir = resolved
	os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module dryrun-test"), 0644)

	dryClient := llm.NewDryRunClient(0) // no delay for tests
	cfg := config.DefaultConfig()
	reqID := "r-dry-001"

	// ── Phase 1: Plan with DryRunClient ──
	planner := engine.NewPlanner(dryClient, cfg, es, ps)
	result, err := planner.Plan(context.Background(), reqID, "Add health check endpoint with tests", projectDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(result.Stories) == 0 {
		t.Fatal("expected at least 1 story from DryRunClient planning")
	}
	t.Logf("Planned %d stories", len(result.Stories))

	// ── Phase 2: Dispatch wave 1 ──
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	completed := make(map[string]bool)
	assignments, err := dispatcher.DispatchWave(result.Graph, completed, reqID, result.Stories, 1)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(assignments) == 0 {
		t.Fatal("expected at least 1 assignment")
	}
	t.Logf("Dispatched %d stories in wave 1", len(assignments))

	storyID := assignments[0].StoryID
	agentID := assignments[0].AgentID

	// ── Phase 3: Simulate agent failure (no code produced) ──
	startEvt := state.NewEvent(state.EventStoryStarted, agentID, storyID, map[string]any{
		"tier": 0, "role": "junior",
	})
	es.Append(startEvt)
	ps.Project(startEvt)

	// Agent produces no changes → review fails
	failEvt := state.NewEvent(state.EventStoryReviewFailed, "monitor", storyID, map[string]any{
		"reason": "agent produced no code changes",
	})
	es.Append(failEvt)
	ps.Project(failEvt)

	// ── Phase 4: Verify attempt tracking ──
	tracker := engine.NewAttemptTracker(es)
	attempts, err := tracker.ListAttempts(storyID)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Outcome != "review_failed" {
		t.Errorf("attempt outcome = %q, want review_failed", attempts[0].Outcome)
	}
	if attempts[0].Role != "junior" {
		t.Errorf("attempt role = %q, want junior", attempts[0].Role)
	}

	// ── Phase 5: Simulate retry (tier 1, senior) ──
	startEvt2 := state.NewEvent(state.EventStoryStarted, "senior-1", storyID, map[string]any{
		"tier": 1, "role": "senior",
	})
	es.Append(startEvt2)
	ps.Project(startEvt2)

	completeEvt := state.NewEvent(state.EventStoryCompleted, "senior-1", storyID, map[string]any{
		"status": "done",
	})
	es.Append(completeEvt)
	ps.Project(completeEvt)

	// ── Phase 6: Review passes (DryRunClient returns APPROVED) ──
	reviewer := engine.NewReviewer(dryClient, "test-model", 4000, es, ps)
	reviewResult, err := reviewer.Review(
		context.Background(), storyID, "Health check", "endpoint exists",
		"diff --git a/health.go\n+func Health() {}", "",
	)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !reviewResult.Passed {
		t.Fatal("DryRunClient review should pass")
	}

	// ── Phase 7: QA with declarative criteria ──
	qa := engine.NewQA(engine.QAConfig{
		BuildCommand: "echo build-ok",
		TestCommand:  "echo test-ok",
	}, &mockRunner{}, es, ps)

	qaResult, err := qa.Run(context.Background(), storyID, projectDir)
	if err != nil {
		t.Fatalf("qa: %v", err)
	}
	if !qaResult.Passed {
		t.Fatal("QA should pass with mockRunner")
	}

	// ── Phase 8: Merge ──
	ghOps := &mockGitHubOps{
		createPR: engine.PRCreationResult{Number: 1, URL: "https://github.com/test/pull/1"},
	}
	merger := engine.NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)
	mergeResult, err := merger.Merge(storyID, "Health check", projectDir, "vxd/"+storyID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !mergeResult.Merged {
		t.Fatal("expected merge to succeed")
	}
	completed[storyID] = true

	// ── Phase 9: Verify attempt history after success ──
	attempts, _ = tracker.ListAttempts(storyID)
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts after retry, got %d", len(attempts))
	}
	if attempts[0].Outcome != "review_failed" {
		t.Errorf("attempt 1 outcome = %q, want review_failed", attempts[0].Outcome)
	}
	if attempts[1].Outcome != "success" {
		t.Errorf("attempt 2 outcome = %q, want success", attempts[1].Outcome)
	}
	if attempts[1].Role != "senior" {
		t.Errorf("attempt 2 role = %q, want senior", attempts[1].Role)
	}

	// ── Phase 10: Verify metrics ──
	metrics, err := engine.ComputeMetrics(es, ps, 10, "")
	if err != nil {
		t.Fatalf("compute metrics: %v", err)
	}
	if metrics.TotalStories == 0 {
		t.Error("metrics should show at least 1 story")
	}
	if metrics.StoriesMerged == 0 {
		t.Error("metrics should show at least 1 merged story")
	}

	// ── Phase 11: Verify report includes attempts ──
	builder := engine.NewReportBuilder(es, ps, cfg)
	report, err := builder.Build(reqID)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	if len(report.Stories) == 0 {
		t.Fatal("report should contain stories")
	}

	// Find the story with attempts
	var reportStory *engine.ReportStory
	for i := range report.Stories {
		if report.Stories[i].ID == storyID {
			reportStory = &report.Stories[i]
			break
		}
	}
	if reportStory == nil {
		t.Fatalf("story %s not found in report", storyID)
	}
	if len(reportStory.Attempts) != 2 {
		t.Errorf("report story should have 2 attempts, got %d", len(reportStory.Attempts))
	}

	t.Logf("DryRun pipeline complete: %d stories planned, %d attempts tracked, metrics and report verified", len(result.Stories), len(attempts))
}
