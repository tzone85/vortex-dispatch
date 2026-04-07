package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// googleAIResponse builds a Google AI generateContent response JSON from text.
func googleAIResponse(text string) map[string]any {
	return map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"parts": []map[string]any{{"text": text}},
					"role":  "model",
				},
				"finishReason": "STOP",
			},
		},
		"modelVersion": "gemma-4-27b-it",
		"usageMetadata": map[string]any{
			"promptTokenCount":     200,
			"candidatesTokenCount": 100,
		},
	}
}

// anthropicResponse builds an Anthropic Messages API response JSON from text.
func anthropicResponse(text string) map[string]any {
	return map[string]any{
		"content":     []map[string]any{{"type": "text", "text": text}},
		"model":       "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 200, "output_tokens": 100},
	}
}

// TestGoogleAI_FullPipeline_PlanDispatchReviewQAMerge exercises the complete
// VXD pipeline using a mock Google AI Studio server as the LLM backend.
// This is the "Selenium test" — it verifies that GoogleAIClient, the request
// mapping, response parsing, and the engine's JSON consumption all work
// together end-to-end through every pipeline stage.
func TestGoogleAI_FullPipeline_PlanDispatchReviewQAMerge(t *testing.T) {
	// --- Setup: Mock Google AI server ---
	var callCount int32
	plannerResponse := `[
		{
			"id": "s-g001",
			"title": "Create user model",
			"description": "Define User struct in internal/model/user.go",
			"acceptance_criteria": "User struct with name, email, validated",
			"complexity": 2,
			"depends_on": [],
			"owned_files": ["internal/model/user.go"],
			"wave_hint": "parallel"
		},
		{
			"id": "s-g002",
			"title": "Add REST endpoints",
			"description": "CRUD handlers in internal/api/users.go",
			"acceptance_criteria": "GET/POST /users return JSON",
			"complexity": 5,
			"depends_on": ["s-g001"],
			"owned_files": ["internal/api/users.go"],
			"wave_hint": "sequential"
		}
	]`
	reviewResponse := `{
		"passed": true,
		"comments": [{"file": "internal/model/user.go", "line": 10, "severity": "info", "comment": "Good struct design"}],
		"summary": "Clean implementation, all acceptance criteria met"
	}`

	googleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)

		// Verify it's hitting the generateContent endpoint
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Query().Get("key") != "test-google-key" {
			t.Errorf("expected API key in query, got %s", r.URL.Query().Get("key"))
		}

		// Verify request body has Google AI format
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		if _, ok := reqBody["contents"]; !ok {
			t.Error("expected 'contents' in Google AI request body")
		}
		if _, ok := reqBody["generationConfig"]; !ok {
			t.Error("expected 'generationConfig' in Google AI request body")
		}

		// First call = planner, second call = reviewer
		var resp map[string]any
		if n == 1 {
			resp = googleAIResponse(plannerResponse)
		} else {
			resp = googleAIResponse(reviewResponse)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer googleServer.Close()

	// --- Setup: Stores and config ---
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module google-e2e"), 0644)

	cfg := config.DefaultConfig()

	// Wire both planner and reviewer to Google AI (for this test)
	googleClient := llm.NewRetryClient(
		llm.NewGoogleAIClient("test-google-key").WithBaseURL(googleServer.URL),
		2,
	)

	// --- Phase 1: Plan via Google AI ---
	planner := engine.NewPlanner(googleClient, cfg, es, ps)
	planResult, err := planner.Plan(context.Background(), "r-google-1", "Add user management API", repoDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(planResult.Stories) != 2 {
		t.Fatalf("expected 2 stories, got %d", len(planResult.Stories))
	}

	// Verify stories were projected correctly
	stories, err := ps.ListStories(state.StoryFilter{ReqID: "r-google-1"})
	if err != nil {
		t.Fatalf("list stories: %v", err)
	}
	if len(stories) != 2 {
		t.Fatalf("expected 2 projected stories, got %d", len(stories))
	}

	// Verify Google AI was actually called (not some fallback)
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("expected 1 Google AI call for planning, got %d", callCount)
	}

	// --- Phase 2: Dispatch Wave 1 ---
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	assignments, err := dispatcher.DispatchWave(planResult.Graph, map[string]bool{}, "r-google-1", planResult.Stories, 0)
	if err != nil {
		t.Fatalf("dispatch wave 1: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment in wave 1 (s-g001 has no deps), got %d", len(assignments))
	}

	storyID := assignments[0].StoryID

	// Verify complexity routing: complexity 2 → junior (Google/Gemma in default config)
	if assignments[0].Role != "junior" {
		t.Fatalf("expected junior role for complexity 2, got %s", assignments[0].Role)
	}

	// --- Phase 3: Simulate agent execution ---
	for _, evt := range []state.Event{
		state.NewEvent(state.EventStoryStarted, assignments[0].AgentID, storyID, nil),
		state.NewEvent(state.EventStoryCompleted, assignments[0].AgentID, storyID, nil),
	} {
		if err := es.Append(evt); err != nil {
			t.Fatalf("append: %v", err)
		}
		if err := ps.Project(evt); err != nil {
			t.Fatalf("project: %v", err)
		}
	}

	story, _ := ps.GetStory(storyID)
	if story.Status != "review" {
		t.Fatalf("expected 'review', got %q", story.Status)
	}

	// --- Phase 4: Review via Google AI ---
	reviewer := engine.NewReviewer(googleClient, "gemma-4-27b-it", 4000, es, ps)
	reviewResult, err := reviewer.Review(
		context.Background(), storyID,
		"Create user model",
		"User struct with name, email, validated",
		"diff --git a/user.go b/user.go\n+type User struct { Name, Email string }",
	)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if !reviewResult.Passed {
		t.Fatalf("expected review to pass, got: %s", reviewResult.Summary)
	}
	if len(reviewResult.Comments) != 1 {
		t.Fatalf("expected 1 review comment, got %d", len(reviewResult.Comments))
	}

	// Verify Google AI was called twice total (plan + review)
	if atomic.LoadInt32(&callCount) != 2 {
		t.Fatalf("expected 2 Google AI calls total, got %d", callCount)
	}

	story, _ = ps.GetStory(storyID)
	if story.Status != "qa" {
		t.Fatalf("expected 'qa' after review, got %q", story.Status)
	}

	// --- Phase 5: QA ---
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

	// --- Phase 6: Merge ---
	ghOps := &mockGitHubOps{
		createPR: engine.PRCreationResult{Number: 42, URL: "https://github.com/test/repo/pull/42"},
	}
	merger := engine.NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)
	mergeResult, err := merger.Merge(storyID, "Create user model", repoDir, assignments[0].Branch)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !mergeResult.Merged {
		t.Fatal("expected merge")
	}

	// --- Verify Final State ---
	story, _ = ps.GetStory(storyID)
	if story.Status != "merged" {
		t.Fatalf("expected 'merged', got %q", story.Status)
	}

	// Verify full event trail
	allEvents, _ := es.List(state.EventFilter{StoryID: storyID})
	eventTypes := make([]string, 0, len(allEvents))
	for _, e := range allEvents {
		eventTypes = append(eventTypes, string(e.Type))
	}
	t.Logf("Event trail for %s: %v", storyID, eventTypes)

	// Should have: STORY_CREATED, STORY_ASSIGNED, STORY_STARTED, STORY_COMPLETED,
	//              STORY_REVIEW_PASSED, STORY_QA_STARTED, STORY_QA_PASSED,
	//              STORY_PR_CREATED, STORY_MERGED
	if len(allEvents) < 8 {
		t.Fatalf("expected at least 8 events in trail, got %d: %v", len(allEvents), eventTypes)
	}
}

