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
