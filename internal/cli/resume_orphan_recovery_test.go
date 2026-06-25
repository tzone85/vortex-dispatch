package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestIsRecoverableStalledStatus(t *testing.T) {
	recoverable := []string{"in_progress", "review", "qa"}
	for _, s := range recoverable {
		if !isRecoverableStalledStatus(s) {
			t.Errorf("status %q should be recoverable", s)
		}
	}
	notRecoverable := []string{"draft", "merged", "split", "awaiting_approval", "pr_submitted", ""}
	for _, s := range notRecoverable {
		if isRecoverableStalledStatus(s) {
			t.Errorf("status %q should NOT be recoverable", s)
		}
	}
}

// A story stranded in "review" (agent emitted STORY_COMPLETED, then the monitor
// was killed before review→QA→merge) with a worktree of committed work must be
// recovered so an interrupted build resumes instead of stalling forever. This
// guards the transient-failure recovery fix (#103), which shipped without a test.
func TestRecoverOrphanedStories_RecoversReviewAndQAStalls(t *testing.T) {
	dir := t.TempDir()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "vxd.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer ps.Close()

	cfg := defaultConfig()
	cfg.Workspace.StateDir = dir

	for _, id := range []string{"s-review", "s-qa", "s-draft"} {
		if err := os.MkdirAll(filepath.Join(dir, "worktrees", id), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	stories := []state.Story{
		{ID: "s-review", Status: "review"},
		{ID: "s-qa", Status: "qa"},
		{ID: "s-draft", Status: "draft"}, // dispatchable, not an orphan
	}

	orphans := recoverOrphanedStories(stories, ps, cfg)
	got := map[string]bool{}
	for _, o := range orphans {
		got[o.Assignment.StoryID] = true
	}
	if !got["s-review"] || !got["s-qa"] {
		t.Fatalf("expected review+qa stalls recovered, got %v", got)
	}
	if got["s-draft"] {
		t.Fatal("a draft (dispatchable) story must not be recovered as an orphan")
	}
	if len(orphans) != 2 {
		t.Fatalf("expected exactly 2 orphans, got %d", len(orphans))
	}
}

// A post-agent-state story with NO worktree (work lost) cannot be recovered —
// there is nothing to re-route through post-execution.
func TestRecoverOrphanedStories_ReviewWithoutWorktreeSkipped(t *testing.T) {
	dir := t.TempDir()
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "vxd.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer ps.Close()

	cfg := defaultConfig()
	cfg.Workspace.StateDir = dir

	stories := []state.Story{{ID: "s-review", Status: "review"}}
	if orphans := recoverOrphanedStories(stories, ps, cfg); len(orphans) != 0 {
		t.Fatalf("expected no orphans without a worktree, got %d", len(orphans))
	}
}
