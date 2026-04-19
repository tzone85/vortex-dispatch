package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ---------------------------------------------------------------------------
// runResume — deeper coverage of paths beyond early exits
// ---------------------------------------------------------------------------

func TestRunResume_SplitAndPRSubmittedCountAsComplete(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-COMPL1",
		"title": "Complete Variants",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-COMPL1",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	// Create two stories: one "pr_submitted", one "split"
	for _, sid := range []string{"STR-PR1", "STR-SPLIT1"} {
		e := state.NewEvent(state.EventStoryCreated, "tech-lead", sid, map[string]any{
			"id":         sid,
			"req_id":     "REQ-COMPL1",
			"title":      "Story " + sid,
			"complexity": 3,
		})
		s.Events.Append(e)
		s.Proj.Project(e)
	}

	// Mark STR-PR1 as pr_submitted
	prEvt := state.NewEvent(state.EventStoryPRCreated, "", "STR-PR1", map[string]any{
		"pr_url":    "https://github.com/test/pr/1",
		"pr_number": 1,
	})
	s.Events.Append(prEvt)
	s.Proj.Project(prEvt)

	// Mark STR-SPLIT1 as split
	splitEvt := state.NewEvent(state.EventStorySplit, "", "STR-SPLIT1", nil)
	s.Events.Append(splitEvt)
	s.Proj.Project(splitEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-COMPL1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "All stories are complete") {
		t.Errorf("expected all stories complete when pr_submitted + split, got: %s", output)
	}
	if !strings.Contains(output, "2 completed") {
		t.Errorf("expected '2 completed' in output, got: %s", output)
	}
}

func TestRunResume_ManualReviewModeRequiresPlanApproval(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-MANUAL1",
		"title": "Manual Review Mode",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	// Set review mode to manual via event (not config — config is created fresh by loadStores)
	modeEvt := state.NewEvent(state.EventReviewModeSet, "system", "", map[string]any{
		"req_id": "REQ-MANUAL1",
		"mode":   "manual",
	})
	s.Events.Append(modeEvt)
	s.Proj.Project(modeEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-MANUAL1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for plan approval required in manual mode")
	}
	if !strings.Contains(err.Error(), "plan approval required") {
		t.Errorf("expected plan approval error, got: %v", err)
	}
}

func TestRunResume_AllStoriesMergedOrSplit(t *testing.T) {
	// Test that all three "completed" statuses are correctly counted
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-WAVE1",
		"title": "Wave Tracking",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-WAVE1",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	// Create 3 stories: one merged, one pr_submitted, one split
	for _, sid := range []string{"STR-W1", "STR-W2", "STR-W3"} {
		e := state.NewEvent(state.EventStoryCreated, "tech-lead", sid, map[string]any{
			"id": sid, "req_id": "REQ-WAVE1", "title": "Story " + sid, "complexity": 3,
		})
		s.Events.Append(e)
		s.Proj.Project(e)
	}

	// Assign STR-W1 at wave 2
	assignEvt := state.NewEvent(state.EventStoryAssigned, "", "STR-W1", map[string]any{"wave": 2})
	s.Events.Append(assignEvt)
	s.Proj.Project(assignEvt)

	s.Events.Append(state.NewEvent(state.EventStoryMerged, "", "STR-W1", nil))
	s.Proj.Project(state.NewEvent(state.EventStoryMerged, "", "STR-W1", nil))

	prEvt := state.NewEvent(state.EventStoryPRCreated, "", "STR-W2", map[string]any{"pr_url": "https://test/1", "pr_number": 1})
	s.Events.Append(prEvt)
	s.Proj.Project(prEvt)

	splitEvt := state.NewEvent(state.EventStorySplit, "", "STR-W3", nil)
	s.Events.Append(splitEvt)
	s.Proj.Project(splitEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-WAVE1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "3 completed") {
		t.Errorf("expected '3 completed', got: %s", output)
	}
	if !strings.Contains(output, "All stories are complete") {
		t.Errorf("expected 'All stories are complete', got: %s", output)
	}
}

