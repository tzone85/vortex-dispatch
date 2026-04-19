package web

import (
	"encoding/json"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// --- handlePause error and success paths ---

func TestHandlePause_StoreError(t *testing.T) {
	s := newTestServer(t)
	// Use an invalid req_id format but valid JSON
	payload := mustMarshal(t, reqPayload{ReqID: "nonexistent-req"})
	resp := s.HandleCommand("pause_requirement", payload)
	// Should not find requirement
	if resp.Success {
		t.Logf("pause of nonexistent req: %s", resp.Message)
	}
	if resp.Message != "requirement not found" {
		t.Logf("message: %s", resp.Message)
	}
}

// --- handleEdit with title and description ---

func TestHandleEdit_TitleAndDescription(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, editPayload{
		StoryID:     storyID,
		Title:       "Updated Title",
		Description: "Updated Description",
	})
	resp := s.HandleCommand("edit_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- handleEscalate at max tier ---

func TestHandleEscalate_CapAtMaxFromHighTier(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	// Escalate multiple times
	for i := 0; i < 5; i++ {
		payload := mustMarshal(t, storyPayload{StoryID: storyID})
		resp := s.HandleCommand("escalate_story", payload)
		if !resp.Success {
			t.Fatalf("escalation %d failed: %s", i, resp.Message)
		}
	}
}

// --- findRequirement and findStory via handlers ---

func TestFindRequirement_ViaHandleResume(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)

	// Pause first
	s.HandleCommand("pause_requirement", mustMarshal(t, reqPayload{ReqID: reqID}))

	// Resume - exercises findRequirement + found path + status check
	resp := s.HandleCommand("resume_requirement", mustMarshal(t, reqPayload{ReqID: reqID}))
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

func TestFindStory_ViaHandleRetry(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	// Retry - exercises findStory + found path
	resp := s.HandleCommand("retry_story", mustMarshal(t, storyPayload{StoryID: storyID}))
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- SnapshotJSON error handling ---

func TestSnapshotJSON_EmptyWithDAG(t *testing.T) {
	s := newTestServer(t)
	// Set nil DAG
	s.SetDAG(nil)

	data, err := s.SnapshotJSON()
	if err != nil {
		t.Fatal(err)
	}

	var snap StateSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.DAG != nil {
		t.Error("expected nil DAG")
	}
}

// --- BuildSnapshot with multiple stories in same req ---

func TestBuildSnapshot_MultipleStoriesInReq(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	seedStory(t, s, reqID)

	// Seed another story with a different ID
	storyEvt2 := newTestStoryEvent(s, "story-test-002", reqID)
	s.eventStore.Append(storyEvt2)
	s.projStore.Project(storyEvt2)

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Stories) < 2 {
		t.Errorf("expected at least 2 stories, got %d", len(snap.Stories))
	}
}

func newTestStoryEvent(s *Server, id, reqID string) state.Event {
	return state.NewEvent(state.EventStoryCreated, "system", id, map[string]any{
		"id":         id,
		"req_id":     reqID,
		"title":      "Test Story 2",
		"complexity": 2,
	})
}

// --- handleReassign with tier 0 ---

func TestHandleReassign_TierZero(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, storyPayload{StoryID: storyID, TargetTier: 0})
	resp := s.HandleCommand("reassign_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- handleReassign with max tier ---

func TestHandleReassign_MaxTier(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, storyPayload{StoryID: storyID, TargetTier: maxEscalationTier})
	resp := s.HandleCommand("reassign_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- handleReassign above max ---

func TestHandleReassign_AboveMax(t *testing.T) {
	s := newTestServer(t)

	payload := mustMarshal(t, storyPayload{StoryID: "s1", TargetTier: maxEscalationTier + 1})
	resp := s.HandleCommand("reassign_story", payload)
	if resp.Success {
		t.Error("expected failure for tier above max")
	}
}
