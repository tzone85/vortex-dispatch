package state

import (
	"path/filepath"
	"testing"
	"time"
)

// helper: spin up a clean SQLite store for one test
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// helper: project an event and fail the test if it errors. We use this
// instead of inlining so tests stay focused on the assertion.
func mustProject(t *testing.T, s *SQLiteStore, evt Event) {
	t.Helper()
	if err := s.Project(evt); err != nil {
		t.Fatalf("project %s: %v", evt.Type, err)
	}
}

// TestProject_ReviewModeSet verifies that REVIEW_MODE_SET writes the
// requested mode onto the requirement row.
func TestProject_ReviewModeSet(t *testing.T) {
	s := newTestStore(t)

	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-RM1", "title": "Review-mode test", "repo_path": "/tmp/repo",
	}))

	mustProject(t, s, NewEvent(EventReviewModeSet, "system", "", map[string]any{
		"req_id": "REQ-RM1",
		"mode":   "manual",
	}))

	req, err := s.GetRequirement("REQ-RM1")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.ReviewMode != "manual" {
		t.Errorf("review_mode = %q, want manual", req.ReviewMode)
	}
}

// TestProject_ReviewModeSet_UnknownReq is a no-op (UPDATE ... WHERE id = ?
// matches zero rows). Mustn't error.
func TestProject_ReviewModeSet_UnknownReq(t *testing.T) {
	s := newTestStore(t)
	err := s.Project(NewEvent(EventReviewModeSet, "system", "", map[string]any{
		"req_id": "REQ-MISSING",
		"mode":   "auto",
	}))
	if err != nil {
		t.Errorf("review-mode set for missing req should be a no-op, got: %v", err)
	}
}

// TestProject_ReqEstimated writes the four estimation fields, accepting
// the "requirement" payload key. Estimator emits this — the projection
// row may not exist when the event fires.
func TestProject_ReqEstimated_WithRequirementKey(t *testing.T) {
	s := newTestStore(t)

	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-EST1", "title": "Estimate", "repo_path": "/tmp/repo",
	}))

	mustProject(t, s, NewEvent(EventReqEstimated, "estimator", "", map[string]any{
		"requirement": "REQ-EST1",
		"hours_low":   2.5,
		"hours_high":  6.0,
		"quote_low":   375.0,
		"quote_high":  900.0,
	}))

	req, err := s.GetRequirement("REQ-EST1")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.EstimatedHoursLow != 2.5 || req.EstimatedHoursHigh != 6.0 {
		t.Errorf("hours got %.2f..%.2f, want 2.50..6.00",
			req.EstimatedHoursLow, req.EstimatedHoursHigh)
	}
	if req.EstimatedCostLow != 375 || req.EstimatedCostHigh != 900 {
		t.Errorf("cost got %.2f..%.2f, want 375.00..900.00",
			req.EstimatedCostLow, req.EstimatedCostHigh)
	}
}

// TestProject_ReqEstimated_WithReqIDKey verifies the fallback to the
// "req_id" payload key for callers that emit either form.
func TestProject_ReqEstimated_WithReqIDKey(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-EST2", "title": "Estimate v2", "repo_path": "/tmp/repo",
	}))
	mustProject(t, s, NewEvent(EventReqEstimated, "estimator", "", map[string]any{
		"req_id":     "REQ-EST2",
		"hours_low":  1, // int — exercises payloadFloat's int branch
		"hours_high": 3,
	}))
	req, _ := s.GetRequirement("REQ-EST2")
	if req.EstimatedHoursLow != 1 || req.EstimatedHoursHigh != 3 {
		t.Errorf("int payload not converted: got %.2f..%.2f",
			req.EstimatedHoursLow, req.EstimatedHoursHigh)
	}
}

// TestProject_RecoveryCompleted attaches a timestamp to the most-recently
// created non-terminal requirement. Verify it does not error and that the
// recovered_at column gets populated.
func TestProject_RecoveryCompleted(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-REC1", "title": "Recovery", "repo_path": "/tmp/repo",
	}))
	// REQ_SUBMITTED sets status to "pending"; the recovery query only
	// matches planned/paused/analyzed. Advance the requirement to
	// "planned" so the projection has a row to attach to.
	mustProject(t, s, NewEvent(EventReqPlanned, "", "", map[string]any{"id": "REQ-REC1"}))

	when := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	evt := NewEvent(EventRecoveryCompleted, "system", "", map[string]any{"issues_found": 1})
	evt.Timestamp = when

	mustProject(t, s, evt)
	req, _ := s.GetRequirement("REQ-REC1")
	if req.RecoveredAt.IsZero() {
		t.Error("recovered_at not set on most recent planned requirement")
	}
}

// TestProject_RecoveryCompleted_NoRequirement is a documented no-op on a
// blank workspace. The projection must not error.
func TestProject_RecoveryCompleted_NoRequirement(t *testing.T) {
	s := newTestStore(t)
	err := s.Project(NewEvent(EventRecoveryCompleted, "system", "", map[string]any{
		"issues_found": 0,
	}))
	if err != nil {
		t.Errorf("recovery-completed on empty workspace must be a no-op, got: %v", err)
	}
}

// TestProject_PlanRejected goes through updateReqStatusByReqID, which is
// the path used for events not tied to a story.
func TestProject_PlanRejected(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-PR1", "title": "Plan reject", "repo_path": "/tmp/repo",
	}))
	mustProject(t, s, NewEvent(EventPlanRejected, "human", "", map[string]any{
		"req_id":   "REQ-PR1",
		"feedback": "scope unclear",
	}))
	req, _ := s.GetRequirement("REQ-PR1")
	if req.Status != "plan_rejected" {
		t.Errorf("status = %q, want plan_rejected", req.Status)
	}
}