func TestRecoverOrphanedStories_WithAgentRecord(t *testing.T) {
	dir, s := setupTestEnv(t)

	// Create a story marked in_progress
	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ORPHAN1", map[string]any{
		"id":         "STR-ORPHAN1",
		"req_id":     "REQ-ORPHAN",
		"title":      "Orphaned",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Assign agent
	assignEvt := state.NewEvent(state.EventStoryAssigned, "agent-123", "STR-ORPHAN1", map[string]any{
		"agent_id":     "agent-123",
		"session_name": "vxd-custom-session",
		"runtime":      "docker",
		"branch":       "vxd/STR-ORPHAN1",
	})
	s.Events.Append(assignEvt)
	s.Proj.Project(assignEvt)

	// Transition to in_progress via EventStoryStarted
	startEvt := state.NewEvent(state.EventStoryStarted, "agent-123", "STR-ORPHAN1", nil)
	s.Events.Append(startEvt)
	s.Proj.Project(startEvt)

	// Create the worktree directory so it's found
	worktreeBase := filepath.Join(dir, ".vxd", "worktrees")
	worktreePath := filepath.Join(worktreeBase, "STR-ORPHAN1")
	os.MkdirAll(worktreePath, 0o755)

	stories, _ := s.Proj.ListStories(state.StoryFilter{})
	cfg := config.DefaultConfig()
	cfg.Workspace.StateDir = filepath.Join(dir, ".vxd")
	cfg.Runtimes = map[string]config.RuntimeConfig{
		"claude": {Command: "claude", Args: []string{"-p"}},
	}

	orphans := recoverOrphanedStories(stories, s.Proj, cfg)
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}

	// Should use the agent's session name (vxd-custom-session) from the agent record
	if orphans[0].Assignment.StoryID != "STR-ORPHAN1" {
		t.Errorf("expected story STR-ORPHAN1, got %s", orphans[0].Assignment.StoryID)
	}
	if orphans[0].WorktreePath != worktreePath {
		t.Errorf("expected worktree path %s, got %s", worktreePath, orphans[0].WorktreePath)
	}
}

func TestRecoverOrphanedStories_NoRuntimeConfig(t *testing.T) {
	dir, s := setupTestEnv(t)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ORP2", map[string]any{
		"id":         "STR-ORP2",
		"req_id":     "REQ-ORP2",
		"title":      "No Runtime",
		"complexity": 2,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	assignEvt := state.NewEvent(state.EventStoryAssigned, "agent-456", "STR-ORP2", map[string]any{})
	s.Events.Append(assignEvt)
	s.Proj.Project(assignEvt)

	startEvt := state.NewEvent(state.EventStoryStarted, "agent-456", "STR-ORP2", nil)
	s.Events.Append(startEvt)
	s.Proj.Project(startEvt)

	worktreeBase := filepath.Join(dir, ".vxd", "worktrees")
	os.MkdirAll(filepath.Join(worktreeBase, "STR-ORP2"), 0o755)

	stories, _ := s.Proj.ListStories(state.StoryFilter{})
	cfg := config.DefaultConfig()
	cfg.Workspace.StateDir = filepath.Join(dir, ".vxd")
	cfg.Runtimes = nil // No runtimes configured

	orphans := recoverOrphanedStories(stories, s.Proj, cfg)
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}
	// RuntimeName should be empty since no runtimes configured
	if orphans[0].RuntimeName != "" {
		t.Errorf("expected empty runtime name, got %s", orphans[0].RuntimeName)
	}
}

func TestRunConsistencyCheck_WithRecoveryStories(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".vxd")
	os.MkdirAll(stateDir, 0o755)
	os.MkdirAll(filepath.Join(stateDir, "worktrees"), 0o755)

	stories := []state.Story{
		{ID: "S1", Status: "in_progress", AgentID: "agent-1"},
		{ID: "S2", Status: "merging", AgentID: "agent-2"},
		{ID: "S3", Status: "draft"},      // should be skipped
		{ID: "S4", Status: "merged"},      // should be skipped
	}

	cfg := config.DefaultConfig()
	issues := runConsistencyCheck(stories, cfg, stateDir)
	// Both in_progress and merging stories should be checked
	// Without worktrees or tmux sessions, they should be flagged
	if len(issues) < 1 {
		t.Errorf("expected at least 1 recovery issue for orphaned in_progress story, got %d", len(issues))
	}
}

func TestRebuildDAG_EmptyStories(t *testing.T) {
	dir, s := setupTestEnv(t)
	defer s.Close()

	dag, planned, err := rebuildDAG(s.Proj, "REQ-EMPTY", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag == nil {
		t.Fatal("expected non-nil DAG")
	}
	if len(planned) != 0 {
		t.Errorf("expected 0 planned stories, got %d", len(planned))
	}
	_ = dir
}
