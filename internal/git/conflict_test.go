package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// helperInitRepo creates a temporary git repo with one commit and returns its path.
func helperInitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	helperRun(t, dir, "git", "init")
	helperRun(t, dir, "git", "config", "user.email", "test@test.com")
	helperRun(t, dir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "init")
	return dir
}

func helperRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v (%s)", name, strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// setupConflict creates a repo with two branches that have conflicting changes,
// checks out the topic branch, and returns (repoDir, mainBranch, topicBranch).
func setupConflict(t *testing.T) (string, string, string) {
	t.Helper()
	dir := helperInitRepo(t)

	// Determine the default branch name (main or master).
	mainBranch := helperRun(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")

	// Create a conflicting change on a topic branch.
	helperRun(t, dir, "git", "checkout", "-b", "topic")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("topic change\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "topic commit")

	// Create a conflicting change on main.
	helperRun(t, dir, "git", "checkout", mainBranch)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main change\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "main commit")

	// Switch back to topic so StartRebase will rebase topic onto main.
	helperRun(t, dir, "git", "checkout", "topic")

	return dir, mainBranch, "topic"
}

func TestIsConflictOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"CONFLICT keyword", "CONFLICT (content): Merge conflict in file.txt", true},
		{"could not apply", "error: could not apply abc1234... some commit", true},
		{"Resolve all conflicts", "Resolve all conflicts manually", true},
		{"clean output", "Successfully rebased and updated refs/heads/main", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConflict(tt.output)
			if got != tt.want {
				t.Errorf("isConflict(%q) = %v; want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestConflictError_Error(t *testing.T) {
	ce := &ConflictError{Output: "CONFLICT in file.txt"}
	msg := ce.Error()
	if !strings.Contains(msg, "merge conflict") {
		t.Errorf("Error() = %q; expected it to contain 'merge conflict'", msg)
	}
	if !strings.Contains(msg, "CONFLICT in file.txt") {
		t.Errorf("Error() = %q; expected it to contain the output", msg)
	}
}

func TestIsConflict(t *testing.T) {
	t.Run("true for ConflictError", func(t *testing.T) {
		err := &ConflictError{Output: "conflict"}
		if !IsConflict(err) {
			t.Error("IsConflict should return true for *ConflictError")
		}
	})

	t.Run("false for generic error", func(t *testing.T) {
		err := errors.New("some other error")
		if IsConflict(err) {
			t.Error("IsConflict should return false for generic error")
		}
	})

	t.Run("false for nil", func(t *testing.T) {
		if IsConflict(nil) {
			t.Error("IsConflict should return false for nil")
		}
	})
}

func TestStartRebase_NoConflict(t *testing.T) {
	dir := helperInitRepo(t)
	mainBranch := helperRun(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")

	// Create a non-conflicting branch.
	helperRun(t, dir, "git", "checkout", "-b", "topic-clean")
	if err := os.WriteFile(filepath.Join(dir, "newfile.txt"), []byte("new content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "topic commit")

	// Add a non-conflicting change on main.
	helperRun(t, dir, "git", "checkout", mainBranch)
	if err := os.WriteFile(filepath.Join(dir, "another.txt"), []byte("another\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "main commit")

	helperRun(t, dir, "git", "checkout", "topic-clean")

	err := StartRebase(dir, mainBranch)
	if err != nil {
		t.Fatalf("StartRebase should succeed with no conflicts, got: %v", err)
	}
}

func TestStartRebase_WithConflict(t *testing.T) {
	dir, mainBranch, _ := setupConflict(t)

	err := StartRebase(dir, mainBranch)
	if err == nil {
		t.Fatal("StartRebase should return an error on conflict")
	}

	if !IsConflict(err) {
		t.Fatalf("expected ConflictError, got: %v", err)
	}

	ce := err.(*ConflictError)
	if ce.Output == "" {
		t.Error("ConflictError.Output should not be empty")
	}
}

func TestStartRebase_InvalidDir(t *testing.T) {
	err := StartRebase("/nonexistent/path", "main")
	if err == nil {
		t.Fatal("StartRebase should fail with invalid directory")
	}
	// Should not be a conflict error.
	if IsConflict(err) {
		t.Error("error for invalid dir should not be a ConflictError")
	}
}

func TestConflictedFiles_WithConflict(t *testing.T) {
	dir, mainBranch, _ := setupConflict(t)

	// Start a rebase that will conflict (leave it in progress).
	err := StartRebase(dir, mainBranch)
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got: %v", err)
	}

	files, err := ConflictedFiles(dir)
	if err != nil {
		t.Fatalf("ConflictedFiles: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("expected at least one conflicted file")
	}

	found := false
	for _, f := range files {
		if f == "file.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'file.txt' in conflicted files, got: %v", files)
	}

	// Clean up rebase state.
	RebaseAbort(dir)
}

func TestConflictedFiles_NoConflict(t *testing.T) {
	dir := helperInitRepo(t)

	files, err := ConflictedFiles(dir)
	if err != nil {
		t.Fatalf("ConflictedFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no conflicted files in clean repo, got: %v", files)
	}
}

func TestConflictedFiles_InvalidDir(t *testing.T) {
	_, err := ConflictedFiles("/nonexistent/path")
	if err == nil {
		t.Fatal("ConflictedFiles should fail with invalid directory")
	}
}

func TestStageFiles(t *testing.T) {
	dir := helperInitRepo(t)

	// Create an unstaged file.
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("content\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := StageFiles(dir, []string{"staged.txt"})
	if err != nil {
		t.Fatalf("StageFiles: %v", err)
	}

	// Verify it's staged.
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "staged.txt") {
		t.Errorf("expected staged.txt in staged files, got: %s", out)
	}
}

func TestStageFiles_InvalidFile(t *testing.T) {
	dir := helperInitRepo(t)
	err := StageFiles(dir, []string{"nonexistent-file-xyz.txt"})
	if err == nil {
		t.Fatal("StageFiles should fail for nonexistent file")
	}
}

func TestRebaseContinue_NoRebaseInProgress(t *testing.T) {
	dir := helperInitRepo(t)
	err := RebaseContinue(dir)
	if err == nil {
		t.Fatal("RebaseContinue should fail when no rebase is in progress")
	}
	// Should not be a conflict error.
	if IsConflict(err) {
		t.Error("error should not be a ConflictError")
	}
}

func TestRebaseContinue_AfterResolvingConflict(t *testing.T) {
	dir, mainBranch, _ := setupConflict(t)

	// Start rebase — will conflict.
	err := StartRebase(dir, mainBranch)
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got: %v", err)
	}

	// Resolve the conflict by choosing one side.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("resolved\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", "file.txt")

	// Continue the rebase.
	err = RebaseContinue(dir)
	if err != nil {
		t.Fatalf("RebaseContinue should succeed after resolving conflict, got: %v", err)
	}
}

func TestRebaseAbort_NoRebase(t *testing.T) {
	dir := helperInitRepo(t)
	// Abort when no rebase is in progress should not error (abortRebase silently succeeds).
	err := RebaseAbort(dir)
	if err != nil {
		t.Fatalf("RebaseAbort should succeed even with no rebase in progress, got: %v", err)
	}
}

func TestRebaseAbort_DuringConflict(t *testing.T) {
	dir, mainBranch, _ := setupConflict(t)

	// Start rebase to get into conflict state.
	err := StartRebase(dir, mainBranch)
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got: %v", err)
	}

	// Abort should succeed and leave the worktree clean.
	err = RebaseAbort(dir)
	if err != nil {
		t.Fatalf("RebaseAbort: %v", err)
	}

	// Verify the repo is clean (no rebase in progress).
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "UU") {
		t.Error("expected no conflict markers after abort")
	}
}

func TestAbortRebase(t *testing.T) {
	dir := helperInitRepo(t)
	// abortRebase always returns nil (even if there's nothing to abort).
	err := abortRebase(dir)
	if err != nil {
		t.Fatalf("abortRebase should return nil, got: %v", err)
	}
}

func TestRebaseContinue_MultipleConflicts(t *testing.T) {
	dir := helperInitRepo(t)
	mainBranch := helperRun(t, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")

	// Create topic branch with two conflicting commits.
	helperRun(t, dir, "git", "checkout", "-b", "multi-conflict")

	// First commit on topic.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("topic v1\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "topic commit 1")

	// Second commit on topic.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("topic v2\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "topic commit 2")

	// Conflicting change on main.
	helperRun(t, dir, "git", "checkout", mainBranch)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main v2\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", ".")
	helperRun(t, dir, "git", "commit", "-m", "main conflicting")

	helperRun(t, dir, "git", "checkout", "multi-conflict")

	// Start rebase — first commit will conflict.
	err := StartRebase(dir, mainBranch)
	if !IsConflict(err) {
		t.Fatalf("expected conflict on first commit, got: %v", err)
	}

	// Resolve first conflict.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("resolved v1\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	helperRun(t, dir, "git", "add", "file.txt")

	// Continue — second commit may also conflict.
	err = RebaseContinue(dir)
	if err != nil {
		if IsConflict(err) {
			// Second commit conflicted too — resolve and continue.
			if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("resolved v2\n"), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			helperRun(t, dir, "git", "add", "file.txt")
			err = RebaseContinue(dir)
			if err != nil {
				t.Fatalf("RebaseContinue should succeed after resolving second conflict: %v", err)
			}
		} else {
			t.Fatalf("unexpected error during continue: %v", err)
		}
	}
}
