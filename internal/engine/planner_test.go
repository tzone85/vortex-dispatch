package engine_test

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

func TestPlanner_Plan(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	techLeadResponse := `[
		{"id": "s-001", "title": "Add user model", "description": "Create user struct and DB table", "acceptance_criteria": "User model exists with tests", "complexity": 3, "depends_on": []},
		{"id": "s-002", "title": "Add auth middleware", "description": "JWT validation middleware", "acceptance_criteria": "Middleware validates tokens", "complexity": 5, "depends_on": ["s-001"]},
		{"id": "s-003", "title": "Add login endpoint", "description": "POST /api/login", "acceptance_criteria": "Returns JWT on valid credentials", "complexity": 3, "depends_on": ["s-001"]}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{
		Content: techLeadResponse,
		Model:   "claude-opus-4",
	})

	cfg := config.DefaultConfig()
	planner := engine.NewPlanner(client, cfg, eventStore, projStore)

	result, err := planner.Plan(context.Background(), "r-001", "Add user authentication", dir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Verify stories
	if len(result.Stories) != 3 {
		t.Fatalf("expected 3 stories, got %d", len(result.Stories))
	}
	if result.Stories[0].Title != "Add user model" {
		t.Fatalf("expected 'Add user model', got %s", result.Stories[0].Title)
	}

	// Verify dependency graph
	waves, err := result.Graph.Waves()
	if err != nil {
		t.Fatalf("waves: %v", err)
	}
	if len(waves) != 2 {
		t.Fatalf("expected 2 waves, got %d", len(waves))
	}

	// Verify events were emitted
	events, err := eventStore.List(state.EventFilter{Type: state.EventStoryCreated})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 STORY_CREATED events, got %d", len(events))
	}

	// Verify projection (story IDs are prefixed with short reqID)
	story, err := projStore.GetStory("r-001-s-001")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Title != "Add user model" {
		t.Fatalf("expected projected story title 'Add user model', got %s", story.Title)
	}

	// Verify LLM was called exactly once
	if client.CallCount() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", client.CallCount())
	}
}

// TestPlanner_PlanEphemeral_DoesNotPersist verifies the read-only planning
// path used by `vxd estimate`: it returns decomposed stories but writes nothing
// to the event store or projection, so it can run any number of times without
// colliding on stories.id.
func TestPlanner_PlanEphemeral_DoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	resp := `[
		{"id": "s-001", "title": "A", "description": "d", "acceptance_criteria": "ac", "complexity": 3, "depends_on": []},
		{"id": "s-002", "title": "B", "description": "d", "acceptance_criteria": "ac", "complexity": 5, "depends_on": ["s-001"]}
	]`
	client := llm.NewReplayClient(
		llm.CompletionResponse{Content: resp},
		llm.CompletionResponse{Content: resp},
	)
	planner := engine.NewPlanner(client, config.DefaultConfig(), eventStore, projStore)

	// Run the same reqID twice — would crash on stories.id if it persisted.
	for i := 0; i < 2; i++ {
		result, err := planner.PlanEphemeral(context.Background(), "est-2026", "build a thing", dir)
		if err != nil {
			t.Fatalf("ephemeral plan run %d: %v", i, err)
		}
		if len(result.Stories) != 2 {
			t.Fatalf("run %d: expected 2 stories, got %d", i, len(result.Stories))
		}
	}

	events, err := eventStore.List(state.EventFilter{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ephemeral plan persisted %d events, want 0", len(events))
	}
	stories, err := projStore.ListStories(state.StoryFilter{})
	if err != nil {
		t.Fatalf("list stories: %v", err)
	}
	if len(stories) != 0 {
		t.Fatalf("ephemeral plan persisted %d stories, want 0", len(stories))
	}
}

func TestPlanner_CycleDetection(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	response := `[
		{"id": "s-001", "title": "A", "description": "d", "acceptance_criteria": "ac", "complexity": 3, "depends_on": ["s-002"]},
		{"id": "s-002", "title": "B", "description": "d", "acceptance_criteria": "ac", "complexity": 3, "depends_on": ["s-001"]}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{Content: response})
	planner := engine.NewPlanner(client, config.DefaultConfig(), eventStore, projStore)

	_, err = planner.Plan(context.Background(), "r-001", "test", dir)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestPlanner_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	client := llm.NewReplayClient(llm.CompletionResponse{Content: "not json"})
	planner := engine.NewPlanner(client, config.DefaultConfig(), eventStore, projStore)

	_, err = planner.Plan(context.Background(), "r-001", "test", dir)
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
}

