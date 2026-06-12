package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTempRepo creates a git repo with one commit so subsequent branch
// + worktree tests have something to operate on. Returns the repo path.
func initTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	// Need at least one commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "seed"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	for _, args := range [][]string{
		{"add", "seed"},
		{"commit", "-q", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	return dir
}

func TestCliGitCleanupOps_BranchExists(t *testing.T) {
	dir := initTempGitRepo(t)
	ops := &cliGitCleanupOps{}

	if !ops.BranchExists(dir, "main") {
		t.Error("main branch should exist after init+commit")
	}
	if ops.BranchExists(dir, "no-such-branch-xyz") {
		t.Error("unknown branch should not be reported as existing")
	}
}

func TestCliGitCleanupOps_DeleteBranch(t *testing.T) {
	dir := initTempGitRepo(t)
	ops := &cliGitCleanupOps{}

	// Create a branch off main so we can drop it.
	cmd := exec.Command("git", "branch", "feature/x")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup branch: %v (%s)", err, out)
	}
	if !ops.BranchExists(dir, "feature/x") {
		t.Fatal("created branch should exist")
	}
	if err := ops.DeleteBranch(dir, "feature/x"); err != nil {
		t.Fatalf("delete branch: %v", err)
	}
	if ops.BranchExists(dir, "feature/x") {
		t.Error("deleted branch should not exist")
	}
}

func TestCliGitCleanupOps_DeleteBranch_NonExistent(t *testing.T) {
	dir := initTempGitRepo(t)
	ops := &cliGitCleanupOps{}
	// Deleting a non-existent branch should surface as error.
	if err := ops.DeleteBranch(dir, "ghost/branch"); err == nil {
		t.Error("expected error deleting non-existent branch")
	}
}

func TestCliGitCleanupOps_DeleteWorktree(t *testing.T) {
	dir := initTempGitRepo(t)
	ops := &cliGitCleanupOps{}

	wt := t.TempDir() + "/wt-target"
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/wt", wt)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup worktree: %v (%s)", err, out)
	}

	if err := ops.DeleteWorktree(dir, wt); err != nil {
		t.Fatalf("delete worktree: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree directory should be gone after DeleteWorktree, stat err: %v", err)
	}
}

func TestCliGitCleanupOps_DeleteWorktree_NonExistent(t *testing.T) {
	dir := initTempGitRepo(t)
	ops := &cliGitCleanupOps{}
	if err := ops.DeleteWorktree(dir, "/tmp/no-such-worktree-vxd"); err == nil {
		t.Error("expected error for non-existent worktree")
	}
}