// TestProject_StoryApproved verifies the merged-guard: once a story is
// merged, an out-of-order STORY_APPROVED must not regress it.
func TestProject_StoryApproved(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-AP1", "title": "Approve", "repo_path": "/tmp/repo",
	}))
	mustProject(t, s, NewEvent(EventStoryCreated, "", "S-AP1", map[string]any{
		"id": "S-AP1", "req_id": "REQ-AP1", "title": "T",
		"complexity": 1, "acceptance_criteria": "",
	}))
	mustProject(t, s, NewEvent(EventStoryAwaitingApproval, "", "S-AP1", nil))
	mustProject(t, s, NewEvent(EventStoryApproved, "human", "S-AP1", nil))
	st, _ := s.GetStory("S-AP1")
	if st.Status != "approved" {
		t.Errorf("status = %q, want approved", st.Status)
	}

	// Now merge and re-apply approve — must remain "merged".
	mustProject(t, s, NewEvent(EventStoryMerged, "", "S-AP1", nil))
	mustProject(t, s, NewEvent(EventStoryApproved, "human", "S-AP1", nil))
	st, _ = s.GetStory("S-AP1")
	if st.Status != "merged" {
		t.Errorf("merged story should not regress to approved; got %q", st.Status)
	}
}

// TestProject_AgentSpawned_AndTerminated round-trips both projection
// helpers and asserts the agents row reflects the final state.
func TestProject_AgentSpawned_AndTerminated(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-AG1", "title": "Agents", "repo_path": "/tmp/repo",
	}))
	mustProject(t, s, NewEvent(EventStoryCreated, "", "S-AG1", map[string]any{
		"id": "S-AG1", "req_id": "REQ-AG1", "title": "T",
		"complexity": 1, "acceptance_criteria": "",
	}))
	mustProject(t, s, NewEvent(EventAgentSpawned, "ag-1", "S-AG1", map[string]any{
		"agent_id":     "ag-1",
		"role":         "junior",
		"session_name": "vxd-S-AG1-junior",
		"runtime":      "claude-cli",
	}))

	agents, err := s.ListAgents(AgentFilter{})
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "ag-1" || agents[0].Status != "active" {
		t.Fatalf("unexpected agents after spawn: %+v", agents)
	}

	mustProject(t, s, NewEvent(EventAgentTerminated, "ag-1", "S-AG1", nil))
	agents, _ = s.ListAgents(AgentFilter{})
	if len(agents) != 1 || agents[0].Status != "terminated" {
		t.Fatalf("agent not marked terminated: %+v", agents)
	}
}

// TestProject_AgentSpawned_PayloadAgentID exercises the fallback path
// where evt.AgentID is empty and the agent_id lives in the payload.
func TestProject_AgentSpawned_PayloadAgentID(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-AG2", "title": "Agents v2", "repo_path": "/tmp/repo",
	}))
	mustProject(t, s, NewEvent(EventStoryCreated, "", "S-AG2", map[string]any{
		"id": "S-AG2", "req_id": "REQ-AG2", "title": "T",
		"complexity": 1, "acceptance_criteria": "",
	}))
	mustProject(t, s, NewEvent(EventAgentSpawned, "" /* AgentID empty */, "S-AG2", map[string]any{
		"agent_id":     "ag-fallback",
		"role":         "senior",
		"session_name": "vxd-S-AG2-senior",
	}))
	agents, _ := s.ListAgents(AgentFilter{})
	found := false
	for _, a := range agents {
		if a.ID == "ag-fallback" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("agent_id fallback from payload not honoured; got %+v", agents)
	}
}

// TestProject_AgentSpawned_Reentry verifies the ON CONFLICT path: a
// second SPAWN for the same agent must update status/session without
// inserting a duplicate row.
func TestProject_AgentSpawned_Reentry(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, NewEvent(EventReqSubmitted, "", "", map[string]any{
		"id": "REQ-AG3", "title": "Agents v3", "repo_path": "/tmp/repo",
	}))
	mustProject(t, s, NewEvent(EventStoryCreated, "", "S-AG3", map[string]any{
		"id": "S-AG3", "req_id": "REQ-AG3", "title": "T",
		"complexity": 1, "acceptance_criteria": "",
	}))

	mustProject(t, s, NewEvent(EventAgentSpawned, "ag-reentry", "S-AG3", map[string]any{
		"agent_id": "ag-reentry", "role": "junior", "session_name": "vxd-S-AG3-v1",
	}))
	mustProject(t, s, NewEvent(EventAgentTerminated, "ag-reentry", "S-AG3", nil))
	// Re-spawn the same agent.
	mustProject(t, s, NewEvent(EventAgentSpawned, "ag-reentry", "S-AG3", map[string]any{
		"agent_id": "ag-reentry", "role": "junior", "session_name": "vxd-S-AG3-v2",
	}))

	agents, _ := s.ListAgents(AgentFilter{})
	count := 0
	var sessionName, status string
	for _, a := range agents {
		if a.ID == "ag-reentry" {
			count++
			sessionName = a.SessionName
			status = a.Status
		}
	}
	if count != 1 {
		t.Errorf("ON CONFLICT failed; agent rows for ag-reentry = %d, want 1", count)
	}
	if status != "active" || sessionName != "vxd-S-AG3-v2" {
		t.Errorf("re-spawn did not refresh row; status=%q session=%q",
			status, sessionName)
	}
}
