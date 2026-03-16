package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initBareRepoWithWorktree creates a temporary bare-ish repo with a worktree
// branch that diverges from origin/main. Returns (worktreePath, cleanup).
func initBareRepoWithWorktree(t *testing.T) (string, func()) {
	t.Helper()

	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	local := filepath.Join(base, "local")
	wt := filepath.Join(base, "worktree")

	// Create a bare "origin" repository with an initial commit.
	run(t, "", "git", "init", "--bare", origin)

	// Clone origin so we have a working local repo.
	run(t, "", "git", "clone", origin, local)
	run(t, local, "git", "config", "user.email", "test@test.com")
	run(t, local, "git", "config", "user.name", "Test")

	// Create initial commit on main.
	writeFile(t, filepath.Join(local, "README.md"), "# hello\n")
	run(t, local, "git", "add", "README.md")
	run(t, local, "git", "commit", "-m", "initial commit")
	run(t, local, "git", "push", "origin", "main")

	// Create a worktree branch from the current HEAD.
	run(t, local, "git", "worktree", "add", "-b", "agent-branch", wt)
	run(t, wt, "git", "config", "user.email", "test@test.com")
	run(t, wt, "git", "config", "user.name", "Test")

	return wt, func() { os.RemoveAll(base) }
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cmd %s %v failed: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestGitDiff_NoAgentCommits(t *testing.T) {
	wt, cleanup := initBareRepoWithWorktree(t)
	defer cleanup()

	// Agent made no commits — branch HEAD == origin/main HEAD.
	diff, err := gitDiff(wt)
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff for no-change worktree, got:\n%s", diff)
	}
}

func TestGitDiff_RealChanges(t *testing.T) {
	wt, cleanup := initBareRepoWithWorktree(t)
	defer cleanup()

	// Agent commits a real code file.
	writeFile(t, filepath.Join(wt, "main.go"), "package main\n")
	run(t, wt, "git", "add", "main.go")
	run(t, wt, "git", "commit", "-m", "add main.go")

	diff, err := gitDiff(wt)
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff for real changes")
	}
	if !strings.Contains(diff, "main.go") {
		t.Errorf("expected diff to mention main.go, got:\n%s", diff)
	}
}

func TestGitDiff_GitignoreOnlyChanges(t *testing.T) {
	wt, cleanup := initBareRepoWithWorktree(t)
	defer cleanup()

	// Only .gitignore was modified (simulates ensureGitignorePatterns).
	writeFile(t, filepath.Join(wt, ".gitignore"), "CLAUDE.md\n.vxd-prompts/\n")
	run(t, wt, "git", "add", ".gitignore")
	run(t, wt, "git", "commit", "-m", "add gitignore patterns")

	diff, err := gitDiff(wt)
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	if diff != "" {
		t.Errorf("expected empty diff for .gitignore-only changes, got:\n%s", diff)
	}
}

func TestGitDiff_GitignorePlusRealChanges(t *testing.T) {
	wt, cleanup := initBareRepoWithWorktree(t)
	defer cleanup()

	// Both .gitignore and real code changed.
	writeFile(t, filepath.Join(wt, ".gitignore"), "CLAUDE.md\n")
	writeFile(t, filepath.Join(wt, "app.go"), "package app\n")
	run(t, wt, "git", "add", ".gitignore", "app.go")
	run(t, wt, "git", "commit", "-m", "add gitignore and app")

	diff, err := gitDiff(wt)
	if err != nil {
		t.Fatalf("gitDiff: %v", err)
	}
	if diff == "" {
		t.Error("expected non-empty diff when real files changed alongside .gitignore")
	}
	if !strings.Contains(diff, "app.go") {
		t.Errorf("expected diff to mention app.go, got:\n%s", diff)
	}
}

func TestIsGitignoreOnlyDiff_NoChanges(t *testing.T) {
	wt, cleanup := initBareRepoWithWorktree(t)
	defer cleanup()

	// HEAD == merge-base, no files changed.
	mb := run(t, wt, "git", "merge-base", "HEAD", "origin/main")
	if isGitignoreOnlyDiff(wt, mb) {
		t.Error("expected false when no files changed")
	}
}

func TestIsGitignoreOnlyDiff_OnlyGitignore(t *testing.T) {
	wt, cleanup := initBareRepoWithWorktree(t)
	defer cleanup()

	writeFile(t, filepath.Join(wt, ".gitignore"), "CLAUDE.md\n")
	run(t, wt, "git", "add", ".gitignore")
	run(t, wt, "git", "commit", "-m", "gitignore")

	mb := run(t, wt, "git", "merge-base", "HEAD", "origin/main")
	if !isGitignoreOnlyDiff(wt, mb) {
		t.Error("expected true when only .gitignore changed")
	}
}

func TestIsGitignoreOnlyDiff_MixedFiles(t *testing.T) {
	wt, cleanup := initBareRepoWithWorktree(t)
	defer cleanup()

	writeFile(t, filepath.Join(wt, ".gitignore"), "CLAUDE.md\n")
	writeFile(t, filepath.Join(wt, "code.go"), "package code\n")
	run(t, wt, "git", "add", ".gitignore", "code.go")
	run(t, wt, "git", "commit", "-m", "mixed")

	mb := run(t, wt, "git", "merge-base", "HEAD", "origin/main")
	if isGitignoreOnlyDiff(wt, mb) {
		t.Error("expected false when non-.gitignore files also changed")
	}
}
