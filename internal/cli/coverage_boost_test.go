package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ---------------------------------------------------------------------------
// runEstimate — quick mode with JSON, rate override, and save flags
// ---------------------------------------------------------------------------

func TestRunEstimate_QuickJSON(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newEstimateCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("quick", "true")
	cmd.Flags().Set("json", "true")
	cmd.SetArgs([]string{"Add a health check endpoint"})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var stdoutBuf bytes.Buffer
	stdoutBuf.ReadFrom(r)
	output := stdoutBuf.String()

	// Should be valid JSON
	var parsed map[string]any
	if jsonErr := json.Unmarshal([]byte(output), &parsed); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput: %s", jsonErr, output)
	}
}

func TestRunEstimate_QuickWithRateOverride(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newEstimateCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("quick", "true")
	cmd.Flags().Set("rate", "200")
	cmd.SetArgs([]string{"Build a REST API"})

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunEstimate_QuickWithSave(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newEstimateCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("quick", "true")
	cmd.Flags().Set("save", "true")
	cmd.SetArgs([]string{"Implement OAuth login"})

	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runArchive — with stories that have branches
// ---------------------------------------------------------------------------

func TestRunArchive_WithStories(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-ARCH2",
		"title": "Archive With Stories",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ARC2", map[string]any{
		"id":         "STR-ARC2",
		"req_id":     "REQ-ARCH2",
		"title":      "Archivable Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	assignEvt := state.NewEvent(state.EventStoryAssigned, "", "STR-ARC2", map[string]any{
		"agent_id": "jr-1",
		"branch":   "vxd/STR-ARC2",
	})
	s.Events.Append(assignEvt)
	s.Proj.Project(assignEvt)

	mergeEvt := state.NewEvent(state.EventStoryMerged, "", "STR-ARC2", nil)
	s.Events.Append(mergeEvt)
	s.Proj.Project(mergeEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newArchiveCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"REQ-ARCH2"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Archived requirement") {
		t.Errorf("expected 'Archived requirement', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runEvents — with type and story filters
// ---------------------------------------------------------------------------

func TestRunEvents_WithTypeFilter(t *testing.T) {
	dir, s := setupTestEnv(t)

	// Seed different event types
	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-EVT",
		"title": "Events Test",
	})
	s.Events.Append(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-EVT", map[string]any{
		"id":         "STR-EVT",
		"req_id":     "REQ-EVT",
		"title":      "Events Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newEventsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("type", "STORY_CREATED")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "STORY_CREATED") {
		t.Errorf("expected 'STORY_CREATED' in output, got: %s", output)
	}
	// Should NOT contain REQ_SUBMITTED (filtered out)
	if strings.Contains(output, "REQ_SUBMITTED") {
		t.Errorf("should not contain REQ_SUBMITTED with type filter, got: %s", output)
	}
}

func TestRunEvents_WithStoryFilter(t *testing.T) {
	dir, s := setupTestEnv(t)

	// Seed events for different stories
	for _, sid := range []string{"STR-EF1", "STR-EF2"} {
		storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", sid, map[string]any{
			"id":         sid,
			"req_id":     "REQ-EVTF",
			"title":      "Story " + sid,
			"complexity": 3,
		})
		s.Events.Append(storyEvt)
	}

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newEventsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("story", "STR-EF1")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "STR-EF1") {
		t.Errorf("expected 'STR-EF1' in output, got: %s", output)
	}
}

func TestRunEvents_WithLimit(t *testing.T) {
	dir, s := setupTestEnv(t)

	// Seed 5 events
	for i := 0; i < 5; i++ {
		sid := "STR-LM" + string(rune('A'+i))
		storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", sid, map[string]any{
			"id":         sid,
			"req_id":     "REQ-LM",
			"title":      "Limit Story " + sid,
			"complexity": 3,
		})
		s.Events.Append(storyEvt)
	}

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newEventsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("limit", "2")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "2 shown of 5 total") {
		t.Errorf("expected '2 shown of 5 total' in output, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runDispatchPreflight — test the warning path (not skipped, reports warnings)
// ---------------------------------------------------------------------------

func TestRunDispatchPreflight_ReportsWarnings(t *testing.T) {
	cmd := newReqCmd()
	cmd.PersistentFlags().Bool("skip-preflight", false, "")

	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	err := runDispatchPreflight(cmd)
	// On dev machines, this may pass with warnings or fail with critical issues
	if err != nil {
		// Error means critical issues found — that's valid behavior
		if !strings.Contains(err.Error(), "critical") {
			t.Errorf("unexpected error type: %v", err)
		}
	}
	// If there are warnings, they should be written to stderr
	// (we can't guarantee warnings in test env)
}

// ---------------------------------------------------------------------------
// printEstimateTable — live (non-quick) path routing
// ---------------------------------------------------------------------------

func TestPrintEstimateTable_LiveRoute(t *testing.T) {
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := printEstimateTable(engine.Estimate{
		IsQuick:     false,
		Requirement: "live",
		Summary:     engine.EstimateSummary{StoryCount: 1, Rate: 150},
		Stories: []engine.StoryEstimate{
			{Title: "S1", Complexity: 3, HoursLow: 1, HoursHigh: 2, Role: "junior"},
		},
	})

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runConsistencyCheck — with in-progress story that has a worktree + checkpoint
// ---------------------------------------------------------------------------

func TestRunConsistencyCheck_WithCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Workspace.StateDir = dir

	// Create worktree dir for the story
	worktreeDir := filepath.Join(dir, "worktrees", "stuck-cp")
	os.MkdirAll(worktreeDir, 0o755)

	// Create checkpoint file
	checkpoint := engine.Checkpoint{
		MergingStory: "stuck-cp",
		Phase:        "merge",
	}
	cpData, _ := json.Marshal(checkpoint)
	cpPath := filepath.Join(dir, "checkpoint.json")
	os.WriteFile(cpPath, cpData, 0644)

	stories := []state.Story{
		{ID: "stuck-cp", Status: "in_progress", AgentID: "a1"},
	}

	// Exercise the checkpoint loading code path — the consistency check
	// may or may not find issues depending on the recovery logic
	issues := runConsistencyCheck(stories, cfg, dir)
	_ = issues // just verify no panic
}

func TestRunConsistencyCheck_MergingStatus(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Workspace.StateDir = dir

	stories := []state.Story{
		{ID: "merging-1", Status: "merging", AgentID: "a1"},
	}

	issues := runConsistencyCheck(stories, cfg, dir)
	// Merging story without worktree should have issues
	if len(issues) == 0 {
		t.Error("expected issues for merging story with no worktree")
	}
}

// ---------------------------------------------------------------------------
// recoverOrphanedStories — with runtime configuration
// ---------------------------------------------------------------------------

func TestRecoverOrphanedStories_WithRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer ps.Close()

	cfg := config.DefaultConfig()
	cfg.Workspace.StateDir = dir
	cfg.Runtimes = map[string]config.RuntimeConfig{
		"tmux": {Command: "tmux"},
	}

	// Create worktree
	worktreeDir := filepath.Join(dir, "worktrees", "orphan-rt")
	os.MkdirAll(worktreeDir, 0o755)

	stories := []state.Story{
		{ID: "orphan-rt", Status: "in_progress", AgentID: "ag-1"},
	}

	orphans := recoverOrphanedStories(stories, ps, cfg)
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}
	if orphans[0].RuntimeName != "tmux" {
		t.Errorf("expected runtime 'tmux', got %q", orphans[0].RuntimeName)
	}
}

