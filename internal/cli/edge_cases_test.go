package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// ---------------------------------------------------------------------------
// resolveRequirement — stdin path (test by providing a file as "-")
// ---------------------------------------------------------------------------

// TestResolveRequirement_Stdin can't be easily tested without a real stdin pipe,
// but we can test that file="-" attempts to read stdin (which will be empty).
func TestResolveRequirement_Stdin_Empty(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringP("file", "f", "", "")
	cmd.Flags().Set("file", "-")

	// In test, stdin is typically connected to /dev/null or closed
	// This should fail with "stdin was empty"
	_, err := resolveRequirement(cmd, nil)
	if err == nil {
		// In some environments, stdin may have data from test runner
		t.Log("stdin not empty — skipping stdin test")
		return
	}
	if !strings.Contains(err.Error(), "stdin") && !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention stdin or empty: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runReviewStory — with agent assigned (covers the agent display branch)
// ---------------------------------------------------------------------------

func TestRunReviewStory_WithAgent(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-REVAG",
		"title": "Review With Agent",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-REVAG", map[string]any{
		"id":         "STR-REVAG",
		"req_id":     "REQ-REVAG",
		"title":      "Agent Review Story",
		"complexity": 5,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	assignEvt := state.NewEvent(state.EventStoryAssigned, "", "STR-REVAG", map[string]any{
		"agent_id": "junior-123",
	})
	s.Events.Append(assignEvt)
	s.Proj.Project(assignEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReviewCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-REVAG"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Agent:") {
		t.Errorf("expected 'Agent:' in output, got: %s", output)
	}
	if !strings.Contains(output, "junior-123") {
		t.Errorf("expected agent ID in output, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runPause — success with in_progress status
// ---------------------------------------------------------------------------

func TestRunPause_InProgressSuccess(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-PIP",
		"title": "In Progress Pause",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	planEvt := state.NewEvent(state.EventReqPlanned, "", "", map[string]any{
		"id": "REQ-PIP",
	})
	s.Events.Append(planEvt)
	s.Proj.Project(planEvt)

	// Create story and start it to put req in in_progress state
	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-PIP", map[string]any{
		"id":         "STR-PIP",
		"req_id":     "REQ-PIP",
		"title":      "IP Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	startEvt := state.NewEvent(state.EventStoryStarted, "jr-1", "STR-PIP", nil)
	s.Events.Append(startEvt)
	s.Proj.Project(startEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newPauseCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"REQ-PIP"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Paused requirement") {
		t.Errorf("expected 'Paused requirement', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runReport — internal mode
// ---------------------------------------------------------------------------

func TestRunReport_Internal(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-INT",
		"title": "Internal Report",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-INT", map[string]any{
		"id":         "STR-INT",
		"req_id":     "REQ-INT",
		"title":      "Internal Story",
		"complexity": 5,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReportCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("internal", "true")
	cmd.SetArgs([]string{"REQ-INT"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if output == "" {
		t.Error("expected internal report output")
	}
}

// ---------------------------------------------------------------------------
// runEstimate — non-quick mode error (no LLM available)
// ---------------------------------------------------------------------------

func TestRunEstimate_NonQuick_NoLLM(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("PATH", "/nonexistent")

	cmd := newEstimateCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"Build an API"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no LLM available for non-quick estimate")
	}
	if !strings.Contains(err.Error(), "LLM") && !strings.Contains(err.Error(), "client") {
		t.Errorf("error should mention LLM: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runGC — real GC (not dry-run) with no eligible branches
// ---------------------------------------------------------------------------

func TestRunGC_RealGC_NoEligibleBranches(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-GCR",
		"title": "GC Real Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "GCS-REAL", map[string]any{
		"id":         "GCS-REAL",
		"req_id":     "REQ-GCR",
		"title":      "GC Real Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	mergeEvt := state.NewEvent(state.EventStoryMerged, "", "GCS-REAL", nil)
	s.Events.Append(mergeEvt)
	s.Proj.Project(mergeEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	// Not dry-run — runs actual GC
	cmd := newGCCmd()
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
	if !strings.Contains(output, "No branches eligible") && !strings.Contains(output, "Cleaned up") {
		t.Errorf("expected cleanup message, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// showRequirementStatus with escalation tier label
// ---------------------------------------------------------------------------

func TestShowRequirementStatus_EscalationTierLabel(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-ETLBL",
		"title": "Tier Label Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ETLBL", map[string]any{
		"id":         "STR-ETLBL",
		"req_id":     "REQ-ETLBL",
		"title":      "Tier Label Story",
		"complexity": 5,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	escEvt := state.NewEvent(state.EventStoryEscalated, "monitor", "STR-ETLBL", map[string]any{
		"from_tier": 0,
		"to_tier":   3,
	})
	s.Events.Append(escEvt)
	s.Proj.Project(escEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newStatusCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("req", "REQ-ETLBL")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "tier:3") {
		t.Errorf("expected 'tier:3' in status label, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runStatus without --all flag (filters by repo path)
// ---------------------------------------------------------------------------

func TestRunStatus_FilterByRepoPath(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-FILT1",
		"title": "Filtered Req",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newStatusCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	// No --all flag, so it filters by cwd repo

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Output depends on whether the req has a matching repo_path
	output := buf.String()
	if output == "" {
		t.Error("expected some output")
	}
}

// ---------------------------------------------------------------------------
// buildLLMClient — cli provider always succeeds (doesn't check PATH)
// ---------------------------------------------------------------------------

func TestBuildLLMClient_CLIProvider_SkipPerms(t *testing.T) {
	// "cli" and "claude-cli" providers don't check PATH — they always create
	// a ClaudeCLIClient.
	client, err := buildLLMClient("claude-cli", nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestBuildLLMClient_CLIProvider_NoSkipPerms(t *testing.T) {
	client, err := buildLLMClient("cli", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}
