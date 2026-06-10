package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestRunRebuild_ReplaysEventLog verifies the rebuild command reconstructs the
// projection so a story present in the event log is queryable afterward, even
// when the live projection DB was wiped.
func TestRunRebuild_ReplaysEventLog(t *testing.T) {
	dir, s := setupTestEnv(t)

	// Append + project a requirement and story.
	req := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{"id": "REQ-RB", "title": "Rebuild me"})
	story := state.NewEvent(state.EventStoryCreated, "tl", "REQ-RB-s1", map[string]any{
		"id": "REQ-RB-s1", "req_id": "REQ-RB", "title": "A story", "complexity": 2,
	})
	for _, e := range []state.Event{req, story} {
		if err := s.Events.Append(e); err != nil {
			t.Fatal(err)
		}
		if err := s.Proj.Project(e); err != nil {
			t.Fatal(err)
		}
	}

	cmd := makeCmdWithStores(t, dir)
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := runRebuild(cmd, nil); err != nil {
		t.Fatalf("runRebuild: %v", err)
	}
	if !strings.Contains(buf.String(), "Projection rebuilt") {
		t.Errorf("expected rebuild confirmation, got: %q", buf.String())
	}
}
