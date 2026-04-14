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
// showRequirementStatus — test with stories that have branches, PR info,
// escalation tiers, agents, and various statuses
// ---------------------------------------------------------------------------

func TestShowRequirementStatus_WithFullStoryDetails(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-STAT1",
		"title": "Status Test Full",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	// Story 1: assigned, in progress
	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ST1", map[string]any{
		"id":         "STR-ST1",
		"req_id":     "REQ-STAT1",
		"title":      "In Progress Story",
		"complexity": 5,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	assignEvt := state.NewEvent(state.EventStoryAssigned, "", "STR-ST1", map[string]any{
		"agent_id": "jr-42",
		"branch":   "vxd/STR-ST1",
	})
	s.Events.Append(assignEvt)
	s.Proj.Project(assignEvt)

	startEvt := state.NewEvent(state.EventStoryStarted, "jr-42", "STR-ST1", nil)
	s.Events.Append(startEvt)
	s.Proj.Project(startEvt)

	// Story 2: merged with PR
	storyEvt2 := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ST2", map[string]any{
		"id":         "STR-ST2",
		"req_id":     "REQ-STAT1",
		"title":      "Merged Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt2)
	s.Proj.Project(storyEvt2)

	prEvt := state.NewEvent(state.EventStoryPRCreated, "", "STR-ST2", map[string]any{
		"pr_url":    "https://github.com/org/repo/pull/10",
		"pr_number": 10,
	})
	s.Events.Append(prEvt)
	s.Proj.Project(prEvt)

	mergeEvt := state.NewEvent(state.EventStoryMerged, "", "STR-ST2", nil)
	s.Events.Append(mergeEvt)
	s.Proj.Project(mergeEvt)

	// Story 3: escalated
	storyEvt3 := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ST3", map[string]any{
		"id":         "STR-ST3",
		"req_id":     "REQ-STAT1",
		"title":      "Escalated Story",
		"complexity": 8,
	})
	s.Events.Append(storyEvt3)
	s.Proj.Project(storyEvt3)

	escEvt := state.NewEvent(state.EventStoryEscalated, "monitor", "STR-ST3", map[string]any{
		"from_tier": 0,
		"to_tier":   2,
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
	cmd.Flags().Set("req", "REQ-STAT1")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()

	// Check story titles
	if !strings.Contains(output, "In Progress Story") {
		t.Errorf("expected 'In Progress Story' in output, got: %s", output)
	}
	if !strings.Contains(output, "Merged Story") {
		t.Errorf("expected 'Merged Story' in output, got: %s", output)
	}
	if !strings.Contains(output, "Escalated Story") {
		t.Errorf("expected 'Escalated Story' in output, got: %s", output)
	}

	// Note: Branch column is not SET by SQLite projection (EventStoryAssigned
	// doesn't update the branch column), so branch info won't appear.

	// Check PR info
	if !strings.Contains(output, "#10") {
		t.Errorf("expected PR number in output, got: %s", output)
	}

	// Check agent assignment
	if !strings.Contains(output, "jr-42") {
		t.Errorf("expected agent ID in output, got: %s", output)
	}

	// Check summary
	if !strings.Contains(output, "Summary:") {
		t.Errorf("expected 'Summary:' in output, got: %s", output)
	}
}

func TestShowRequirementStatus_NoStories(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-STAT2",
		"title": "Empty Status",
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
	cmd.Flags().Set("req", "REQ-STAT2")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "No stories yet") {
		t.Errorf("expected 'No stories yet', got: %s", output)
	}
}

func TestShowRequirementStatus_UnassignedStory(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-STAT3",
		"title": "Unassigned Status",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-ST4", map[string]any{
		"id":         "STR-ST4",
		"req_id":     "REQ-STAT3",
		"title":      "Unassigned Story",
		"complexity": 2,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newStatusCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("req", "REQ-STAT3")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "unassigned") {
		t.Errorf("expected 'unassigned' in output, got: %s", output)
	}
}

func TestShowRequirementStatus_ReqNotFound(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newStatusCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("req", "NONEXISTENT")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent requirement")
	}
}

// ---------------------------------------------------------------------------
// runStatus — multiple requirements
// ---------------------------------------------------------------------------

func TestRunStatus_MultipleRequirements(t *testing.T) {
	dir, s := setupTestEnv(t)

	// Create two requirements (IDs must be >= 8 chars due to status.go:69 [:8] slice)
	for _, id := range []string{"REQ-MULT1", "REQ-MULT2"} {
		reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
			"id":    id,
			"title": "Multi Req " + id,
		})
		s.Events.Append(reqEvt)
		s.Proj.Project(reqEvt)

		storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "S-"+id, map[string]any{
			"id":         "S-" + id,
			"req_id":     id,
			"title":      "Story for " + id,
			"complexity": 3,
		})
		s.Events.Append(storyEvt)
		s.Proj.Project(storyEvt)
	}

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newStatusCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("all", "true")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Requirements:") {
		t.Errorf("expected 'Requirements:', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runStatus with PR URL but no PR number (tests the PRUrl-only branch)
// ---------------------------------------------------------------------------

func TestShowRequirementStatus_PRUrlOnly(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-PRURL",
		"title": "PR URL Only",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-PRURL", map[string]any{
		"id":         "STR-PRURL",
		"req_id":     "REQ-PRURL",
		"title":      "PR URL Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Create PR event — set pr_url but not pr_number (or 0)
	prEvt := state.NewEvent(state.EventStoryPRCreated, "", "STR-PRURL", map[string]any{
		"pr_url": "https://github.com/org/repo/pull/99",
	})
	s.Events.Append(prEvt)
	s.Proj.Project(prEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newStatusCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.Flags().Set("req", "REQ-PRURL")

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "PR:") {
		t.Errorf("expected PR info in output, got: %s", output)
	}
}
