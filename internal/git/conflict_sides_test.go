package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupRebaseConflict builds a repo where `base` and `topic` both change the
// same file, then starts `git rebase base` on topic (leaving it conflicted).
// Returns the worktree dir and the conflicted filename.
func setupRebaseConflict(t *testing.T, name, baseContent, topicContent string) string {
	t.Helper()
	dir := t.TempDir()
	helperRun(t, dir, "git", "init")
	helperRun(t, dir, "git", "config", "user.email", "test@test.com")
	helperRun(t, dir, "git", "config", "user.name", "Test")

	write := func(c string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(c), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("common\n")
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "init")
	base := helperRun(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")

	helperRun(t, dir, "git", "checkout", "-b", "topic")
	write(topicContent)
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "topic")

	helperRun(t, dir, "git", "checkout", base)
	write(baseContent)
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "base")

	helperRun(t, dir, "git", "checkout", "topic")
	rebase := exec.Command("git", "rebase", base)
	rebase.Dir = dir
	_, _ = rebase.CombinedOutput() // expected to conflict
	return dir
}

// ConflictSides must return ours = the base (rebased-onto) version and theirs =
// the topic (story) version. This pins the rebase ours/theirs semantics that the
// deterministic conflict fallbacks depend on.
func TestConflictSides_OursIsBaseTheirsIsStory(t *testing.T) {
	dir := setupRebaseConflict(t, "package.json",
		`{"base":true}`+"\n",  // base side
		`{"story":true}`+"\n", // topic/story side
	)

	ours, theirs, err := ConflictSides(dir, "package.json")
	if err != nil {
		t.Fatalf("ConflictSides: %v", err)
	}
	if !strings.Contains(string(ours), `"base"`) {
		t.Errorf("ours should be the base version, got %q", ours)
	}
	if !strings.Contains(string(theirs), `"story"`) {
		t.Errorf("theirs should be the story version, got %q", theirs)
	}
}

// CheckoutTheirs must resolve the file to the story version AND stage it (so the
// rebase can continue with no remaining conflict).
func TestCheckoutTheirs_ResolvesToStoryAndStages(t *testing.T) {
	dir := setupRebaseConflict(t, "config.ts",
		"export const base = true\n",
		"export const story = true\n",
	)

	if err := CheckoutTheirs(dir, "config.ts"); err != nil {
		t.Fatalf("CheckoutTheirs: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "config.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "story") {
		t.Errorf("working tree should hold the story version, got %q", got)
	}
	// No remaining conflicts: the file must be staged/resolved.
	files, err := ConflictedFiles(dir)
	if err != nil {
		t.Fatalf("ConflictedFiles: %v", err)
	}
	for _, f := range files {
		if f == "config.ts" {
			t.Errorf("config.ts still conflicted after CheckoutTheirs; remaining=%v", files)
		}
	}
}
