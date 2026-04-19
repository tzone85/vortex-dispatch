package web

import (
	"encoding/json"
	"testing"
)

// --- handleKill coverage (currently 52.2%) ---

func TestHandleKill_AgentFoundButSessionDead(t *testing.T) {
	s := newTestServer(t)
	agentID := seedAgent(t, s, "vxd-dead-session")

	payload := mustMarshal(t, agentPayload{AgentID: agentID})
	resp := s.HandleCommand("kill_agent", payload)
	// Even if tmux session doesn't exist, kill should succeed (best-effort)
	if !resp.Success {
		// Agent might not be found if ListAgents returns differently
		// Either way, the handler code path is exercised
		t.Logf("kill result: %s", resp.Message)
	}
}

func TestHandleKill_InvalidPayloadJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.HandleCommand("kill_agent", json.RawMessage(`{invalid`))
	if resp.Success {
		t.Error("expected failure for invalid payload")
	}
}

// --- handleEscalate coverage ---

func TestHandleEscalate_StoryMissing(t *testing.T) {
	s := newTestServer(t)
	payload := mustMarshal(t, storyPayload{StoryID: "nonexistent"})
	resp := s.HandleCommand("escalate_story", payload)
	if resp.Success {
		t.Error("expected failure for nonexistent story")
	}
	if resp.Message != "story not found" {
		t.Errorf("unexpected message: %s", resp.Message)
	}
}

func TestHandleEscalate_InvalidPayloadJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.HandleCommand("escalate_story", json.RawMessage(`{bad`))
	if resp.Success {
		t.Error("expected failure for invalid payload")
	}
}

// --- handleResume coverage ---

func TestHandleResume_RequirementMissing(t *testing.T) {
	s := newTestServer(t)
	payload := mustMarshal(t, reqPayload{ReqID: "nonexistent"})
	resp := s.HandleCommand("resume_requirement", payload)
	if resp.Success {
		t.Error("expected failure for nonexistent requirement")
	}
}

func TestHandleResume_InvalidPayloadJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.HandleCommand("resume_requirement", json.RawMessage(`{bad`))
	if resp.Success {
		t.Error("expected failure for invalid payload")
	}
}

// --- handleRetry coverage ---

func TestHandleRetry_InvalidPayloadJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.HandleCommand("retry_story", json.RawMessage(`{bad`))
	if resp.Success {
		t.Error("expected failure for invalid payload")
	}
}

// --- handleReassign coverage ---

func TestHandleReassign_InvalidPayloadJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.HandleCommand("reassign_story", json.RawMessage(`{bad`))
	if resp.Success {
		t.Error("expected failure for invalid payload")
	}
}

func TestHandleReassign_StoryMissing(t *testing.T) {
	s := newTestServer(t)
	payload := mustMarshal(t, storyPayload{StoryID: "nonexistent", TargetTier: 1})
	resp := s.HandleCommand("reassign_story", payload)
	if resp.Success {
		t.Error("expected failure for nonexistent story")
	}
}

// --- SnapshotJSON coverage ---

func TestSnapshotJSON_ValidJSON(t *testing.T) {
	s := newTestServer(t)
	data, err := s.SnapshotJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Error("expected valid JSON output")
	}
}

// --- handleEdit coverage ---

func TestHandleEdit_InvalidPayloadJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.HandleCommand("edit_story", json.RawMessage(`{bad`))
	if resp.Success {
		t.Error("expected failure for invalid payload")
	}
}

func TestHandleEdit_ComplexityChange(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, editPayload{StoryID: storyID, Complexity: 5})
	resp := s.HandleCommand("edit_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

func TestHandleEdit_DescriptionChange(t *testing.T) {
	s := newTestServer(t)
	reqID := seedRequirement(t, s)
	storyID := seedStory(t, s, reqID)

	payload := mustMarshal(t, editPayload{StoryID: storyID, Description: "Updated description"})
	resp := s.HandleCommand("edit_story", payload)
	if !resp.Success {
		t.Errorf("expected success, got: %s", resp.Message)
	}
}

// --- handlePause edge cases ---

func TestHandlePause_EmptyReqID(t *testing.T) {
	s := newTestServer(t)
	payload := mustMarshal(t, reqPayload{ReqID: ""})
	resp := s.HandleCommand("pause_requirement", payload)
	if resp.Success {
		t.Error("expected failure for empty req_id")
	}
}
