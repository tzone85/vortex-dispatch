package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
)

// createTestRepo initialises a temporary git repository with one commit so
// that branches and worktrees can be created from it.
func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.email", "test@test.com")
	runCmd(t, dir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("test"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runCmd(t, dir, "git", "add", ".")
	runCmd(t, dir, "git", "commit", "-m", "init")
	return dir
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v (%s)", name, strings.Join(args, " "), err, string(out))
	}
}

func TestCreateAndDeleteWorktree(t *testing.T) {
	repo := createTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "wt-feature")

	err := vxdgit.CreateWorktree(repo, wtPath, "feature/test-story")
	if err != nil {
		t.Fatalf("create worktree: %v", err)
	}

	// Verify worktree directory exists.
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Fatal("worktree directory should exist")
	}

	// Verify branch was created.
	cmd := exec.Command("git", "branch", "--list", "feature/test-story")
	cmd.Dir = repo
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "feature/test-story") {
		t.Fatal("branch should exist")
	}

	// Delete.
	err = vxdgit.DeleteWorktree(repo, wtPath)
	if err != nil {
		t.Fatalf("delete worktree: %v", err)
	}
}

// TestCreateWorktreeIdempotentWithRemoteTracking verifies that CreateWorktree
// succeeds on a second call even when a remote tracking branch (origin/<branch>)
// was left behind by a previous failed attempt.
func TestCreateWorktreeIdempotentWithRemoteTracking(t *testing.T) {
	// Set up a "remote" (bare repo) and clone from it so origin/* refs exist.
	remote := t.TempDir()
	runCmd(t, remote, "git", "init", "--bare")

	cloneDir := t.TempDir()
	runCmd(t, cloneDir, "git", "clone", remote, ".")
	runCmd(t, cloneDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, cloneDir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(cloneDir, "README"), []byte("test"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runCmd(t, cloneDir, "git", "add", ".")
	runCmd(t, cloneDir, "git", "commit", "-m", "init")
	runCmd(t, cloneDir, "git", "push", "origin", "HEAD:main")

	// Simulate a previous failed attempt: create and push the branch, then
	// delete only the local branch (leaving origin/feature/retry-test behind).
	runCmd(t, cloneDir, "git", "checkout", "-b", "feature/retry-test")
	runCmd(t, cloneDir, "git", "push", "origin", "feature/retry-test")
	runCmd(t, cloneDir, "git", "checkout", "main")
	runCmd(t, cloneDir, "git", "branch", "-D", "feature/retry-test")

	// Verify the remote tracking ref exists to confirm our test setup.
	cmd := exec.Command("git", "branch", "-r")
	cmd.Dir = cloneDir
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "origin/feature/retry-test") {
		t.Fatalf("expected origin/feature/retry-test to exist, got: %s", out)
	}

	// CreateWorktree should succeed despite the stale remote tracking branch.
	wtPath := filepath.Join(t.TempDir(), "wt-retry")
	if err := vxdgit.CreateWorktree(cloneDir, wtPath, "feature/retry-test"); err != nil {
		t.Fatalf("CreateWorktree should succeed after cleaning remote tracking ref: %v", err)
	}

	// Verify the worktree was created.
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Fatal("worktree directory should exist after successful create")
	}

	// Cleanup.
	vxdgit.DeleteWorktree(cloneDir, wtPath)
}

func TestListWorktrees(t *testing.T) {
	repo := createTestRepo(t)
	wtPath := filepath.Join(t.TempDir(), "wt-list")

	if err := vxdgit.CreateWorktree(repo, wtPath, "feature/list-test"); err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	defer vxdgit.DeleteWorktree(repo, wtPath)

	trees, err := vxdgit.ListWorktrees(repo)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// Should have at least 2 (main + our worktree).
	if len(trees) < 2 {
		t.Fatalf("expected at least 2 worktrees, got %d: %v", len(trees), trees)
	}

	// Resolve symlinks so macOS /var/folders matches /private/var/folders.
	realWtPath, err := filepath.EvalSymlinks(wtPath)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	found := false
	for _, p := range trees {
		if p == wtPath || p == realWtPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected worktree %s (real: %s) in list %v", wtPath, realWtPath, trees)
	}
}
