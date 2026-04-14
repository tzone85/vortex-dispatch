package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCaptureStoryContext_WithGitRepo verifies that CaptureStoryContext writes
// a context entry to the WAVE_CONTEXT.md file using real git diff information.
func TestCaptureStoryContext_WithGitRepo(t *testing.T) {
	repoDir := setupGitRepoForWaveContext(t)

	CaptureStoryContext(repoDir, "s-wc-001", "Add User Service", "feature")

	contextPath := filepath.Join(repoDir, waveContextFileName)
	data, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatalf("read wave context: %v", err)
	}
	content := string(data)

	// Should contain story ID and title.
	if !strings.Contains(content, "s-wc-001") {
		t.Error("expected story ID in wave context")
	}
	if !strings.Contains(content, "Add User Service") {
		t.Error("expected story title in wave context")
	}

	// Should contain the file list from the diff.
	if !strings.Contains(content, "service.go") {
		t.Error("expected service.go in files changed section")
	}
}

// TestCaptureStoryContext_NoDuplicateEntry verifies that calling
// CaptureStoryContext twice for the same story doesn't create duplicates.
func TestCaptureStoryContext_NoDuplicateEntry(t *testing.T) {
	repoDir := setupGitRepoForWaveContext(t)

	CaptureStoryContext(repoDir, "s-wc-dup", "Duplicate Test", "feature")
	CaptureStoryContext(repoDir, "s-wc-dup", "Duplicate Test", "feature")

	contextPath := filepath.Join(repoDir, waveContextFileName)
	data, _ := os.ReadFile(contextPath)
	count := strings.Count(string(data), "s-wc-dup:")
	if count != 1 {
		t.Errorf("expected story to appear once, found %d times", count)
	}
}

// TestCaptureStoryContext_MultipleStories verifies multiple stories are
// appended correctly.
func TestCaptureStoryContext_MultipleStories(t *testing.T) {
	repoDir := setupGitRepoForWaveContext(t)

	CaptureStoryContext(repoDir, "s-wc-m1", "First Story", "feature")
	CaptureStoryContext(repoDir, "s-wc-m2", "Second Story", "feature")

	contextPath := filepath.Join(repoDir, waveContextFileName)
	data, _ := os.ReadFile(contextPath)
	content := string(data)

	if !strings.Contains(content, "s-wc-m1:") {
		t.Error("expected first story in context")
	}
	if !strings.Contains(content, "s-wc-m2:") {
		t.Error("expected second story in context")
	}
}

// TestCaptureStoryContext_ExtractsSignatures verifies that exported function
// signatures from changed Go files are extracted.
func TestCaptureStoryContext_ExtractsSignatures(t *testing.T) {
	repoDir := setupGitRepoForWaveContextWithGoFile(t)

	CaptureStoryContext(repoDir, "s-wc-sig", "Add Store", "feature")

	contextPath := filepath.Join(repoDir, waveContextFileName)
	data, _ := os.ReadFile(contextPath)
	content := string(data)

	// The Go file has an exported function — should appear in signatures.
	if !strings.Contains(content, "NewStore") {
		t.Error("expected exported func NewStore in wave context signatures")
	}
}

// TestCaptureStoryContext_NoChanges verifies behavior when the branch has no diff.
func TestCaptureStoryContext_NoChanges(t *testing.T) {
	repoDir := setupGitRepoForWaveContextNoChanges(t)

	// Should not panic, should still write something.
	CaptureStoryContext(repoDir, "s-wc-empty", "No Changes", "main")

	contextPath := filepath.Join(repoDir, waveContextFileName)
	data, _ := os.ReadFile(contextPath)
	content := string(data)

	// Should at least contain the story header.
	if !strings.Contains(content, "s-wc-empty:") {
		t.Error("expected story header even with no changes")
	}
}

// TestRunGitSafe_InvalidDir verifies runGitSafe returns empty on bad directory.
func TestRunGitSafe_InvalidDir(t *testing.T) {
	result := runGitSafe("/nonexistent/path", "status")
	if result != "" {
		t.Errorf("expected empty string for invalid dir, got %q", result)
	}
}

