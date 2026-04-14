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
// runReject — success path and edge cases
// ---------------------------------------------------------------------------

func TestRunReject_AwaitingApproval_Success(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RJS",
		"title": "Reject Success",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-RJS1", map[string]any{
		"id":         "STR-RJS1",
		"req_id":     "REQ-RJS",
		"title":      "Reject Me Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "", "STR-RJS1", nil)
	s.Events.Append(awaitEvt)
	s.Proj.Project(awaitEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newRejectCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-RJS1", "needs more tests"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Rejected") {
		t.Errorf("expected 'Rejected' in output, got: %s", output)
	}
	if !strings.Contains(output, "needs more tests") {
		t.Errorf("expected feedback in output, got: %s", output)
	}
}

func TestRunReject_MergedStatus(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RJM",
		"title": "Reject Merged",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-RJM1", map[string]any{
		"id":         "STR-RJM1",
		"req_id":     "REQ-RJM",
		"title":      "Already Merged Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	mergeEvt := state.NewEvent(state.EventStoryMerged, "", "STR-RJM1", nil)
	s.Events.Append(mergeEvt)
	s.Proj.Project(mergeEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newRejectCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-RJM1", "too late"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for merged story")
	}
	if !strings.Contains(err.Error(), "not awaiting_approval") {
		t.Errorf("error should mention status: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runRejectPlan — success path
// ---------------------------------------------------------------------------

func TestRunRejectPlan_SuccessWithFeedback(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RPL",
		"title": "Reject Plan Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newRejectPlanCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"REQ-RPL", "needs smaller stories"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Plan rejected") {
		t.Errorf("expected 'Plan rejected', got: %s", output)
	}
	if !strings.Contains(output, "needs smaller stories") {
		t.Errorf("expected feedback in output, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runReviewStory — various story states
// ---------------------------------------------------------------------------

func TestRunReviewStory_FullStoryDetails(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-REV",
		"title": "Review Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-REV1", map[string]any{
		"id":         "STR-REV1",
		"req_id":     "REQ-REV",
		"title":      "Review Story Full",
		"complexity": 5,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Assign to agent with branch
	assignEvt := state.NewEvent(state.EventStoryAssigned, "", "STR-REV1", map[string]any{
		"agent_id": "jr-50",
		"branch":   "vxd/STR-REV1",
	})
	s.Events.Append(assignEvt)
	s.Proj.Project(assignEvt)

	// Set to awaiting_approval
	awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "", "STR-REV1", nil)
	s.Events.Append(awaitEvt)
	s.Proj.Project(awaitEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReviewCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-REV1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Review Story Full") {
		t.Errorf("expected story title in output, got: %s", output)
	}
	if !strings.Contains(output, "STR-REV1") {
		t.Errorf("expected story ID in output, got: %s", output)
	}
	if !strings.Contains(output, "vxd approve STR-REV1") {
		t.Errorf("expected approve hint in output, got: %s", output)
	}
	if !strings.Contains(output, "vxd reject STR-REV1") {
		t.Errorf("expected reject hint in output, got: %s", output)
	}
}

func TestRunReviewStory_DraftStatus_NoApproveHint(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-REV2",
		"title": "Review Test 2",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-REV2", map[string]any{
		"id":         "STR-REV2",
		"req_id":     "REQ-REV2",
		"title":      "Draft Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReviewCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-REV2"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	// Draft status should NOT show approve/reject hints
	if strings.Contains(output, "vxd approve") {
		t.Errorf("draft story should not show approve hint, got: %s", output)
	}
}

func TestRunReviewStory_WithPRInfo(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-REVPR",
		"title": "Review PR Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-REVPR", map[string]any{
		"id":         "STR-REVPR",
		"req_id":     "REQ-REVPR",
		"title":      "PR Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Create PR
	prEvt := state.NewEvent(state.EventStoryPRCreated, "", "STR-REVPR", map[string]any{
		"pr_url":    "https://github.com/org/repo/pull/42",
		"pr_number": 42,
	})
	s.Events.Append(prEvt)
	s.Proj.Project(prEvt)

	// Set to awaiting_approval
	awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "", "STR-REVPR", nil)
	s.Events.Append(awaitEvt)
	s.Proj.Project(awaitEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newReviewCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-REVPR"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "#42") {
		t.Errorf("expected PR number in output, got: %s", output)
	}
	if !strings.Contains(output, "vxd review STR-REVPR --open") {
		t.Errorf("expected open PR hint, got: %s", output)
	}
}

func TestRunReviewStory_WithEscalationTier(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RESC",
		"title": "Review Escalated",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-RESC1", map[string]any{
		"id":         "STR-RESC1",
		"req_id":     "REQ-RESC",
		"title":      "Escalated Story",
		"complexity": 8,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Escalate
	escEvt := state.NewEvent(state.EventStoryEscalated, "monitor", "STR-RESC1", map[string]any{
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

	cmd := newReviewCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-RESC1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Escalation Tier: 2") {
		t.Errorf("expected escalation tier in output, got: %s", output)
	}
}
