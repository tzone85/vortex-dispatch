package engine

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignorePatterns_CreatesNew(t *testing.T) {
	dir := t.TempDir()

	ensureGitignorePatterns(dir)

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	for _, pat := range []string{"CLAUDE.md", "WAVE_CONTEXT.md", "REQUIREMENT.md", "vxd.yaml", ".vxd-prompts/", ".serena/", "firebase-debug.log"} {
		if !strings.Contains(string(content), pat) {
			t.Errorf("expected .gitignore to contain %q", pat)
		}
	}
}

func TestEnsureGitignorePatterns_ExistingPatterns(t *testing.T) {
	dir := t.TempDir()
	giPath := filepath.Join(dir, ".gitignore")

	// Pre-create .gitignore with all patterns already present (AGENTS.md
	// was added alongside CLAUDE.md when VXD started dual-writing the
	// agent directive for Codex/Gemini runtimes).
	existing := "CLAUDE.md\nAGENTS.md\nWAVE_CONTEXT.md\nREQUIREMENT.md\nvxd.yaml\n.vxd-prompts/\n.serena/\nfirebase-debug.log\n"
	os.WriteFile(giPath, []byte(existing), 0o644)

	ensureGitignorePatterns(dir)

	content, err := os.ReadFile(giPath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	// Should not add duplicates
	if string(content) != existing {
		t.Errorf("expected no changes, but .gitignore was modified to:\n%s", string(content))
	}
}

func TestEnsureGitignorePatterns_PartialExisting(t *testing.T) {
	dir := t.TempDir()
	giPath := filepath.Join(dir, ".gitignore")

	// Only has CLAUDE.md
	os.WriteFile(giPath, []byte("CLAUDE.md\nnode_modules/\n"), 0o644)

	ensureGitignorePatterns(dir)

	content, err := os.ReadFile(giPath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	s := string(content)
	// Should keep existing content and add missing patterns
	if !strings.Contains(s, "node_modules/") {
		t.Error("expected existing node_modules/ to be preserved")
	}
	if !strings.Contains(s, ".vxd-prompts/") {
		t.Error("expected .vxd-prompts/ to be added")
	}
	if !strings.Contains(s, ".serena/") {
		t.Error("expected .serena/ to be added")
	}
	if !strings.Contains(s, "firebase-debug.log") {
		t.Error("expected firebase-debug.log to be added")
	}

	// Count occurrences of CLAUDE.md — should only appear once
	count := strings.Count(s, "CLAUDE.md")
	if count != 1 {
		t.Errorf("expected CLAUDE.md to appear once, appeared %d times", count)
	}
}

// initBareGitRepo creates a minimal git repo in dir with one commit so that
// stash and pull operations have a valid HEAD to work against.
func initBareGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %v\n%s", args, err, out)
		}
	}
}

func TestGitPullWithStash_DirtyTree_SkipsCleanly(t *testing.T) {
	dir := t.TempDir()
	initBareGitRepo(t, dir)

	// Dirty the working tree with an untracked file.
	dirtyFile := filepath.Join(dir, "dirty.txt")
	if err := os.WriteFile(dirtyFile, []byte("uncommitted"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	// Capture log output to assert no "failed" message is emitted.
	var logBuf bytes.Buffer
	oldFlags := log.Flags()
	log.SetFlags(0)
	log.SetOutput(&logBuf)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(oldFlags)
	})

	// No remote "origin" exists, so stash will succeed but pull will fail.
	// The important assertion is that the log message does NOT contain "failed"
	// and that the dirty file still exists (working tree preserved).
	gitPullWithStash(dir, "main")

	logOut := logBuf.String()
	if strings.Contains(logOut, "failed") {
		t.Errorf("expected no 'failed' in log output, got:\n%s", logOut)
	}

	// Dirty file should still be present (either via stash pop or skip).
	if _, err := os.Stat(dirtyFile); os.IsNotExist(err) {
		t.Error("expected dirty.txt to still exist after pull attempt")
	}
}

func TestEnsureGitignorePatterns_VXDArtifactHeader(t *testing.T) {
	dir := t.TempDir()

	ensureGitignorePatterns(dir)

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}

	if !strings.Contains(string(content), "# VXD agent artifacts") {
		t.Error("expected VXD artifact header comment in .gitignore")
	}
}
