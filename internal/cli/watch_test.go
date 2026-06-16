package cli

import (
	"strings"
	"testing"
	"time"

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

func TestEventMatchesReq_PrefixMatch(t *testing.T) {
	reqID := "01HABCDEF1234567"
	evt := state.Event{StoryID: "01HABCDE-S2"}
	if !eventMatchesReq(evt, reqID) {
		t.Errorf("expected event with matching 8-char prefix to match")
	}

	other := state.Event{StoryID: "ZZZZZZZZ-S2"}
	if eventMatchesReq(other, reqID) {
		t.Errorf("unrelated story should not match")
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
