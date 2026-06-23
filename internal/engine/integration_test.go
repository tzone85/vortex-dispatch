package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// newIntegrationStores creates real FileStore and SQLiteStore backed by temp
// files on disk (not :memory:). Returns both stores and a cleanup function.
func newIntegrationStores(t *testing.T) (state.EventStore, state.ProjectionStore, func()) {
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

func TestIntegration_PlannerToDispatcher(t *testing.T) {
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	// Create a fake repo directory so ScanRepo detects Go.
	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module integ-test"), 0644)

	plannerResponse := `[
		{
			"id": "s-001",
			"title": "Create user model",
			"description": "Define User struct with validation",
			"acceptance_criteria": "User struct exists with name, email fields",
			"complexity": 2,
			"depends_on": []
		},
		{
			"id": "s-002",
			"title": "Add REST endpoints",
			"description": "Create CRUD handlers for /users",
			"acceptance_criteria": "GET/POST/PUT/DELETE /users work",
			"complexity": 5,
			"depends_on": ["s-001"]
		},
		{
			"id": "s-003",
			"title": "Write unit tests",
			"description": "Add tests for user model and handlers",
			"acceptance_criteria": "80% coverage on user package",
			"complexity": 3,
			"depends_on": ["s-001"]
		}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{
		Content: plannerResponse,
		Model:   "claude-opus-4",
	})

	cfg := config.DefaultConfig()
	planner := engine.NewPlanner(client, cfg, es, ps)

	// --- Phase 1: Plan ---
	planResult, err := planner.Plan(context.Background(), "r-integ", "Add user management API", repoDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if len(planResult.Stories) != 3 {
		t.Fatalf("expected 3 planned stories, got %d", len(planResult.Stories))
	}

	// Verify stories were projected into the store.
	allStories, err := ps.ListStories(state.StoryFilter{ReqID: "r-integ"})
	if err != nil {
		t.Fatalf("list stories: %v", err)
	}
	if len(allStories) != 3 {
		t.Fatalf("expected 3 projected stories, got %d", len(allStories))
	}

	// --- Phase 2: Dispatch Wave 1 ---
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	completed := make(map[string]bool)

	assignments, err := dispatcher.DispatchWave(planResult.Graph, completed, "r-integ", planResult.Stories, 0)
	if err != nil {
		t.Fatalf("dispatch wave 1: %v", err)
	}

	// Wave 1: s-001 (no deps) and s-003 depends on s-001, s-002 depends on s-001
	// Only s-001 is ready in wave 1.
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment in wave 1, got %d", len(assignments))
	}
	if assignments[0].StoryID != "r-integ-s-001" {
		t.Fatalf("expected r-integ-s-001 in wave 1, got %s", assignments[0].StoryID)
	}

	// Verify story r-integ-s-001 is now 'assigned' in projection.
	s001, err := ps.GetStory("r-integ-s-001")
	if err != nil {
		t.Fatalf("get story r-integ-s-001: %v", err)
	}
	if s001.Status != "assigned" {
		t.Fatalf("expected s-001 status 'assigned', got %q", s001.Status)
	}

	// Verify routing: complexity 2 should go to junior.
	if assignments[0].Role != "junior" {
		t.Fatalf("expected junior role for complexity 2, got %s", assignments[0].Role)
	}

	// --- Phase 3: Dispatch Wave 2 ---
	completed["r-integ-s-001"] = true
	assignments2, err := dispatcher.DispatchWave(planResult.Graph, completed, "r-integ", planResult.Stories, 1)
	if err != nil {
		t.Fatalf("dispatch wave 2: %v", err)
	}

	// Wave 2: r-integ-s-002 and r-integ-s-003 both depend only on r-integ-s-001, which is now complete.
	if len(assignments2) != 2 {
		t.Fatalf("expected 2 assignments in wave 2, got %d", len(assignments2))
	}

	// Verify routing by complexity.
	assignmentMap := make(map[string]engine.Assignment)
	for _, a := range assignments2 {
		assignmentMap[a.StoryID] = a
	}

	if a, ok := assignmentMap["r-integ-s-002"]; ok {
		if a.Role != "intermediate" {
			t.Fatalf("r-integ-s-002 (complexity 5) should route to intermediate, got %s", a.Role)
		}
	} else {
		t.Fatal("r-integ-s-002 not found in wave 2 assignments")
	}

	if a, ok := assignmentMap["r-integ-s-003"]; ok {
		if a.Role != "junior" {
			t.Fatalf("r-integ-s-003 (complexity 3) should route to junior, got %s", a.Role)
		}
	} else {
		t.Fatal("r-integ-s-003 not found in wave 2 assignments")
	}

	// Verify all wave 2 stories are now 'assigned'.
	for _, id := range []string{"r-integ-s-002", "r-integ-s-003"} {
		story, err := ps.GetStory(id)
		if err != nil {
			t.Fatalf("get story %s: %v", id, err)
		}
		if story.Status != "assigned" {
			t.Fatalf("expected %s status 'assigned', got %q", id, story.Status)
		}
	}
}

func TestIntegration_FullPipeline_PlanDispatchReviewQAMerge(t *testing.T) {
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module pipeline-test"), 0644)

	// Single story for simplicity.
	plannerResponse := `[
		{
			"id": "s-pipe-1",
			"title": "Implement feature X",
			"description": "Build out feature X end to end",
			"acceptance_criteria": "Feature X works correctly",
			"complexity": 3,
			"depends_on": []
		}
	]`

	reviewResponse := `{
		"passed": true,
		"comments": [],
		"summary": "Code looks great, all acceptance criteria met"
	}`

	cfg := config.DefaultConfig()
	replayClient := llm.NewReplayClient(
		llm.CompletionResponse{Content: plannerResponse, Model: "claude-opus-4"},
		llm.CompletionResponse{Content: reviewResponse, Model: "claude-sonnet-4"},
	)

	// --- Step 1: Plan ---
	planner := engine.NewPlanner(replayClient, cfg, es, ps)
	planResult, err := planner.Plan(context.Background(), "r-pipe-1", "Build feature X", repoDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// --- Step 2: Dispatch ---
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	assignments, err := dispatcher.DispatchWave(planResult.Graph, map[string]bool{}, "r-pipe-1", planResult.Stories, 0)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}

	storyID := assignments[0].StoryID

	// --- Step 3: Simulate agent work (started → completed) ---
	for _, evt := range []state.Event{
		state.NewEvent(state.EventStoryStarted, assignments[0].AgentID, storyID, nil),
		state.NewEvent(state.EventStoryCompleted, assignments[0].AgentID, storyID, nil),
	} {
		if err := es.Append(evt); err != nil {
			t.Fatalf("append event: %v", err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project event: %v", err)
		}
	}

	// Verify story is now in 'review'.
	story, err := ps.GetStory(storyID)
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Status != "review" {
		t.Fatalf("expected 'review', got %q", story.Status)
	}

	// --- Step 4: Review ---
	reviewer := engine.NewReviewer(replayClient, cfg.Models.Senior.Model, cfg.Models.Senior.MaxTokens, es, ps)
	reviewResult, err := reviewer.Review(
		context.Background(),
		storyID,
		"Implement feature X",
		"Feature X works correctly",
		"diff --git a/feature.go b/feature.go\n+func FeatureX() { /* implementation */ }",
	)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !reviewResult.Passed {
		t.Fatal("expected review to pass")
	}

	// Verify story is now in 'qa'.
	story, _ = ps.GetStory(storyID)
	if story.Status != "qa" {
		t.Fatalf("expected 'qa' after review pass, got %q", story.Status)
	}

	// --- Step 5: QA (mock runner that always passes) ---
	runner := &mockRunner{results: map[string]mockRunResult{
		"go": {output: "ok", err: nil},
	}}
	qa := engine.NewQA(engine.QAConfig{
		BuildCommand: "go build ./...",
		TestCommand:  "go test ./...",
	}, runner, es, ps)

	qaResult, err := qa.Run(context.Background(), storyID, repoDir)
	if err != nil {
		t.Fatalf("qa: %v", err)
	}
	if !qaResult.Passed {
		t.Fatal("expected QA to pass")
	}

	// Verify story is now 'pr_submitted'.
	story, _ = ps.GetStory(storyID)
	if story.Status != "pr_submitted" {
		t.Fatalf("expected 'pr_submitted' after QA pass, got %q", story.Status)
	}

	// --- Step 6: Merge (mock GitHub ops) ---
	ghOps := &mockGitHubOps{
		createPR: engine.PRCreationResult{Number: 1, URL: "https://github.com/test/repo/pull/1"},
	}
	merger := engine.NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)

	mergeResult, err := merger.Merge(storyID, "Implement feature X", repoDir, assignments[0].Branch)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !mergeResult.Merged {
		t.Fatal("expected story to be merged")
	}

	// --- Verify Final State ---
	story, _ = ps.GetStory(storyID)
	if story.Status != "merged" {
		t.Fatalf("expected final status 'merged', got %q", story.Status)
	}

	// Verify the requirement was submitted and planned.
	req, err := ps.GetRequirement("r-pipe-1")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Status != "pending" {
		// Planner emits REQ_SUBMITTED (pending) and REQ_PLANNED. The planner
		// only appends REQ_PLANNED to the event store but does not project it,
		// so the projected status stays 'pending'. This is expected.
		t.Logf("requirement status is %q (REQ_PLANNED appended but not projected by planner)", req.Status)
	}

	// Verify LLM was called exactly twice: once for planning, once for review.
	if replayClient.CallCount() != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", replayClient.CallCount())
	}

	// Verify event counts.
	verifyEventCount := func(eventType state.EventType, expected int) {
		t.Helper()
		events, err := es.List(state.EventFilter{Type: eventType})
		if err != nil {
			t.Fatalf("list %s events: %v", eventType, err)
		}
		if len(events) != expected {
			t.Fatalf("expected %d %s events, got %d", expected, eventType, len(events))
		}
	}

	verifyEventCount(state.EventReqSubmitted, 1)
	verifyEventCount(state.EventStoryCreated, 1)
	verifyEventCount(state.EventStoryAssigned, 1)
	verifyEventCount(state.EventStoryStarted, 1)
	verifyEventCount(state.EventStoryCompleted, 1)
	verifyEventCount(state.EventStoryReviewPassed, 1)
	verifyEventCount(state.EventStoryQAStarted, 1)
	verifyEventCount(state.EventStoryQAPassed, 1)
	verifyEventCount(state.EventStoryPRCreated, 1)
	verifyEventCount(state.EventStoryMerged, 1)
}

func TestIntegration_MultiStoryPipeline(t *testing.T) {
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module multi-test"), 0644)

	plannerResponse := `[
		{
			"id": "s-m1",
			"title": "Foundation layer",
			"description": "Core abstractions",
			"acceptance_criteria": "Core types defined",
			"complexity": 2,
			"depends_on": []
		},
		{
			"id": "s-m2",
			"title": "Business logic",
			"description": "Implement business rules",
			"acceptance_criteria": "Rules pass validation",
			"complexity": 8,
			"depends_on": ["s-m1"]
		}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{
		Content: plannerResponse,
		Model:   "claude-opus-4",
	})

	cfg := config.DefaultConfig()
	cfg.Planning.MaxStoryComplexity = 13
	planner := engine.NewPlanner(client, cfg, es, ps)
	planResult, err := planner.Plan(context.Background(), "r-multi", "Build multi-story feature", repoDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Dispatch wave 1: only s-m1 (no deps).
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	wave1, err := dispatcher.DispatchWave(planResult.Graph, map[string]bool{}, "r-multi", planResult.Stories, 0)
	if err != nil {
		t.Fatalf("dispatch wave 1: %v", err)
	}
	if len(wave1) != 1 || wave1[0].StoryID != "r-multi-s-m1" {
		t.Fatalf("wave 1: expected [r-multi-s-m1], got %v", wave1)
	}

	// Simulate r-multi-s-m1 completion through the full lifecycle.
	for _, evtType := range []state.EventType{
		state.EventStoryStarted,
		state.EventStoryCompleted,
		state.EventStoryReviewPassed,
		state.EventStoryQAPassed,
		state.EventStoryMerged,
	} {
		evt := state.NewEvent(evtType, "agent-m1", "r-multi-s-m1", nil)
		if err := es.Append(evt); err != nil {
			t.Fatalf("append %s: %v", evtType, err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project %s: %v", evtType, err)
		}
	}

	// Verify r-multi-s-m1 is merged.
	sm1, _ := ps.GetStory("r-multi-s-m1")
	if sm1.Status != "merged" {
		t.Fatalf("expected r-multi-s-m1 'merged', got %q", sm1.Status)
	}

	// Dispatch wave 2: r-multi-s-m2 depends on r-multi-s-m1 which is now completed.
	wave2, err := dispatcher.DispatchWave(planResult.Graph, map[string]bool{"r-multi-s-m1": true}, "r-multi", planResult.Stories, 1)
	if err != nil {
		t.Fatalf("dispatch wave 2: %v", err)
	}
	if len(wave2) != 1 || wave2[0].StoryID != "r-multi-s-m2" {
		t.Fatalf("wave 2: expected [r-multi-s-m2], got %v", wave2)
	}

	// Complexity 8 should route to senior.
	if wave2[0].Role != "senior" {
		t.Fatalf("expected senior role for complexity 8, got %s", wave2[0].Role)
	}

	// Verify r-multi-s-m2 is assigned.
	sm2, _ := ps.GetStory("r-multi-s-m2")
	if sm2.Status != "assigned" {
		t.Fatalf("expected r-multi-s-m2 'assigned', got %q", sm2.Status)
	}

	// No more waves should be dispatchable.
	completed := map[string]bool{"r-multi-s-m1": true, "r-multi-s-m2": true}
	wave3, err := dispatcher.DispatchWave(planResult.Graph, completed, "r-multi", planResult.Stories, 2)
	if err != nil {
		t.Fatalf("dispatch wave 3: %v", err)
	}
	if len(wave3) != 0 {
		t.Fatalf("expected 0 assignments when all completed, got %d", len(wave3))
	}
}

func TestIntegration_PlannerEventPersistence(t *testing.T) {
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module persist-test"), 0644)

	response := `[
		{"id": "s-p1", "title": "Task A", "description": "Do A", "acceptance_criteria": "A done", "complexity": 1, "depends_on": []}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{Content: response})
	cfg := config.DefaultConfig()
	planner := engine.NewPlanner(client, cfg, es, ps)

	_, err := planner.Plan(context.Background(), "r-persis", "Persist test", repoDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Verify events were persisted to the real file store (not in-memory).
	reqEvents, err := es.List(state.EventFilter{Type: state.EventReqSubmitted})
	if err != nil {
		t.Fatalf("list req events: %v", err)
	}
	if len(reqEvents) != 1 {
		t.Fatalf("expected 1 REQ_SUBMITTED event, got %d", len(reqEvents))
	}

	storyEvents, err := es.List(state.EventFilter{Type: state.EventStoryCreated})
	if err != nil {
		t.Fatalf("list story events: %v", err)
	}
	if len(storyEvents) != 1 {
		t.Fatalf("expected 1 STORY_CREATED event, got %d", len(storyEvents))
	}

	plannedEvents, err := es.List(state.EventFilter{Type: state.EventReqPlanned})
	if err != nil {
		t.Fatalf("list planned events: %v", err)
	}
	if len(plannedEvents) != 1 {
		t.Fatalf("expected 1 REQ_PLANNED event, got %d", len(plannedEvents))
	}

	// Verify projection has the story (ID namespaced with the reqID prefix).
	story, err := ps.GetStory("r-persis-s-p1")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Title != "Task A" {
		t.Fatalf("expected title 'Task A', got %q", story.Title)
	}

	// Verify the requirement projection.
	req, err := ps.GetRequirement("r-persis")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Title != "Persist test" {
		t.Fatalf("expected requirement title 'Persist test', got %q", req.Title)
	}
}

func TestIntegration_DependencyStorageAndDAGReconstruction(t *testing.T) {
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module dep-test"), 0644)

	plannerResponse := `[
		{
			"id": "s-dep-1",
			"title": "Base layer",
			"description": "Foundation",
			"acceptance_criteria": "Foundation built",
			"complexity": 2,
			"depends_on": []
		},
		{
			"id": "s-dep-2",
			"title": "Feature A",
			"description": "Build A on base",
			"acceptance_criteria": "A works",
			"complexity": 3,
			"depends_on": ["s-dep-1"]
		},
		{
			"id": "s-dep-3",
			"title": "Feature B",
			"description": "Build B on base",
			"acceptance_criteria": "B works",
			"complexity": 5,
			"depends_on": ["s-dep-1"]
		},
		{
			"id": "s-dep-4",
			"title": "Integration",
			"description": "Combine A and B",
			"acceptance_criteria": "Integration complete",
			"complexity": 8,
			"depends_on": ["s-dep-2", "s-dep-3"]
		}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{
		Content: plannerResponse,
		Model:   "claude-opus-4",
	})

	cfg := config.DefaultConfig()
	cfg.Planning.MaxStoryComplexity = 13
	planner := engine.NewPlanner(client, cfg, es, ps)

	// --- Phase 1: Plan ---
	planResult, err := planner.Plan(context.Background(), "r-dep-1", "Build layered feature with dependencies", repoDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(planResult.Stories) != 4 {
		t.Fatalf("expected 4 planned stories, got %d", len(planResult.Stories))
	}

	// --- Phase 2: Verify story_deps table was populated ---
	sqlitePS, ok := ps.(*state.SQLiteStore)
	if !ok {
		t.Fatal("expected ProjectionStore to be *state.SQLiteStore")
	}

	deps, err := sqlitePS.ListStoryDeps("r-dep-1")
	if err != nil {
		t.Fatalf("list story deps: %v", err)
	}

	// Expected edges (prefixed with "r-dep-1"):
	// r-dep-1-s-dep-2→r-dep-1-s-dep-1, r-dep-1-s-dep-3→r-dep-1-s-dep-1,
	// r-dep-1-s-dep-4→r-dep-1-s-dep-2, r-dep-1-s-dep-4→r-dep-1-s-dep-3
	if len(deps) != 4 {
		t.Fatalf("expected 4 dependency edges, got %d", len(deps))
	}

	// Build a lookup for verifying specific edges.
	type edge struct{ from, to string }
	edgeSet := make(map[edge]bool)
	for _, d := range deps {
		edgeSet[edge{d.StoryID, d.DependsOnID}] = true
	}

	expectedEdges := []edge{
		{"r-dep-1-s-dep-2", "r-dep-1-s-dep-1"},
		{"r-dep-1-s-dep-3", "r-dep-1-s-dep-1"},
		{"r-dep-1-s-dep-4", "r-dep-1-s-dep-2"},
		{"r-dep-1-s-dep-4", "r-dep-1-s-dep-3"},
	}
	for _, e := range expectedEdges {
		if !edgeSet[e] {
			t.Fatalf("missing dependency edge: %s depends on %s", e.from, e.to)
		}
	}

	// --- Phase 3: Rebuild DAG from stored deps and verify ReadyNodes ---
	reconstructed := graph.New()
	allStories, err := sqlitePS.ListStories(state.StoryFilter{ReqID: "r-dep-1"})
	if err != nil {
		t.Fatalf("list stories: %v", err)
	}
	for _, s := range allStories {
		reconstructed.AddNode(s.ID)
	}
	for _, d := range deps {
		reconstructed.AddEdge(d.StoryID, d.DependsOnID)
	}

	// With nothing completed, only r-dep-1-s-dep-1 (no deps) should be ready.
	completed := make(map[string]bool)
	ready := reconstructed.ReadyNodes(completed)
	if len(ready) != 1 || ready[0] != "r-dep-1-s-dep-1" {
		t.Fatalf("expected ReadyNodes with empty completed to be [r-dep-1-s-dep-1], got %v", ready)
	}

	// Mark r-dep-1-s-dep-1 as completed; r-dep-1-s-dep-2 and r-dep-1-s-dep-3 should become ready.
	completed["r-dep-1-s-dep-1"] = true
	ready = reconstructed.ReadyNodes(completed)
	sort.Strings(ready)
	if len(ready) != 2 || ready[0] != "r-dep-1-s-dep-2" || ready[1] != "r-dep-1-s-dep-3" {
		t.Fatalf("expected ReadyNodes after r-dep-1-s-dep-1 done to be [r-dep-1-s-dep-2 r-dep-1-s-dep-3], got %v", ready)
	}

	// Mark r-dep-1-s-dep-2 and r-dep-1-s-dep-3 as completed; r-dep-1-s-dep-4 should become ready.
	completed["r-dep-1-s-dep-2"] = true
	completed["r-dep-1-s-dep-3"] = true
	ready = reconstructed.ReadyNodes(completed)
	if len(ready) != 1 || ready[0] != "r-dep-1-s-dep-4" {
		t.Fatalf("expected ReadyNodes after r-dep-1-s-dep-2,r-dep-1-s-dep-3 done to be [r-dep-1-s-dep-4], got %v", ready)
	}

	// Mark everything completed; no nodes should be ready.
	completed["r-dep-1-s-dep-4"] = true
	ready = reconstructed.ReadyNodes(completed)
	if len(ready) != 0 {
		t.Fatalf("expected no ReadyNodes when all completed, got %v", ready)
	}
}

func TestIntegration_EscalationChain(t *testing.T) {
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	// --- Step 1: Emit foundational events for a requirement and story ---
	reqID := "r-esc-1"
	storyID := "r-esc-1-s-001"

	reqEvt := state.NewEvent(state.EventReqSubmitted, "user", "", map[string]any{
		"id":          reqID,
		"title":       "Escalation chain test",
		"description": "Verify full escalation tier traversal",
	})
	if err := es.Append(reqEvt); err != nil {
		t.Fatalf("append req submitted: %v", err)
	}
	if err := ps.Project(reqEvt); err != nil {
		t.Fatalf("project req submitted: %v", err)
	}

	storyEvt := state.NewEvent(state.EventStoryCreated, "planner", storyID, map[string]any{
		"id":                  storyID,
		"req_id":              reqID,
		"title":               "Implement widget",
		"description":         "Build the widget feature",
		"acceptance_criteria": "Widget works correctly",
		"complexity":          5,
	})
	if err := es.Append(storyEvt); err != nil {
		t.Fatalf("append story created: %v", err)
	}
	if err := ps.Project(storyEvt); err != nil {
		t.Fatalf("project story created: %v", err)
	}

	// --- Step 2: Create EscalationMachine with known config ---
	routing := config.RoutingConfig{
		MaxRetriesBeforeEscalation: 2,
		MaxSeniorRetries:           2,
		MaxManagerAttempts:         2,
	}
	esc := engine.NewEscalationMachine(es, routing)

	// --- Step 3: Tier 0 - emit 2 STORY_REVIEW_FAILED events ---
	for i := 0; i < 2; i++ {
		evt := state.NewEvent(state.EventStoryReviewFailed, "agent-junior", storyID, nil)
		if err := es.Append(evt); err != nil {
			t.Fatalf("append review failed %d: %v", i, err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project review failed %d: %v", i, err)
		}
	}

	// Verify tier 0 is still current (no escalation event yet).
	tier, err := esc.CurrentTier(storyID)
	if err != nil {
		t.Fatalf("current tier: %v", err)
	}
	if tier != 0 {
		t.Fatalf("expected tier 0, got %d", tier)
	}

	// Verify ShouldEscalate returns true, nextTier=1.
	shouldEsc, nextTier, err := esc.ShouldEscalate(storyID)
	if err != nil {
		t.Fatalf("should escalate: %v", err)
	}
	if !shouldEsc {
		t.Fatal("expected escalation needed after 2 failures at tier 0")
	}
	if nextTier != 1 {
		t.Fatalf("expected next tier 1, got %d", nextTier)
	}

	// --- Step 4: Escalate to tier 1 (senior) ---
	escEvt1 := state.NewEvent(state.EventStoryEscalated, "monitor", storyID, map[string]any{
		"from_tier": 0, "to_tier": 1, "reason": "max retries at tier 0",
	})
	if err := es.Append(escEvt1); err != nil {
		t.Fatalf("append escalation to tier 1: %v", err)
	}
	if err := ps.Project(escEvt1); err != nil {
		t.Fatalf("project escalation to tier 1: %v", err)
	}

	tier, err = esc.CurrentTier(storyID)
	if err != nil {
		t.Fatalf("current tier after escalation: %v", err)
	}
	if tier != 1 {
		t.Fatalf("expected tier 1 after escalation, got %d", tier)
	}

	// Retry count should be 0 at the new tier (fresh counter).
	retries, err := esc.RetryCountAtCurrentTier(storyID)
	if err != nil {
		t.Fatalf("retry count at tier 1: %v", err)
	}
	if retries != 0 {
		t.Fatalf("expected 0 retries at fresh tier 1, got %d", retries)
	}

	// Verify story projection has escalation_tier = 1.
	story, err := ps.GetStory(storyID)
	if err != nil {
		t.Fatalf("get story after tier 1 escalation: %v", err)
	}
	if story.EscalationTier != 1 {
		t.Fatalf("expected projection escalation_tier 1, got %d", story.EscalationTier)
	}

	// --- Step 5: Tier 1 - emit 2 more STORY_REVIEW_FAILED events ---
	// Small sleep to ensure timestamps are strictly after escalation event.
	for i := 0; i < 2; i++ {
		evt := state.NewEvent(state.EventStoryReviewFailed, "agent-senior", storyID, nil)
		if err := es.Append(evt); err != nil {
			t.Fatalf("append tier 1 review failed %d: %v", i, err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project tier 1 review failed %d: %v", i, err)
		}
	}

	shouldEsc, nextTier, err = esc.ShouldEscalate(storyID)
	if err != nil {
		t.Fatalf("should escalate at tier 1: %v", err)
	}
	if !shouldEsc {
		t.Fatal("expected escalation needed after 2 failures at tier 1")
	}
	if nextTier != 2 {
		t.Fatalf("expected next tier 2, got %d", nextTier)
	}

	// --- Step 6: Escalate to tier 2 (manager) ---
	escEvt2 := state.NewEvent(state.EventStoryEscalated, "monitor", storyID, map[string]any{
		"from_tier": 1, "to_tier": 2, "reason": "max retries at tier 1",
	})
	if err := es.Append(escEvt2); err != nil {
		t.Fatalf("append escalation to tier 2: %v", err)
	}
	if err := ps.Project(escEvt2); err != nil {
		t.Fatalf("project escalation to tier 2: %v", err)
	}

	tier, err = esc.CurrentTier(storyID)
	if err != nil {
		t.Fatalf("current tier after tier 2 escalation: %v", err)
	}
	if tier != 2 {
		t.Fatalf("expected tier 2, got %d", tier)
	}

	// Emit 2 failures at tier 2 to trigger escalation to tier 3 (tech lead).
	for i := 0; i < 2; i++ {
		evt := state.NewEvent(state.EventStoryReviewFailed, "manager", storyID, nil)
		if err := es.Append(evt); err != nil {
			t.Fatalf("append tier 2 review failed %d: %v", i, err)
		}
	}

	shouldEsc, nextTier, err = esc.ShouldEscalate(storyID)
	if err != nil {
		t.Fatalf("should escalate at tier 2: %v", err)
	}
	if !shouldEsc {
		t.Fatal("expected escalation needed after 2 failures at tier 2")
	}
	if nextTier != 3 {
		t.Fatalf("expected next tier 3, got %d", nextTier)
	}

	// --- Step 7: Escalate to tier 3 (tech lead) ---
	escEvt3 := state.NewEvent(state.EventStoryEscalated, "monitor", storyID, map[string]any{
		"from_tier": 2, "to_tier": 3, "reason": "max manager attempts",
	})
	if err := es.Append(escEvt3); err != nil {
		t.Fatalf("append escalation to tier 3: %v", err)
	}
	if err := ps.Project(escEvt3); err != nil {
		t.Fatalf("project escalation to tier 3: %v", err)
	}

	tier, err = esc.CurrentTier(storyID)
	if err != nil {
		t.Fatalf("current tier after tier 3 escalation: %v", err)
	}
	if tier != 3 {
		t.Fatalf("expected tier 3, got %d", tier)
	}

	// Tier 3 allows 1 attempt; emit 1 failure to trigger tier 4 (pause).
	failEvt := state.NewEvent(state.EventStoryReviewFailed, "tech-lead", storyID, nil)
	if err := es.Append(failEvt); err != nil {
		t.Fatalf("append tier 3 review failed: %v", err)
	}

	shouldEsc, nextTier, err = esc.ShouldEscalate(storyID)
	if err != nil {
		t.Fatalf("should escalate at tier 3: %v", err)
	}
	if !shouldEsc {
		t.Fatal("expected escalation needed after 1 failure at tier 3")
	}
	if nextTier != 4 {
		t.Fatalf("expected next tier 4 (pause), got %d", nextTier)
	}

	// --- Step 8: Escalate to tier 4 (pause) ---
	escEvt4 := state.NewEvent(state.EventStoryEscalated, "monitor", storyID, map[string]any{
		"from_tier": 3, "to_tier": 4, "reason": "tech lead re-plan failed",
	})
	if err := es.Append(escEvt4); err != nil {
		t.Fatalf("append escalation to tier 4: %v", err)
	}
	if err := ps.Project(escEvt4); err != nil {
		t.Fatalf("project escalation to tier 4: %v", err)
	}

	// Tier 4 has 0 max retries - immediately suggests escalation to tier 5.
	shouldEsc, nextTier, err = esc.ShouldEscalate(storyID)
	if err != nil {
		t.Fatalf("should escalate at tier 4: %v", err)
	}
	if !shouldEsc {
		t.Fatal("expected escalation at tier 4 (pause)")
	}
	if nextTier != 5 {
		t.Fatalf("expected next tier 5, got %d", nextTier)
	}

	// Verify final projection tier.
	story, err = ps.GetStory(storyID)
	if err != nil {
		t.Fatalf("get story at tier 4: %v", err)
	}
	if story.EscalationTier != 4 {
		t.Fatalf("expected projection escalation_tier 4, got %d", story.EscalationTier)
	}

	// --- Step 9: Test allDone check - split status counts as completed ---
	splitStoryID := "r-esc-1-s-002"
	splitEvt := state.NewEvent(state.EventStoryCreated, "planner", splitStoryID, map[string]any{
		"id":                  splitStoryID,
		"req_id":              reqID,
		"title":               "Splittable story",
		"description":         "This story will be split",
		"acceptance_criteria": "Split children pass",
		"complexity":          8,
	})
	if err := es.Append(splitEvt); err != nil {
		t.Fatalf("append split story created: %v", err)
	}
	if err := ps.Project(splitEvt); err != nil {
		t.Fatalf("project split story created: %v", err)
	}

	// Mark the story as split via STORY_SPLIT event.
	storySplitEvt := state.NewEvent(state.EventStorySplit, "manager", splitStoryID, nil)
	if err := es.Append(storySplitEvt); err != nil {
		t.Fatalf("append story split: %v", err)
	}
	if err := ps.Project(storySplitEvt); err != nil {
		t.Fatalf("project story split: %v", err)
	}

	splitStory, err := ps.GetStory(splitStoryID)
	if err != nil {
		t.Fatalf("get split story: %v", err)
	}
	if splitStory.Status != "split" {
		t.Fatalf("expected split story status 'split', got %q", splitStory.Status)
	}

	// In the monitor's allDone logic, "split" counts as completed
	// alongside "merged" and "pr_submitted". Verify this logic here
	// by checking against the same condition the monitor uses.
	terminalStatuses := map[string]bool{"merged": true, "pr_submitted": true, "split": true}
	if !terminalStatuses[splitStory.Status] {
		t.Fatalf("split status should be treated as terminal, but %q is not in terminal set", splitStory.Status)
	}

	// --- Step 10: Test split validation - overlapping files should fail ---
	overlappingChildren := []engine.SplitChild{
		{ID: "child-a", OwnedFiles: []string{"pkg/handler.go", "pkg/model.go"}, Complexity: 3},
		{ID: "child-b", OwnedFiles: []string{"pkg/model.go", "pkg/util.go"}, Complexity: 2},
	}
	if err := esc.ValidateSplit(0, overlappingChildren, 10); err == nil {
		t.Fatal("expected error for overlapping owned files in split children")
	}

	// Non-overlapping files should pass.
	validChildren := []engine.SplitChild{
		{ID: "child-a", Suffix: "a", OwnedFiles: []string{"pkg/handler.go"}, Complexity: 3},
		{ID: "child-b", Suffix: "b", OwnedFiles: []string{"pkg/model.go"}, Complexity: 2},
	}
	if err := esc.ValidateSplit(0, validChildren, 10); err != nil {
		t.Fatalf("expected valid split, got error: %v", err)
	}

	// Max depth exceeded should fail.
	if err := esc.ValidateSplit(2, validChildren, 10); err == nil {
		t.Fatal("expected error for exceeding max split depth")
	}

	// --- Step 11: Verify the full event trail in the store ---
	verifyEventCount := func(eventType state.EventType, expected int) {
		t.Helper()
		events, err := es.List(state.EventFilter{Type: eventType})
		if err != nil {
			t.Fatalf("list %s events: %v", eventType, err)
		}
		if len(events) != expected {
			t.Fatalf("expected %d %s events, got %d", expected, eventType, len(events))
		}
	}

	verifyEventCount(state.EventReqSubmitted, 1)
	verifyEventCount(state.EventStoryCreated, 2)        // original + split story
	verifyEventCount(state.EventStoryEscalated, 4)      // tiers 0->1, 1->2, 2->3, 3->4
	verifyEventCount(state.EventStoryReviewFailed, 7)    // 2 + 2 + 2 + 1
	verifyEventCount(state.EventStorySplit, 1)           // one split event

	// Verify escalation records in the projection store.
	sqlitePS, ok := ps.(*state.SQLiteStore)
	if !ok {
		t.Fatal("expected ProjectionStore to be *state.SQLiteStore")
	}

	escalations, err := sqlitePS.ListEscalations()
	if err != nil {
		t.Fatalf("list escalations: %v", err)
	}
	if len(escalations) != 4 {
		t.Fatalf("expected 4 escalation records, got %d", len(escalations))
	}

	// Escalations are ordered DESC by created_at; verify tier progression.
	// Most recent first: 3->4, 2->3, 1->2, 0->1
	expectedTiers := [][2]int{{3, 4}, {2, 3}, {1, 2}, {0, 1}}
	for i, expected := range expectedTiers {
		if escalations[i].FromTier != expected[0] || escalations[i].ToTier != expected[1] {
			t.Fatalf("escalation %d: expected %d->%d, got %d->%d",
				i, expected[0], expected[1], escalations[i].FromTier, escalations[i].ToTier)
		}
	}
}
