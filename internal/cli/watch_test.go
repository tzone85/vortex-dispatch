package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/engine"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestFormatWatchLine_IncludesTypeAndIDs(t *testing.T) {
	ts := time.Date(2026, 6, 16, 14, 30, 5, 0, time.UTC)
	evt := state.Event{
		ID:        "evt-1",
		Type:      state.EventType("STORY_STARTED"),
		StoryID:   "01234567-S1",
		AgentID:   "agent-a",
		Timestamp: ts,
	}
	got := formatWatchLine(evt)
	want := []string{"14:30:05", "STORY_STARTED", "story=01234567-S1", "agent=agent-a"}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("formatWatchLine missing %q in %q", w, got)
		}
	}
}

func TestFormatWatchLine_OmitsEmptyFields(t *testing.T) {
	ts := time.Date(2026, 6, 16, 14, 30, 5, 0, time.UTC)
	evt := state.Event{
		ID:        "evt-1",
		Type:      state.EventType("REQ_SUBMITTED"),
		Timestamp: ts,
	}
	got := formatWatchLine(evt)
	if strings.Contains(got, "story=") {
		t.Errorf("formatWatchLine should omit story= when empty: %q", got)
	}
	if strings.Contains(got, "agent=") {
		t.Errorf("formatWatchLine should omit agent= when empty: %q", got)
	}
}

// ulidLikeReqID is 26 chars — the length `vxd req` really generates. Story IDs
// for reqIDs >8 chars are namespaced with sha256(reqID)[:8] (StoryIDPrefix),
// NOT reqID[:8], so a matcher comparing raw reqID prefixes silently drops
// every story event in production.
const ulidLikeReqID = "01JYZX8Q2M3N4P5R6S7T8V9W0X"

func TestEventMatchesReq_HashedStoryPrefix(t *testing.T) {
	storyID := engine.StoryIDPrefix(ulidLikeReqID) + "-s-001"
	evt := state.Event{Type: "STORY_STARTED", StoryID: storyID}
	if !eventMatchesReq(evt, ulidLikeReqID) {
		t.Fatalf("story %s must match req %s (hashed prefix)", storyID, ulidLikeReqID)
	}
}

func TestEventMatchesReq_ShortReqIDVerbatimPrefix(t *testing.T) {
	evt := state.Event{Type: "STORY_STARTED", StoryID: "r-001-s-002"}
	if !eventMatchesReq(evt, "r-001") {
		t.Fatal("short reqIDs are used verbatim as story prefixes and must match")
	}
}

func TestEventMatchesReq_ReqEventViaPayload(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"req_id": ulidLikeReqID})
	evt := state.Event{Type: "REQ_PLANNED", Payload: payload}
	if !eventMatchesReq(evt, ulidLikeReqID) {
		t.Fatal("REQ_* events carry req_id in the payload and must match")
	}
}

func TestEventMatchesReq_OtherRequirement(t *testing.T) {
	otherStory := engine.StoryIDPrefix("01JOTHERREQIDENTIFIER00000") + "-s-001"
	payload, _ := json.Marshal(map[string]any{"req_id": "someone-else"})
	for _, evt := range []state.Event{
		{Type: "STORY_STARTED", StoryID: otherStory},
		{Type: "REQ_PLANNED", Payload: payload},
		{Type: "AGENT_STUCK"},
	} {
		if eventMatchesReq(evt, ulidLikeReqID) {
			t.Errorf("event %+v must NOT match req %s", evt, ulidLikeReqID)
		}
	}
}

func TestTerminalRequirementStatuses_CoversExpected(t *testing.T) {
	for _, status := range []string{"completed", "done", "failed", "archived"} {
		if !terminalRequirementStatuses[status] {
			t.Errorf("status %q should be terminal", status)
		}
	}
	if terminalRequirementStatuses["in_progress"] {
		t.Errorf("'in_progress' must NOT be terminal")
	}
}

// newTestStores builds real (file + in-memory-sqlite) stores in a temp dir.
func newTestStores(t *testing.T) stores {
	t.Helper()
	es, err := state.NewFileStore(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("projection store: %v", err)
	}
	t.Cleanup(func() { _ = es.Close(); _ = ps.Close() })
	return stores{Events: es, Proj: ps}
}

func appendAndProject(t *testing.T, s stores, evt state.Event) {
	t.Helper()
	if err := s.Events.Append(evt); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.Proj.Project(evt); err != nil {
		t.Fatalf("project: %v", err)
	}
}

func TestTailRequirementEvents_PrintsMatchesAndStopsOnTerminal(t *testing.T) {
	s := newTestStores(t)
	reqID := ulidLikeReqID
	storyID := engine.StoryIDPrefix(reqID) + "-s-001"

	// Payload keys mirror the real emitters: REQ_SUBMITTED (planner) and
	// REQ_COMPLETED (emitRequirementOutcome) both use "id", not "req_id".
	submitPayload, _ := json.Marshal(map[string]any{"id": reqID, "title": "Build the thing", "description": "x"})
	appendAndProject(t, s, state.Event{
		ID: "e1", Type: state.EventReqSubmitted, Timestamp: time.Now().Add(-3 * time.Second),
		AgentID: "system", Payload: submitPayload,
	})
	appendAndProject(t, s, state.Event{
		ID: "e2", Type: "STORY_STARTED", Timestamp: time.Now().Add(-2 * time.Second),
		AgentID: "agent-1", StoryID: storyID,
	})
	donePayload, _ := json.Marshal(map[string]any{"id": reqID})
	appendAndProject(t, s, state.Event{
		ID: "e3", Type: state.EventReqCompleted, Timestamp: time.Now().Add(-1 * time.Second),
		AgentID: "system", Payload: donePayload,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out strings.Builder
	if err := tailRequirementEvents(ctx, &out, s, reqID); err != nil {
		t.Fatalf("tail: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "STORY_STARTED") {
		t.Errorf("output missing story event:\n%s", got)
	}
	if !strings.Contains(got, string(state.EventReqCompleted)) {
		t.Errorf("output missing REQ_COMPLETED event:\n%s", got)
	}
	if !strings.Contains(got, "terminal status") {
		t.Errorf("output should announce terminal exit:\n%s", got)
	}
}

func TestResolveWatchReqID_ExplicitArgWins(t *testing.T) {
	got, err := resolveWatchReqID(newWatchCmd(), stores{}, []string{"explicit-id"})
	if err != nil || got != "explicit-id" {
		t.Fatalf("want explicit-id, got %q err=%v", got, err)
	}
}

func TestResolveWatchReqID_NoRequirements(t *testing.T) {
	s := newTestStores(t)
	cmd := newWatchCmd()
	if _, err := resolveWatchReqID(cmd, s, nil); err == nil || !strings.Contains(err.Error(), "no requirement to watch") {
		t.Fatalf("expected 'no requirement to watch' error, got %v", err)
	}
}