func TestRecoverOrphanedStories_DraftStatusSkipped(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer ps.Close()

	cfg := config.DefaultConfig()
	cfg.Workspace.StateDir = dir

	stories := []state.Story{
		{ID: "draft-1", Status: "draft", AgentID: "ag-1"},
		{ID: "merged-1", Status: "merged", AgentID: "ag-2"},
	}

	orphans := recoverOrphanedStories(stories, ps, cfg)
	if len(orphans) != 0 {
		t.Errorf("expected no orphans for non-in_progress stories, got %d", len(orphans))
	}
}

// ---------------------------------------------------------------------------
// rebuildDAG — with dependencies
// ---------------------------------------------------------------------------

func TestRebuildDAG_WithDependencies(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vxd.db")
	ps, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer ps.Close()

	// Seed stories
	for _, sid := range []string{"s-dep1", "s-dep2", "s-dep3"} {
		evt := state.NewEvent(state.EventStoryCreated, "tech-lead", sid, map[string]any{
			"id":         sid,
			"req_id":     "r-dep",
			"title":      "Story " + sid,
			"complexity": 3,
		})
		ps.Project(evt)
	}

	stories := []state.Story{
		{ID: "s-dep1", ReqID: "r-dep", Title: "Story 1", Complexity: 3},
		{ID: "s-dep2", ReqID: "r-dep", Title: "Story 2", Complexity: 5},
		{ID: "s-dep3", ReqID: "r-dep", Title: "Story 3", Complexity: 2},
	}

	dag, planned, err := rebuildDAG(ps, "r-dep", stories)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag == nil {
		t.Fatal("DAG is nil")
	}
	if len(planned) != 3 {
		t.Errorf("expected 3 planned stories, got %d", len(planned))
	}
}

// ---------------------------------------------------------------------------
// ghOpsAdapter — verify interface implementation
// ---------------------------------------------------------------------------

func TestGHOpsAdapter_MethodsExist(t *testing.T) {
	adapter := &ghOpsAdapter{}
	// Compile-time check that methods exist
	var _ func(string, string) error = adapter.PushBranch
	var _ func(string, string, string, string, string) (engine.PRCreationResult, error) = adapter.CreatePR
	var _ func(string, int) error = adapter.MergePR
}

// ---------------------------------------------------------------------------
// runMetrics — with seeded data
// ---------------------------------------------------------------------------

func TestRunMetrics_WithData(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-METRICS",
		"title": "Metrics Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-MET1", map[string]any{
		"id":         "STR-MET1",
		"req_id":     "REQ-METRICS",
		"title":      "Metrics Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	mergeEvt := state.NewEvent(state.EventStoryMerged, "", "STR-MET1", nil)
	s.Events.Append(mergeEvt)
	s.Proj.Project(mergeEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newMetricsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Error("expected metrics output")
	}
}

func TestRunMetrics_WithLimit(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newMetricsCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("limit", "5")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// loadConfig edge cases
// ---------------------------------------------------------------------------

func TestLoadConfig_HomeNotAvailable(t *testing.T) {
	// When HOME can't be determined but repo config exists
	dir := t.TempDir()
	cfgContent := `workspace:
  state_dir: "~/.vxd"
`
	cfgPath := filepath.Join(dir, "vxd.yaml")
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workspace.StateDir == "" {
		t.Error("expected non-empty state dir")
	}
}

// ---------------------------------------------------------------------------
// resolveProject — git detection path
// ---------------------------------------------------------------------------

func TestResolveProject_GitDetection(t *testing.T) {
	dir, _ := setupTestEnv(t)
	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })

	cmd := makeCmdWithStores(t, dir)
	// Clear the project flag and env to force git detection
	cmd.Flags().Set("project", "")
	t.Setenv("VXD_PROJECT", "")

	name, err := resolveProject(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name == "" {
		t.Error("expected non-empty project name from git detection")
	}
}