// TestGoogleAI_FallbackMidPipeline verifies that when Google AI returns a
// quota error (429 / RESOURCE_EXHAUSTED) during the review phase, the
// FallbackClient transparently switches to the Anthropic backend and the
// pipeline completes successfully. This is the critical resilience test —
// a real user's free-tier quota can exhaust between planning and review.
func TestGoogleAI_FallbackMidPipeline(t *testing.T) {
	// --- Setup: Google server that succeeds once (planning), then returns 429 ---
	var googleCalls int32
	plannerResponse := `[{
		"id": "s-fb01",
		"title": "Build feature",
		"description": "Implement the feature",
		"acceptance_criteria": "Feature works",
		"complexity": 3,
		"depends_on": [],
		"owned_files": ["feature.go"],
		"wave_hint": "parallel"
	}]`

	googleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&googleCalls, 1)
		if n == 1 {
			// First call (planning): succeed
			resp := googleAIResponse(plannerResponse)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Second call (review): quota exhausted
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":400,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded for generativelanguage.googleapis.com"}}`))
	}))
	defer googleServer.Close()

	// --- Setup: Anthropic fallback server ---
	var anthropicCalls int32
	reviewResponse := `{
		"passed": true,
		"comments": [],
		"summary": "Looks good — verified via Anthropic fallback"
	}`

	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&anthropicCalls, 1)

		// Verify it's Anthropic format (x-api-key header)
		if r.Header.Get("x-api-key") != "test-anthropic-key" {
			t.Errorf("expected Anthropic API key header, got %q", r.Header.Get("x-api-key"))
		}

		resp := anthropicResponse(reviewResponse)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer anthropicServer.Close()

	// --- Build FallbackClient: Google primary, Anthropic fallback ---
	googleClient := llm.NewGoogleAIClient("test-google-key").WithBaseURL(googleServer.URL)
	anthropicClient := llm.NewAnthropicClient("test-anthropic-key").WithBaseURL(anthropicServer.URL)
	fallbackClient := llm.NewFallbackClient(googleClient, anthropicClient)

	// --- Setup: Stores and config ---
	es, ps, cleanup := newIntegrationStores(t)
	defer cleanup()

	repoDir := t.TempDir()
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module fallback-e2e"), 0644)
	cfg := config.DefaultConfig()

	// --- Phase 1: Plan (succeeds via Google) ---
	planner := engine.NewPlanner(fallbackClient, cfg, es, ps)
	planResult, err := planner.Plan(context.Background(), "r-fb-1", "Build a feature", repoDir)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(planResult.Stories) != 1 {
		t.Fatalf("expected 1 story, got %d", len(planResult.Stories))
	}

	// Verify planning went through Google
	if atomic.LoadInt32(&googleCalls) != 1 {
		t.Fatalf("expected 1 Google call for planning, got %d", googleCalls)
	}
	if atomic.LoadInt32(&anthropicCalls) != 0 {
		t.Fatal("Anthropic should not have been called during planning")
	}

	// --- Phase 2: Dispatch + simulate agent work ---
	dispatcher := engine.NewDispatcher(cfg, es, ps)
	assignments, err := dispatcher.DispatchWave(planResult.Graph, map[string]bool{}, "r-fb-1", planResult.Stories, 0)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	storyID := assignments[0].StoryID

	for _, evt := range []state.Event{
		state.NewEvent(state.EventStoryStarted, assignments[0].AgentID, storyID, nil),
		state.NewEvent(state.EventStoryCompleted, assignments[0].AgentID, storyID, nil),
	} {
		es.Append(evt)
		ps.Project(evt)
	}

	// --- Phase 3: Review (Google fails with 429, falls back to Anthropic) ---
	reviewer := engine.NewReviewer(fallbackClient, "gemma-4-27b-it", 4000, es, ps)
	reviewResult, err := reviewer.Review(
		context.Background(), storyID,
		"Build feature", "Feature works",
		"diff --git a/feature.go\n+func Feature() {}",
	)
	if err != nil {
		t.Fatalf("review should have succeeded via fallback, got: %v", err)
	}
	if !reviewResult.Passed {
		t.Fatal("expected review to pass via Anthropic fallback")
	}

	// --- Verify fallback occurred ---
	// Google was called twice: once for planning (success), once for review (429)
	if atomic.LoadInt32(&googleCalls) != 2 {
		t.Fatalf("expected 2 Google calls (plan + failed review), got %d", googleCalls)
	}
	// Anthropic was called once: for the review fallback
	if atomic.LoadInt32(&anthropicCalls) != 1 {
		t.Fatalf("expected 1 Anthropic fallback call, got %d", anthropicCalls)
	}

	// Verify review result came from Anthropic
	if reviewResult.Summary != "Looks good — verified via Anthropic fallback" {
		t.Fatalf("expected Anthropic review summary, got %q", reviewResult.Summary)
	}

	story, _ := ps.GetStory(storyID)
	if story.Status != "qa" {
		t.Fatalf("expected 'qa' after fallback review, got %q", story.Status)
	}
}

// TestGoogleAI_ToolCallAdapter_SupervisorReview verifies that when Gemma
// returns structured output using <|tool_call|> tokens, the ToolCallAdapter
// correctly extracts the JSON and the engine's Supervisor.Review() can parse
// it. This tests the full chain: ToolCallAdapter augments the prompt, Gemma
// mock returns tool-call format, adapter strips tokens, Supervisor unmarshals.
func TestGoogleAI_ToolCallAdapter_SupervisorReview(t *testing.T) {
	// --- Setup: Mock Google AI that returns tool-call tokens ---
	toolCallResponse := fmt.Sprintf(
		"<|tool_call|>\nreport_status\n%s\n<|end_tool_call|>",
		`{"on_track":true,"concerns":["Story s-tc01 is taking longer than expected"],"reprioritize":[]}`,
	)

	googleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the system prompt was augmented with tool schema
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)

		sysInstr, ok := reqBody["system_instruction"].(map[string]any)
		if !ok {
			t.Error("expected system_instruction in request")
		} else {
			parts := sysInstr["parts"].([]any)
			sysText := parts[0].(map[string]any)["text"].(string)
			if len(sysText) < 50 {
				t.Error("system prompt seems too short — tool schema may not have been injected")
			}
			// Verify tool schema was injected
			if !containsSubstring(sysText, "<tools>") {
				t.Error("expected <tools> block in system prompt")
			}
			if !containsSubstring(sysText, "report_status") {
				t.Error("expected 'report_status' tool name in system prompt")
			}
		}

		resp := googleAIResponse(toolCallResponse)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer googleServer.Close()

	// --- Build client with ToolCallAdapter ---
	schema := llm.ToolSchemaFor(
		// We need to import agent for the role constant, but since this is
		// engine_test package, use the string directly via the llm package
		agent.RoleSupervisor,
	)
	googleClient := llm.NewToolCallAdapter(
		llm.NewGoogleAIClient("test-key").WithBaseURL(googleServer.URL),
		schema,
	)

	// --- Setup: Stores ---
	es, _, cleanup := newIntegrationStores(t)
	defer cleanup()

	// --- Run Supervisor Review ---
	supervisor := engine.NewSupervisor(googleClient, "gemma-4-27b-it", 4000, es)

	stories := []engine.PlannedStory{
		{ID: "r-tc---s-tc01", Title: "Task one", Complexity: 3},
		{ID: "r-tc---s-tc02", Title: "Task two", Complexity: 2},
	}
	statuses := map[string]string{
		"r-tc---s-tc01": "in_progress",
		"r-tc---s-tc02": "completed",
	}

	result, err := supervisor.Review(context.Background(), "Build something cool", stories, statuses)
	if err != nil {
		t.Fatalf("supervisor review: %v", err)
	}

	// Verify the tool-call response was correctly parsed
	if !result.OnTrack {
		t.Error("expected on_track=true")
	}
	if len(result.Concerns) != 1 {
		t.Fatalf("expected 1 concern, got %d", len(result.Concerns))
	}
	if result.Concerns[0] != "Story s-tc01 is taking longer than expected" {
		t.Errorf("unexpected concern: %q", result.Concerns[0])
	}
	if len(result.Reprioritize) != 0 {
		t.Errorf("expected empty reprioritize, got %v", result.Reprioritize)
	}

	// Verify the supervisor emitted the right event
	events, _ := es.List(state.EventFilter{Type: state.EventSupervisorCheck})
	if len(events) != 1 {
		t.Fatalf("expected 1 SUPERVISOR_CHECK event, got %d", len(events))
	}
}

// TestGoogleAI_ToolCallAdapter_GracefulDegradation verifies that when Gemma
// returns free-text JSON instead of tool-call tokens (e.g., older model version
// or complex prompt that bypasses tool-calling), the ToolCallAdapter passes
// through and the engine's existing extractJSON still works.
func TestGoogleAI_ToolCallAdapter_GracefulDegradation(t *testing.T) {
	// Gemma returns plain JSON without tool-call tokens
	plainJSON := `{"on_track":false,"concerns":["drift detected","blocked on dependency"],"reprioritize":["s-deg02"]}`

	googleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := googleAIResponse(plainJSON)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer googleServer.Close()

	schema := llm.ToolSchemaFor(agent.RoleSupervisor)
	googleClient := llm.NewToolCallAdapter(
		llm.NewGoogleAIClient("test-key").WithBaseURL(googleServer.URL),
		schema,
	)

	es, _, cleanup := newIntegrationStores(t)
	defer cleanup()

	supervisor := engine.NewSupervisor(googleClient, "gemma-4-27b-it", 4000, es)

	stories := []engine.PlannedStory{
		{ID: "r-deg--s-deg01", Title: "Task one", Complexity: 3},
		{ID: "r-deg--s-deg02", Title: "Task two", Complexity: 5},
	}
	statuses := map[string]string{
		"r-deg--s-deg01": "completed",
		"r-deg--s-deg02": "in_progress",
	}

	result, err := supervisor.Review(context.Background(), "Build something", stories, statuses)
	if err != nil {
		t.Fatalf("supervisor review with plain JSON: %v", err)
	}

	if result.OnTrack {
		t.Error("expected on_track=false")
	}
	if len(result.Concerns) != 2 {
		t.Fatalf("expected 2 concerns, got %d", len(result.Concerns))
	}
	if len(result.Reprioritize) != 1 || result.Reprioritize[0] != "s-deg02" {
		t.Errorf("expected reprioritize=['s-deg02'], got %v", result.Reprioritize)
	}
}

// containsSubstring checks if substr appears within s.
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