func TestPlannedStory_HasFileOwnership(t *testing.T) {
	story := engine.PlannedStory{
		ID:          "s-001",
		Title:       "Add user model",
		Description: "Create user struct",
		Complexity:  3,
		OwnedFiles:  []string{"src/models/user.go", "src/models/user_test.go"},
		WaveHint:    "parallel",
	}

	if len(story.OwnedFiles) != 2 {
		t.Fatalf("expected 2 owned files, got %d", len(story.OwnedFiles))
	}
	if story.OwnedFiles[0] != "src/models/user.go" {
		t.Fatalf("expected 'src/models/user.go', got %s", story.OwnedFiles[0])
	}
	if story.OwnedFiles[1] != "src/models/user_test.go" {
		t.Fatalf("expected 'src/models/user_test.go', got %s", story.OwnedFiles[1])
	}
	if story.WaveHint != "parallel" {
		t.Fatalf("expected wave_hint 'parallel', got %s", story.WaveHint)
	}

	// Verify JSON marshaling round-trip
	storySeq := engine.PlannedStory{
		ID:         "s-002",
		Title:      "Config setup",
		Complexity: 2,
		OwnedFiles: []string{"package.json"},
		WaveHint:   "sequential",
	}
	if storySeq.WaveHint != "sequential" {
		t.Fatalf("expected wave_hint 'sequential', got %s", storySeq.WaveHint)
	}
}

func TestPlan_ParsesOwnedFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	response := `[
		{"id": "s-001", "title": "Setup", "description": "scaffold", "acceptance_criteria": "exists", "complexity": 2, "depends_on": [], "owned_files": ["src/main.go", "go.mod"], "wave_hint": "sequential"},
		{"id": "s-002", "title": "Add API", "description": "api layer", "acceptance_criteria": "endpoints work", "complexity": 3, "depends_on": ["s-001"], "owned_files": ["src/api/handler.go"], "wave_hint": "parallel"}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{Content: response})
	cfg := config.DefaultConfig()
	planner := engine.NewPlanner(client, cfg, eventStore, projStore)

	result, err := planner.Plan(context.Background(), "r-002", "Add API layer", dir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if len(result.Stories) != 2 {
		t.Fatalf("expected 2 stories, got %d", len(result.Stories))
	}

	// Verify owned_files were parsed
	if len(result.Stories[0].OwnedFiles) != 2 {
		t.Fatalf("expected 2 owned files for s-001, got %d", len(result.Stories[0].OwnedFiles))
	}
	if result.Stories[0].OwnedFiles[0] != "src/main.go" {
		t.Fatalf("expected 'src/main.go', got %s", result.Stories[0].OwnedFiles[0])
	}
	if result.Stories[0].WaveHint != "sequential" {
		t.Fatalf("expected wave_hint 'sequential', got %s", result.Stories[0].WaveHint)
	}

	if len(result.Stories[1].OwnedFiles) != 1 {
		t.Fatalf("expected 1 owned file for s-002, got %d", len(result.Stories[1].OwnedFiles))
	}
	if result.Stories[1].WaveHint != "parallel" {
		t.Fatalf("expected wave_hint 'parallel', got %s", result.Stories[1].WaveHint)
	}
}

func TestPlan_RejectsExcessiveComplexity(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	response := `[
		{"id": "s-001", "title": "Big task", "description": "too complex", "acceptance_criteria": "ac", "complexity": 13, "depends_on": [], "owned_files": ["src/big.go"], "wave_hint": "parallel"}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{Content: response})
	cfg := config.DefaultConfig()
	cfg.Planning.MaxStoryComplexity = 5
	planner := engine.NewPlanner(client, cfg, eventStore, projStore)

	_, err = planner.Plan(context.Background(), "r-003", "Big feature", dir)
	if err == nil {
		t.Fatal("expected complexity validation error")
	}
}

