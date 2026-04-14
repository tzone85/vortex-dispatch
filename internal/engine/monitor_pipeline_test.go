package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- autoCommit tests ---

func TestAutoCommit_NoUncommittedChanges(t *testing.T) {
	repoDir := setupCleanGitRepo(t)

	// No uncommitted changes — should be a no-op.
	autoCommit(repoDir, "s-auto-001")

	// Verify no new commits were made (should still be just the initial commit).
	logCmd := exec.Command("git", "log", "--oneline")
	logCmd.Dir = repoDir
	out, _ := logCmd.CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 commit (initial), got %d", len(lines))
	}
}

func TestAutoCommit_WithUncommittedChanges(t *testing.T) {
	repoDir := setupCleanGitRepo(t)

	// Create an uncommitted file.
	os.WriteFile(filepath.Join(repoDir, "new_file.go"), []byte("package main\n"), 0o644)

	autoCommit(repoDir, "s-auto-002")

	// Verify a new commit was made.
	logCmd := exec.Command("git", "log", "--oneline")
	logCmd.Dir = repoDir
	out, _ := logCmd.CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 commits (initial + auto), got %d", len(lines))
	}

	// Verify commit message.
	msgCmd := exec.Command("git", "log", "-1", "--format=%s")
	msgCmd.Dir = repoDir
	msgOut, _ := msgCmd.CombinedOutput()
	if !strings.Contains(string(msgOut), "auto-commit") {
		t.Error("expected auto-commit in commit message")
	}
}

func TestAutoCommit_InvalidDir(t *testing.T) {
	// Should not panic on invalid directory.
	autoCommit("/nonexistent/path", "s-auto-003")
}

func TestAutoCommit_SkipsVXDArtifacts(t *testing.T) {
	repoDir := setupCleanGitRepo(t)

	// Create .gitignore with VXD patterns already, then create CLAUDE.md.
	os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("CLAUDE.md\n"), 0o644)
	gitAddCommit(t, repoDir, "add gitignore")

	// Create CLAUDE.md (should be ignored).
	os.WriteFile(filepath.Join(repoDir, "CLAUDE.md"), []byte("# Test"), 0o644)
	// Create a normal file (should be committed).
	os.WriteFile(filepath.Join(repoDir, "code.go"), []byte("package main\n"), 0o644)

	autoCommit(repoDir, "s-auto-004")

	// CLAUDE.md should not be tracked.
	lsCmd := exec.Command("git", "ls-files")
	lsCmd.Dir = repoDir
	out, _ := lsCmd.CombinedOutput()
	if strings.Contains(string(out), "CLAUDE.md") {
		t.Error("CLAUDE.md should not be tracked")
	}
}

// --- captureFileTree tests ---

func TestCaptureFileTree_ValidRepo(t *testing.T) {
	repoDir := setupCleanGitRepo(t)
	os.WriteFile(filepath.Join(repoDir, "extra.go"), []byte("package main\n"), 0o644)
	gitAddCommit(t, repoDir, "add extra.go")

	tree := captureFileTree(repoDir)
	if !strings.Contains(tree, "README.md") {
		t.Error("expected README.md in file tree")
	}
	if !strings.Contains(tree, "extra.go") {
		t.Error("expected extra.go in file tree")
	}
}

func TestCaptureFileTree_InvalidDir(t *testing.T) {
	tree := captureFileTree("/nonexistent/path")
	if tree != "" {
		t.Errorf("expected empty string for invalid dir, got %q", tree)
	}
}

// --- gitDiff edge case tests ---

func TestGitDiff_InvalidDir_Returns_Error(t *testing.T) {
	_, err := gitDiff("/nonexistent/path")
	if err == nil {
		t.Error("expected error for invalid directory")
	}
}

// TestGitDiff_FallbackToRootCommit verifies that gitDiff works on a repo
// without origin/main by falling back to the root commit.
func TestGitDiff_FallbackToRootCommit(t *testing.T) {
	repoDir := setupGitRepoWithFeatureBranch(t)

	diff, err := gitDiff(repoDir)
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff for feature branch with changes")
	}
	if !strings.Contains(diff, "feature.go") {
		t.Error("expected feature.go in diff")
	}
}

// TestIsGitignoreOnlyDiff_InvalidDir_EdgeCase verifies isGitignoreOnlyDiff
// returns false for invalid directories.
func TestIsGitignoreOnlyDiff_InvalidDir_EdgeCase(t *testing.T) {
	result := isGitignoreOnlyDiff("/nonexistent/path", "abc123")
	if result {
		t.Error("expected false for invalid directory")
	}
}

// --- Helper functions ---

func setupCleanGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitInit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	runGitInit("init")
	runGitInit("config", "user.email", "test@test.com")
	runGitInit("config", "user.name", "Test")

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644)
	runGitInit("add", ".")
	runGitInit("commit", "-m", "init")

	return dir
}

func setupGitRepoWithFeatureBranch(t *testing.T) string {
	t.Helper()
	dir := setupCleanGitRepo(t)
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	runGit("checkout", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package main\n\nfunc Feature() {}\n"), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "feat: add feature")

	return dir
}

func gitAddCommit(t *testing.T, dir, msg string) {
	t.Helper()
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v (%s)", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", msg)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, out)
	}
}
