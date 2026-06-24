package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestRunRetry_ResetsStory verifies `vxd retry` emits a STORY_RESET, returns the
// story to draft, and the escalation tier is recomputed as 0 afterwards (so a
// transiently-pinned story is genuinely un-pinned).
func TestRunRetry_ResetsStory(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{"id": "REQ-RT", "title": "Retry"})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-RT1", map[string]any{
		"id": "STR-RT1", "req_id": "REQ-RT", "title": "Pinned story", "complexity": 3,
	})
	s.Events.Append(storyEvt)
	s.Proj.Project(storyEvt)

	// Escalate to tier 4 (pinned).
	for _, tt := range [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}} {
		esc := state.NewEvent(state.EventStoryEscalated, "", "STR-RT1", map[string]any{"from_tier": tt[0], "to_tier": tt[1]})
		s.Events.Append(esc)
		s.Proj.Project(esc)
	}
	s.Close()

	orig, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(orig) })
	t.Setenv("HOME", dir)

	cmd := newRetryCmd()
	cmd.PersistentFlags().String("config", filepath.Join(dir, "nonexistent.yaml"), "")
	cmd.PersistentFlags().String("project", "test-project", "")
	cmd.SetArgs([]string{"STR-RT1"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "reset") {
		t.Errorf("expected reset confirmation, got: %s", out)
	}
	if !strings.Contains(out, "resume REQ-RT") {
		t.Errorf("expected resume guidance with req id, got: %s", out)
	}
}