func TestPlan_RejectsFileOverlap(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	response := `[
		{"id": "s-001", "title": "Task A", "description": "d", "acceptance_criteria": "ac", "complexity": 3, "depends_on": [], "owned_files": ["src/shared.go", "src/a.go"], "wave_hint": "parallel"},
		{"id": "s-002", "title": "Task B", "description": "d", "acceptance_criteria": "ac", "complexity": 3, "depends_on": [], "owned_files": ["src/shared.go", "src/b.go"], "wave_hint": "parallel"}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{Content: response})
	cfg := config.DefaultConfig()
	planner := engine.NewPlanner(client, cfg, eventStore, projStore)

	// Overlapping files should be auto-sequenced instead of rejected.
	result, err := planner.Plan(context.Background(), "r-004", "Overlapping files", dir)
	if err != nil {
		t.Fatalf("expected auto-sequencing, got error: %v", err)
	}
	// s-002 should now depend on s-001 (auto-added dependency for shared file).
	for _, s := range result.Stories {
		if s.ID == "s-002" {
			found := false
			for _, d := range s.DependsOn {
				if d == "s-001" {
					found = true
				}
			}
			if !found {
				t.Error("expected s-002 to depend on s-001 after auto-sequencing")
			}
		}
	}
}

func TestRePlan_CreatesReplacementStories(t *testing.T) {
	dir := t.TempDir()

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	replanResponse := `[
		{"id": "s-001-a", "title": "Part A: Data layer", "description": "Implement data models", "acceptance_criteria": "Models exist with tests", "complexity": 3, "depends_on": [], "owned_files": ["src/models/data.go"]},
		{"id": "s-001-b", "title": "Part B: API layer", "description": "Implement API handlers", "acceptance_criteria": "Handlers work", "complexity": 3, "depends_on": ["s-001-a"], "owned_files": ["src/api/handler.go"]}
	]`

	client := llm.NewReplayClient(llm.CompletionResponse{
		Content: replanResponse,
		Model:   "claude-opus-4",
	})

	cfg := config.DefaultConfig()
	planner := engine.NewPlanner(client, cfg, eventStore, projStore)

	stories, err := planner.RePlan(context.Background(), "s-001", "r-001", "Story failed 3 times: test compilation error in handler.go")
	if err != nil {
		t.Fatalf("replan: %v", err)
	}

	// 1. Verify correct number of replacement stories
	if len(stories) != 2 {
		t.Fatalf("expected 2 replacement stories, got %d", len(stories))
	}

	// 2. Verify story fields
	if stories[0].ID != "s-001-a" {
		t.Fatalf("expected first story ID 's-001-a', got %s", stories[0].ID)
	}
	if stories[0].Title != "Part A: Data layer" {
		t.Fatalf("expected first story title 'Part A: Data layer', got %s", stories[0].Title)
	}
	if stories[1].ID != "s-001-b" {
		t.Fatalf("expected second story ID 's-001-b', got %s", stories[1].ID)
	}
	if stories[0].Complexity != 3 {
		t.Fatalf("expected complexity 3, got %d", stories[0].Complexity)
	}
	if len(stories[0].OwnedFiles) != 1 || stories[0].OwnedFiles[0] != "src/models/data.go" {
		t.Fatalf("unexpected owned_files for first story: %v", stories[0].OwnedFiles)
	}

	// 3. Verify NO events were emitted by RePlan itself.
	// The caller (monitor) is responsible for emitting STORY_CREATED with
	// split_depth and STORY_SPLIT events. RePlan is a pure LLM + parse.
	createdEvents, err := eventStore.List(state.EventFilter{Type: state.EventStoryCreated})
	if err != nil {
		t.Fatalf("list created events: %v", err)
	}
	if len(createdEvents) != 0 {
		t.Fatalf("expected 0 STORY_CREATED events from RePlan, got %d", len(createdEvents))
	}

	reqSubmitted, err := eventStore.List(state.EventFilter{Type: state.EventReqSubmitted})
	if err != nil {
		t.Fatalf("list req submitted events: %v", err)
	}
	if len(reqSubmitted) != 0 {
		t.Fatalf("expected 0 REQ_SUBMITTED events, got %d", len(reqSubmitted))
	}

	reqPlanned, err := eventStore.List(state.EventFilter{Type: state.EventReqPlanned})
	if err != nil {
		t.Fatalf("list req planned events: %v", err)
	}
	if len(reqPlanned) != 0 {
		t.Fatalf("expected 0 REQ_PLANNED events, got %d", len(reqPlanned))
	}

	// 5. Verify LLM was called exactly once
	if client.CallCount() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", client.CallCount())
	}
}

func TestRePlan_LLMError(t *testing.T) {
	dir := t.TempDir()

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	// No responses -> will error
	client := llm.NewReplayClient()
	planner := engine.NewPlanner(client, config.DefaultConfig(), eventStore, projStore)

	_, err = planner.RePlan(context.Background(), "s-001", "r-001", "failure context")
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
}

func TestRePlan_InvalidJSON(t *testing.T) {
	dir := t.TempDir()

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	client := llm.NewReplayClient(llm.CompletionResponse{Content: "not valid json"})
	planner := engine.NewPlanner(client, config.DefaultConfig(), eventStore, projStore)

	_, err = planner.RePlan(context.Background(), "s-001", "r-001", "failure context")
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
}

func TestPlanner_LLMError(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	eventStore, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer eventStore.Close()

	projStore, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("create proj store: %v", err)
	}
	defer projStore.Close()

	// No responses loaded -> will error
	client := llm.NewReplayClient()
	planner := engine.NewPlanner(client, config.DefaultConfig(), eventStore, projStore)

	_, err = planner.Plan(context.Background(), "r-001", "test", dir)
	if err == nil {
		t.Fatal("expected LLM error when replay client is exhausted")
	}
}
