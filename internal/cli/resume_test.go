package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ---------------------------------------------------------------------------
// runResume — early exit paths (testing without needing real LLM/tmux)
// ---------------------------------------------------------------------------

func TestRunResume_RequirementNotFound(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"NONEXISTENT"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent requirement")
	}
	if !strings.Contains(err.Error(), "requirement not found") {
		t.Errorf("error should mention 'requirement not found': %v", err)
	}
}

func TestRunResume_ReviewAutoConflict(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME1",
		"title": "Resume Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("review", "true")
	cmd.Flags().Set("auto", "true")
	cmd.SetArgs([]string{"REQ-RESUME1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when both --review and --auto set")
	}
	if !strings.Contains(err.Error(), "cannot use both") {
		t.Errorf("error should mention conflict: %v", err)
	}
}

func TestRunResume_NoStories(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME2",
		"title": "Resume No Stories",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	// Approve plan so we pass the plan gate
	planEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-RESUME2",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-RESUME2"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "No stories found") {
		t.Errorf("expected 'No stories found', got: %s", output)
	}
}

func TestRunResume_AllStoriesComplete(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME3",
		"title": "Resume All Complete",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-RESUME3",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	// Create and merge a story
	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-RES3", map[string]any{
		"id":         "STR-RES3",
		"req_id":     "REQ-RESUME3",
		"title":      "Complete Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	mergeEvt := state.NewEvent(state.EventStoryMerged, "", "STR-RES3", nil)
	s.Events.Append(mergeEvt)
	s.Proj.Project(mergeEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-RESUME3"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "All stories are complete") {
		t.Errorf("expected 'All stories are complete', got: %s", output)
	}
}

func TestRunResume_PausedRequirement(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME4",
		"title": "Resume Paused",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventReqPlanned, "", "", map[string]any{
		"id": "REQ-RESUME4",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	pauseEvt := state.NewEvent(state.EventReqPaused, "", "", map[string]any{
		"id": "REQ-RESUME4",
	})
	s.Events.Append(pauseEvt)
	s.Proj.Project(pauseEvt)

	planApproveEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-RESUME4",
	})
	s.Events.Append(planApproveEvt)
	s.Proj.Project(planApproveEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-RESUME4"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Unpaused") {
		t.Errorf("expected 'Unpaused' in output, got: %s", output)
	}
}

func TestRunResume_PlanApprovalRequired(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME5",
		"title": "Plan Required",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	// Set review mode to plan_only so plan approval is required
	modeEvt := state.NewEvent(state.EventReviewModeSet, "system", "", map[string]any{
		"req_id": "REQ-RESUME5",
		"mode":   "plan_only",
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
	cmd.SetArgs([]string{"REQ-RESUME5"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for plan approval requirement")
	}
	if !strings.Contains(err.Error(), "plan approval required") {
		t.Errorf("error should mention plan approval: %v", err)
	}
}

func TestRunResume_WithReviewFlag(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME6",
		"title": "Review Flag",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-RESUME6",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("review", "true")
	cmd.SetArgs([]string{"REQ-RESUME6"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "No stories found") {
		t.Errorf("expected 'No stories found', got: %s", output)
	}
}

func TestRunResume_WithAutoFlag(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME7",
		"title": "Auto Flag",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("auto", "true")
	cmd.SetArgs([]string{"REQ-RESUME7"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "No stories found") {
		t.Errorf("expected 'No stories found', got: %s", output)
	}
}

func TestRunResume_NoDependenciesMet(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-NODEP1",
		"title": "No Deps Met",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-NODEP1",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	// Create story B first (no deps, but will be marked as "pr_submitted")
	storyB := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-NDP-B", map[string]any{
		"id":         "STR-NDP-B",
		"req_id":     "REQ-NODEP1",
		"title":      "Base Story",
		"complexity": 3,
	})
	s.Events.Append(storyB)
	s.Proj.Project(storyB)

	// Create story A that depends on B (B is not yet merged/pr_submitted)
	storyA := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-NDP-A", map[string]any{
		"id":         "STR-NDP-A",
		"req_id":     "REQ-NODEP1",
		"title":      "Dependent Story",
		"complexity": 5,
		"depends_on": []any{"STR-NDP-B"},
	})
	s.Events.Append(storyA)
	s.Proj.Project(storyA)

	// Mark B as pr_submitted (in "completed" set), so only A remains
	// But A depends on B, and B is pr_submitted (completed), so A should
	// actually be ready. Let me instead have 3 stories: C depends on A,
	// and A depends on B, with only B merged. A is ready, C is not.
	// But that will dispatch A which enters the monitor loop.
	//
	// Alternative: create only story A with depends_on B, but B doesn't exist.
	// That should make A not-ready since its dependency is never met.
	// Actually the DAG node needs to exist. Let me just merge B to make A ready,
	// but also create story C that depends on A which is not complete.
	// Then only C remains, with deps not met.

	// Merge B
	mergeEvt := state.NewEvent(state.EventStoryMerged, "", "STR-NDP-B", nil)
	s.Events.Append(mergeEvt)
	s.Proj.Project(mergeEvt)

	// Mark A as merged too
	mergeEvt2 := state.NewEvent(state.EventStoryMerged, "", "STR-NDP-A", nil)
	s.Events.Append(mergeEvt2)
	s.Proj.Project(mergeEvt2)

	// Create story C that depends on a nonexistent story "STR-GHOST"
	storyC := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-NDP-C", map[string]any{
		"id":         "STR-NDP-C",
		"req_id":     "REQ-NODEP1",
		"title":      "Blocked Story",
		"complexity": 3,
		"depends_on": []any{"STR-GHOST"},
	})
	s.Events.Append(storyC)
	s.Proj.Project(storyC)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.SetArgs([]string{"REQ-NODEP1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	output := buf.String()
	if err != nil {
		t.Logf("error (may be expected): %v", err)
	}
	// Should exercise the full path: story loading, DAG rebuild, dispatch wave
	if !strings.Contains(output, "Resuming requirement") {
		t.Errorf("expected 'Resuming requirement', got: %s", output)
	}
	if !strings.Contains(output, "3 total") {
		t.Errorf("expected '3 total', got: %s", output)
	}
}

func TestRunResume_WithForceFlag(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESUME8",
		"title": "Force Flag",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventPlanApproved, "human", "", map[string]any{
		"req_id": "REQ-RESUME8",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newResumeCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.PersistentFlags().Bool("skip-preflight", true, "")
	cmd.Flags().Set("force", "true")
	cmd.SetArgs([]string{"REQ-RESUME8"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