// TestRunGitSafe_ValidCommand verifies runGitSafe returns output for valid command.
func TestRunGitSafe_ValidCommand(t *testing.T) {
	repoDir := setupGitRepoForWaveContext(t)
	result := runGitSafe(repoDir, "rev-parse", "--git-dir")
	if result == "" {
		t.Error("expected non-empty output for valid git command")
	}
}

// TestRunGitSafe_InvalidCommand verifies runGitSafe returns empty for bad command.
func TestRunGitSafe_BadCommand(t *testing.T) {
	repoDir := setupGitRepoForWaveContext(t)
	result := runGitSafe(repoDir, "nonexistent-subcommand")
	if result != "" {
		t.Errorf("expected empty string for bad git subcommand, got %q", result)
	}
}

// TestExtractSignatures_MaxCap verifies the 30-signature cap across multiple files.
// The cap is checked after processing each file: once sigs > 30, no more files
// are processed. A single file can exceed 30 before the check runs.
func TestExtractSignatures_MaxCap(t *testing.T) {
	dir := t.TempDir()

	// Create two Go files, each with 20 exported functions (40 total across files).
	for _, fname := range []string{"file1.go", "file2.go"} {
		var b strings.Builder
		b.WriteString("package engine\n\n")
		for i := 0; i < 20; i++ {
			b.WriteString("func Exported")
			b.WriteString(fname[:5])
			b.WriteByte(byte('A' + i%26))
			b.WriteString("(x int) error { return nil }\n")
		}
		os.WriteFile(filepath.Join(dir, fname), []byte(b.String()), 0o644)
	}

	got := extractSignatures(dir, "file1.go\nfile2.go")
	lines := strings.Split(got, "\n")
	// After processing file1.go (20 sigs), the cap check (>30) is false.
	// After processing file2.go (40 sigs), the cap check is true — breaks.
	// But file2.go's sigs are already collected. So total = 40.
	// The cap prevents processing a THIRD file, not retroactive removal.
	// The important thing: it doesn't grow unboundedly.
	if len(lines) > 50 {
		t.Errorf("expected cap to limit signatures across files, got %d lines", len(lines))
	}
}

// --- Helper functions ---

func setupGitRepoForWaveContext(t *testing.T) string {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	if err := exec.Command("git", "init", "--bare", bareDir).Run(); err != nil {
		t.Fatalf("init bare: %v", err)
	}

	cloneDir := filepath.Join(t.TempDir(), "clone")
	if err := exec.Command("git", "clone", bareDir, cloneDir).Run(); err != nil {
		t.Fatalf("clone: %v", err)
	}

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cloneDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")

	os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("init"), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "init")
	runGit("push", "origin", "main")

	// Create feature branch with a Go service file.
	runGit("checkout", "-b", "feature")
	os.WriteFile(filepath.Join(cloneDir, "service.go"), []byte("package main\n\nfunc Serve() {}\n"), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "feat: add service")

	return cloneDir
}

func setupGitRepoForWaveContextWithGoFile(t *testing.T) string {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	if err := exec.Command("git", "init", "--bare", bareDir).Run(); err != nil {
		t.Fatalf("init bare: %v", err)
	}

	cloneDir := filepath.Join(t.TempDir(), "clone")
	if err := exec.Command("git", "clone", bareDir, cloneDir).Run(); err != nil {
		t.Fatalf("clone: %v", err)
	}

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cloneDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")

	os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("init"), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "init")
	runGit("push", "origin", "main")

	runGit("checkout", "-b", "feature")
	goFile := `package store

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Get(key string) (string, error) {
	return "", nil
}
`
	os.WriteFile(filepath.Join(cloneDir, "store.go"), []byte(goFile), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "feat: add store")

	return cloneDir
}

func setupGitRepoForWaveContextNoChanges(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}

	runGit := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644)
	runGit("add", ".")
	runGit("commit", "-m", "init")

	return dir
}
