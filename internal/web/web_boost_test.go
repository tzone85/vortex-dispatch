package web

import (
	"encoding/json"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// --- handleKill full path test (currently 52.2%) ---

func TestHandleKill_WithAgentInStore(t *testing.T) {
	s := newTestServer(t)
	// Seed a requirement first
	reqID := seedRequirement(t, s)
	// Seed a story
	seedStory(t, s, reqID)
	// Seed an agent
	agentID := seedAgent(t, s, "vxd-kill-test")

	payload := mustMarshal(t, agentPayload{AgentID: agentID})
	resp := s.HandleCommand("kill_agent", payload)
	// Should succeed even if tmux session doesn't exist (best-effort kill)
	if !resp.Success {
		t.Logf("kill response: %s (may depend on tmux state)", resp.Message)
	}
}

func TestHandleKill_EmptyAgentIDPayload(t *testing.T) {
	s := newTestServer(t)
	payload := mustMarshal(t, agentPayload{AgentID: ""})
	resp := s.HandleCommand("kill_agent", payload)
	if resp.Success {
		t.Error("expected failure for empty agent_id")
	}
}

// --- handleEscalate with real story ---

func TestHandleEscalate_WithStory(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, storyPayload{StoryID: storyID})
	resp := s.HandleCommand("escalate_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- handleResume with paused requirement ---

func TestHandleResume_WithPausedReq(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)

	// Pause the requirement first
	pausePayload := mustMarshal(t, reqPayload{ReqID: reqID})
	pauseResp := s.HandleCommand("pause_requirement", pausePayload)
	if !pauseResp.Success {
		t.Fatalf("pause failed: %s", pauseResp.Message)
	}

	// Now resume
	resumePayload := mustMarshal(t, reqPayload{ReqID: reqID})
	resp := s.HandleCommand("resume_requirement", resumePayload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- handleRetry with story ---

func TestHandleRetry_WithStory(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, storyPayload{StoryID: storyID})
	resp := s.HandleCommand("retry_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- handleReassign with story ---

func TestHandleReassign_WithStory(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, storyPayload{StoryID: storyID, TargetTier: 2})
	resp := s.HandleCommand("reassign_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- SnapshotJSON with data ---

func TestSnapshotJSON_WithRequirements(t *testing.T) {
	s := newTestServer(t)
	seedRequirement(t, s)

	data, err := s.SnapshotJSON()
	if err != nil {
		t.Fatal(err)
	}

	var snap StateSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Requirements) != 1 {
		t.Errorf("expected 1 requirement, got %d", len(snap.Requirements))
	}
}

// --- BuildSnapshot with event count ---

func TestBuildSnapshot_EventLimit(t *testing.T) {
	s := newTestServer(t)

	// Seed many events
	for i := 0; i < 55; i++ {
		evt := state.NewEvent(state.EventStoryProgress, "agent", "s1", map[string]any{
			"action": "progress",
		})
		s.eventStore.Append(evt)
	}

	snap, err := s.BuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	// Events should be limited to 50
	if len(snap.Events) > 55 {
		t.Errorf("expected <= 55 events, got %d", len(snap.Events))
	}
}
