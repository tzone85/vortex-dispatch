package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
)

// When the senior CLI surfaces a session-limit / capacity notice as SUCCESSFUL
// content (not an error envelope), the resolver must NOT mistake it for a merged
// file and write it to disk — that corrupts the source while the rebase
// "succeeds". It must surface a capacity error so the pipeline pauses-and-resumes
// after the limit resets. This mirrors the guard resolveFileTechLead already has;
// the senior fast path previously lacked it (the common single-file conflict).
func TestRebaseWithResolution_SeniorCapacityContentDoesNotCorruptFile(t *testing.T) {
	_, worktreeDir := setupDivergentRepos(t, true)

	// The senior CLI returns a session-limit notice as ordinary content — it has
	// no conflict markers and matches no chatter marker, so without the capacity
	// guard it would be returned as a clean resolution and written to feature.go.
	notice := llm.CompletionResponse{Content: "You've hit your session limit · resets 3pm. Please try again later."}
	client := llm.NewReplayClient(notice, notice, notice, notice)

	// No tech lead configured (senior-only) — the common single-file path.
	cr := NewConflictResolver(client, "test-model", nil, "", 4096, nil, nil)

	err := cr.RebaseWithResolution(context.Background(), "s-capacity", worktreeDir, "origin/main")
	if err == nil {
		t.Fatal("expected a capacity error, got nil (file would have been corrupted)")
	}
	if !llm.IsCapacityError(err) {
		t.Fatalf("expected IsCapacityError, got: %v", err)
	}

	// The capacity notice must NOT have been written into the source file.
	got, rErr := os.ReadFile(filepath.Join(worktreeDir, "feature.go"))
	if rErr != nil {
		t.Fatal(rErr)
	}
	if strings.Contains(string(got), "session limit") {
		t.Errorf("capacity notice was written into the source file:\n%s", got)
	}

	// The rebase must have been aborted, not left in progress.
	status := exec.Command("git", "status")
	status.Dir = worktreeDir
	out, _ := status.CombinedOutput()
	if strings.Contains(string(out), "rebase in progress") {
		t.Errorf("rebase still in progress after capacity abort:\n%s", out)
	}
}

// When the LLM returns conversational COMMENTARY instead of merged file content,
// the resolver must NOT abort and thrash the story through every escalation tier
// forever (the clipforge/pulsereview failure). Instead it deterministically
// keeps the story-branch version (--theirs) and the rebase completes. The
// pre-merge QA gate validates the result downstream.
func TestRebaseWithResolution_CommentaryFallsBackToStoryVersion(t *testing.T) {
	_, worktreeDir := setupDivergentRepos(t, true)

	// Every resolver call returns commentary (a known chatter marker), which
	// classifies as errUnmergeable → deterministic fallback, not abort.
	commentary := llm.CompletionResponse{Content: "Conflict resolved. Kept both sides merged."}
	client := llm.NewReplayClient(commentary, commentary, commentary, commentary)

	cr := NewConflictResolver(client, "test-model", nil, "", 4096, nil, nil)

	err := cr.RebaseWithResolution(context.Background(), "s-commentary", worktreeDir, "origin/main")
	if err != nil {
		t.Fatalf("expected rebase to succeed via deterministic fallback, got: %v", err)
	}

	// Rebase must be finished, not left in progress.
	status := exec.Command("git", "status")
	status.Dir = worktreeDir
	out, _ := status.CombinedOutput()
	if strings.Contains(string(out), "rebase in progress") {
		t.Errorf("rebase still in progress after fallback:\n%s", out)
	}

	// The story-branch (theirs) version must have won.
	got, err := os.ReadFile(filepath.Join(worktreeDir, "feature.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "feature version") {
		t.Errorf("expected story-branch content after fallback, got:\n%s", got)
	}
	if strings.Contains(string(got), "<<<<<<<") {
		t.Errorf("conflict markers left in file:\n%s", got)
	}
}
