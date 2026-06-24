package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

// TestRunReviewStory_ShowsIntent verifies that `vxd review <story>` — the path a
// human takes to "click through" a story — surfaces the description and the
// acceptance criteria as readable bullet items, so the intent is legible.
func TestRunReviewStory_ShowsIntent(t *testing.T) {
	dir, s := setupTestEnv(t)

	reqEvt := state.NewEvent(state.EventReqSubmitted, "", "", map[string]any{
		"id":    "REQ-RVI",
		"title": "Review Intent",
	})
	s.Events.Append(reqEvt)
	s.Proj.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tech-lead", "STR-RVI1", map[string]any{
		"id":                  "STR-RVI1",
		"req_id":              "REQ-RVI",
		"title":               "Domain model",
		"complexity":          3,
		"description":         "Define the core domain entities for the world.",
		"acceptance_criteria": "Failing tests written first. mvn test green. WorldState.copy() produces independent instance.",
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
	cmd.SetArgs([]string{"STR-RVI1"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Description:") {
		t.Errorf("expected a Description: label, got:\n%s", out)
	}
	if !strings.Contains(out, "Define the core domain entities for the world.") {
		t.Errorf("expected the description text, got:\n%s", out)
	}
	if !strings.Contains(out, "Acceptance Criteria:") {
		t.Errorf("expected an Acceptance Criteria: label, got:\n%s", out)
	}
	// Each criterion should appear as its own bullet, not a run-on blob.
	for _, want := range []string{
		"- Failing tests written first.",
		"- mvn test green.",
		"- WorldState.copy() produces independent instance.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected bullet %q in output, got:\n%s", want, out)
		}
	}
}
