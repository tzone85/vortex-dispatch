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
// approveStory — test story lookup, status validation, event emission
// ---------------------------------------------------------------------------

func TestApproveStory_StoryNotFound(t *testing.T) {
	dir, s := setupTestEnv(t)
	_ = dir

	cmd := &bytes.Buffer{}
	_ = cmd

	// Use the stores directly — approveStory takes a cobra.Command for output
	fakeCmd := makeCmdWithStores(t, dir)
	var buf bytes.Buffer
	fakeCmd.SetOut(&buf)

	err := approveStory(fakeCmd, s, "NONEXISTENT-STORY")
	if err == nil {
		t.Fatal("expected error for nonexistent story")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found': %v", err)
	}
}

func TestApproveStory_WrongStatus_Draft(t *testing.T) {
	_, s := setupTestEnv(t)

	// Create a story in draft status
	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-APP1", map[string]any{
		"id":         "STR-APP1",
		"req_id":     "REQ-APP",
		"title":      "Approve Test Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	fakeCmd := &bytes.Buffer{}
	cmdObj := makeCmdWithStores(t, "")
	cmdObj.SetOut(fakeCmd)

	err := approveStory(cmdObj, s, "STR-APP1")
	if err == nil {
		t.Fatal("expected error for story in draft status")
	}
	if !strings.Contains(err.Error(), "not awaiting_approval") {
		t.Errorf("error should mention 'not awaiting_approval': %v", err)
	}
}

func TestApproveStory_AwaitingApproval_MergeFails(t *testing.T) {
	_, s := setupTestEnv(t)

	// Create req and story
	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-APP2",
		"title": "Approve Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-APP2", map[string]any{
		"id":         "STR-APP2",
		"req_id":     "REQ-APP2",
		"title":      "Story For Approval",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Set to awaiting_approval
	awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "", "STR-APP2", nil)
	s.Events.Append(awaitEvt)
	s.Proj.Project(awaitEvt)

	cmdObj := makeCmdWithStores(t, "")
	var buf bytes.Buffer
	cmdObj.SetOut(&buf)

	// approveStory will emit EventStoryApproved then try MergeExistingPR
	// which will fail (no real git repo/PR), but the function should not error —
	// it prints a message and returns nil
	err := approveStory(cmdObj, s, "STR-APP2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should mention approval and suggest manual merge
	if !strings.Contains(output, "Approved") {
		t.Errorf("expected 'Approved' in output, got: %s", output)
	}

	// Verify event was emitted
	events, _ := s.Events.List(state.EventFilter{Type: state.EventStoryApproved})
	if len(events) == 0 {
		t.Error("expected EventStoryApproved event to be emitted")
	}
}

func TestApproveStory_WrongStatus_InProgress(t *testing.T) {
	_, s := setupTestEnv(t)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-APP3", map[string]any{
		"id":         "STR-APP3",
		"req_id":     "REQ-APP",
		"title":      "In Progress Story",
		"complexity": 5,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	startEvt := state.NewEvent(state.EventStoryStarted, "jr-1", "STR-APP3", nil)
	s.Events.Append(startEvt)
	s.Proj.Project(startEvt)

	cmdObj := makeCmdWithStores(t, "")
	var buf bytes.Buffer
	cmdObj.SetOut(&buf)

	err := approveStory(cmdObj, s, "STR-APP3")
	if err == nil {
		t.Fatal("expected error for in_progress story")
	}
	if !strings.Contains(err.Error(), "not awaiting_approval") {
		t.Errorf("error should mention 'not awaiting_approval': %v", err)
	}
}

// ---------------------------------------------------------------------------
// approveAll — test batch approval logic
// ---------------------------------------------------------------------------

func TestApproveAll_NoPendingStories(t *testing.T) {
	_, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-ALL1",
		"title": "Approve All Test",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	cmdObj := makeCmdWithStores(t, "")
	var buf bytes.Buffer
	cmdObj.SetOut(&buf)

	err := approveAll(cmdObj, s, "REQ-ALL1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No stories awaiting approval") {
		t.Errorf("expected 'No stories awaiting approval', got: %s", buf.String())
	}
}

func TestApproveAll_WithPendingStories(t *testing.T) {
	_, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-ALL2",
		"title": "Approve All Test 2",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	// Create two stories and set them to awaiting_approval
	for _, sid := range []string{"STR-ALL2A", "STR-ALL2B"} {
		storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", sid, map[string]any{
			"id":         sid,
			"req_id":     "REQ-ALL2",
			"title":      "Story " + sid,
			"complexity": 3,
		})
		s.Events.Append(storyEvt)
		s.Proj.Project(storyEvt)

		awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "", sid, nil)
		s.Events.Append(awaitEvt)
		s.Proj.Project(awaitEvt)
	}

	cmdObj := makeCmdWithStores(t, "")
	var buf bytes.Buffer
	cmdObj.SetOut(&buf)

	err := approveAll(cmdObj, s, "REQ-ALL2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Approving 2 stories") {
		t.Errorf("expected 'Approving 2 stories', got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// runApprove — integration test for the full command
// ---------------------------------------------------------------------------

func TestRunApprove_StoryNotFound2(t *testing.T) {
	dir, _ := setupTestEnv(t)

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newApproveCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"TOTALLY-NONEXISTENT"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent story")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found': %v", err)
	}
}

func TestRunApprove_AwaitingApproval_FullFlow(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-APPF",
		"title": "Full Approve Flow",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-APPF1", map[string]any{
		"id":         "STR-APPF1",
		"req_id":     "REQ-APPF",
		"title":      "Full Approval Story",
		"complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	awaitEvt := state.NewEvent(state.EventStoryAwaitingApproval, "", "STR-APPF1", nil)
	s.Events.Append(awaitEvt)
	s.Proj.Project(awaitEvt)

	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newApproveCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-APPF1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Approved") {
		t.Errorf("expected 'Approved' in output, got: %s", output)
	}
}
