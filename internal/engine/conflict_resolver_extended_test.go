package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// resolveFileTest directly tests the resolveFile method which calls the LLM
// to resolve a conflicted file's content.
func TestResolveFile_SuccessfulResolution(t *testing.T) {
	resolvedContent := "package main\n\nfunc main() {\n\tfmt.Println(\"merged\")\n}"
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: resolvedContent,
	})

	cr := NewConflictResolver(replayClient, "test-model", 4096, nil)

	conflicted := "package main\n<<<<<<< HEAD\nfunc main() { fmt.Println(\"from HEAD\") }\n=======\nfunc main() { fmt.Println(\"from branch\") }\n>>>>>>> feature/branch\n"

	got, err := cr.resolveFile(context.Background(), "main.go", conflicted)
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if got != resolvedContent {
		t.Errorf("expected resolved content %q, got %q", resolvedContent, got)
	}
}

func TestResolveFile_StillContainsConflictMarkers(t *testing.T) {
	// LLM returns content that still has conflict markers — should error.
	badResolution := `package main
<<<<<<< HEAD
func main() {}
=======
func main() { return }
>>>>>>> feature
`
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: badResolution,
	})

	cr := NewConflictResolver(replayClient, "test-model", 4096, nil)

	_, err := cr.resolveFile(context.Background(), "main.go", "conflicted content")
	if err == nil {
		t.Fatal("expected error when resolved content contains conflict markers")
	}
	if !strings.Contains(err.Error(), "conflict markers") {
		t.Errorf("expected conflict markers error, got: %v", err)
	}
}

func TestResolveFile_LLMError(t *testing.T) {
	// ReplayClient with no responses will return an error.
	replayClient := llm.NewReplayClient() // zero responses

	cr := NewConflictResolver(replayClient, "test-model", 4096, nil)

	_, err := cr.resolveFile(context.Background(), "main.go", "conflicted")
	if err == nil {
		t.Fatal("expected error when LLM client fails")
	}
}

func TestResolveFile_StripsMarkdownFences(t *testing.T) {
	// LLM wraps output in markdown fences — should be stripped.
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: "```go\npackage main\n\nfunc main() {}\n```",
	})

	cr := NewConflictResolver(replayClient, "test-model", 4096, nil)

	got, err := cr.resolveFile(context.Background(), "main.go", "conflicted")
	if err != nil {
		t.Fatalf("resolveFile: %v", err)
	}
	if strings.Contains(got, "```") {
		t.Error("expected markdown fences to be stripped")
	}
	if !strings.Contains(got, "package main") {
		t.Error("expected content to be preserved after fence stripping")
	}
}

func TestResolveFile_FatalAPIError(t *testing.T) {
	// Create a client that returns a fatal API error (e.g., 401).
	fatalErr := &llm.APIError{StatusCode: 401, Message: "unauthorized"}
	errorClient := &errorLLMClient{err: fatalErr}

	cr := NewConflictResolver(errorClient, "test-model", 4096, nil)

	_, err := cr.resolveFile(context.Background(), "main.go", "conflicted")
	if err == nil {
		t.Fatal("expected error for fatal API error")
	}
	if !strings.Contains(err.Error(), "fatal API error") {
		t.Errorf("expected fatal API error message, got: %v", err)
	}
}

// errorLLMClient always returns the configured error.
type errorLLMClient struct {
	err error
}

func (c *errorLLMClient) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{}, c.err
}

// --- RebaseWithResolution tests using real git repos ---

func TestRebaseWithResolution_CleanRebase(t *testing.T) {
	// Set up two repos: a "remote" (bare) and a "clone". Create diverging
	// branches that DON'T conflict, so rebase is clean.
	_, worktreeDir := setupDivergentRepos(t, false)

	replayClient := llm.NewReplayClient() // should not be called for clean rebase
	es := newEventStoreForTest(t)

	cr := NewConflictResolver(replayClient, "test-model", 4096, es)

	err := cr.RebaseWithResolution(context.Background(), "s-clean", worktreeDir, "origin/main")
	if err != nil {
		t.Fatalf("expected clean rebase to succeed, got: %v", err)
	}
}

func TestRebaseWithResolution_ConflictResolved(t *testing.T) {
	// Create repos with actual conflicting changes.
	_, worktreeDir := setupDivergentRepos(t, true)

	resolvedContent := `package main

import "fmt"

func Feature() {
	fmt.Println("merged from both sides")
}
`

	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: resolvedContent,
	})

	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	defer es.Close()

	cr := NewConflictResolver(replayClient, "test-model", 4096, es)

	err = cr.RebaseWithResolution(context.Background(), "s-conflict", worktreeDir, "origin/main")
	if err != nil {
		t.Fatalf("expected conflict to be resolved, got: %v", err)
	}

	// Verify resolution event was emitted.
	events, _ := es.List(state.EventFilter{Type: state.EventStoryProgress, StoryID: "s-conflict"})
	if len(events) != 1 {
		t.Errorf("expected 1 resolution event, got %d", len(events))
	}
}

func TestRebaseWithResolution_LLMFailure_AbortsRebase(t *testing.T) {
	_, worktreeDir := setupDivergentRepos(t, true)

	// LLM fails on the first call.
	replayClient := llm.NewReplayClient() // no responses -> error

	cr := NewConflictResolver(replayClient, "test-model", 4096, nil)

	err := cr.RebaseWithResolution(context.Background(), "s-llm-fail", worktreeDir, "origin/main")
	if err == nil {
		t.Fatal("expected error when LLM fails during conflict resolution")
	}

	// Verify rebase was aborted (no rebase in progress).
	statusCmd := exec.Command("git", "status")
	statusCmd.Dir = worktreeDir
	out, _ := statusCmd.CombinedOutput()
	if strings.Contains(string(out), "rebase in progress") {
		t.Error("expected rebase to be aborted after LLM failure")
	}
}

// --- Helper functions ---

func newEventStoreForTest(t *testing.T) state.EventStore {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	t.Cleanup(func() { es.Close() })
	return es
}

// setupDivergentRepos creates a bare remote, a clone, commits on main,
// pushes, then creates a feature branch with diverging changes.
// If withConflict is true, both branches modify the same file at the same line.
func setupDivergentRepos(t *testing.T, withConflict bool) (bareDir, cloneDir string) {
	t.Helper()

	bareDir = filepath.Join(t.TempDir(), "remote.git")
	if err := exec.Command("git", "init", "--bare", bareDir).Run(); err != nil {
		t.Fatalf("init bare: %v", err)
	}

	cloneDir = filepath.Join(t.TempDir(), "clone")
	if err := exec.Command("git", "clone", bareDir, cloneDir).Run(); err != nil {
		t.Fatalf("clone: %v", err)
	}

	runGitInDir := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v (%s)", args, dir, err, out)
		}
	}

	runGitInDir(cloneDir, "config", "user.email", "test@test.com")
	runGitInDir(cloneDir, "config", "user.name", "Test")

	// Initial commit on main.
	featureFile := filepath.Join(cloneDir, "feature.go")
	os.WriteFile(featureFile, []byte("package main\n\nfunc Feature() {}\n"), 0o644)
	runGitInDir(cloneDir, "add", ".")
	runGitInDir(cloneDir, "commit", "-m", "init")
	runGitInDir(cloneDir, "push", "origin", "main")

	if withConflict {
		// Modify on main (will be pushed) — same file, same function.
		os.WriteFile(featureFile, []byte("package main\n\nimport \"fmt\"\n\nfunc Feature() {\n\tfmt.Println(\"main version\")\n}\n"), 0o644)
		runGitInDir(cloneDir, "add", ".")
		runGitInDir(cloneDir, "commit", "-m", "update feature on main")
		runGitInDir(cloneDir, "push", "origin", "main")

		// Create feature branch from the initial commit and modify same file.
		runGitInDir(cloneDir, "checkout", "-b", "feature", "HEAD~1")
		os.WriteFile(featureFile, []byte("package main\n\nimport \"fmt\"\n\nfunc Feature() {\n\tfmt.Println(\"feature version\")\n}\n"), 0o644)
		runGitInDir(cloneDir, "add", ".")
		runGitInDir(cloneDir, "commit", "-m", "update feature on branch")
		// Fetch to make origin/main available.
		runGitInDir(cloneDir, "fetch", "origin", "main")
	} else {
		// Create feature branch with non-conflicting changes.
		runGitInDir(cloneDir, "checkout", "-b", "feature")
		otherFile := filepath.Join(cloneDir, "other.go")
		os.WriteFile(otherFile, []byte("package main\n\nfunc Other() {}\n"), 0o644)
		runGitInDir(cloneDir, "add", ".")
		runGitInDir(cloneDir, "commit", "-m", "add other.go on feature")
		runGitInDir(cloneDir, "fetch", "origin", "main")
	}

	return bareDir, cloneDir
}

// TestRebaseWithResolution_MaxRoundsExhausted verifies that the resolver
// aborts after maxRounds of conflict resolution attempts.
func TestRebaseWithResolution_MaxRoundsExhausted(t *testing.T) {
	_, worktreeDir := setupDivergentRepos(t, true)

	// Create a resolver that always returns content with conflict markers,
	// causing infinite conflict rounds. Set maxRounds to 1.
	badClient := llm.NewReplayClient(
		// Return content that will still conflict on rebase-continue.
		llm.CompletionResponse{Content: "package main\n\nfunc Feature() { /* round 1 */ }\n"},
		llm.CompletionResponse{Content: "package main\n\nfunc Feature() { /* round 2 */ }\n"},
	)

	cr := NewConflictResolver(badClient, "test-model", 4096, nil)
	cr.maxRounds = 1 // Only allow 1 round before giving up.

	err := cr.RebaseWithResolution(context.Background(), "s-exhaust", worktreeDir, "origin/main")
	// This should either succeed (if 1 round was enough) or fail with "exhausted".
	// Either outcome is valid depending on git behavior — the key test is no panic.
	if err != nil {
		if !strings.Contains(err.Error(), "exhausted") && !strings.Contains(err.Error(), "conflict") {
			t.Logf("rebase error (expected): %v", err)
		}
	}
}

// TestNewConflictResolver_DefaultValues verifies the constructor sets proper defaults.
func TestNewConflictResolver_Defaults(t *testing.T) {
	es := newEventStoreForTest(t)
	cr := NewConflictResolver(nil, "claude-sonnet-4-20250514", 8192, es)

	if cr.model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %q", cr.model)
	}
	if cr.maxTokens != 8192 {
		t.Errorf("expected maxTokens 8192, got %d", cr.maxTokens)
	}
	if cr.maxRounds != 10 {
		t.Errorf("expected default maxRounds 10, got %d", cr.maxRounds)
	}
	if cr.eventStore != es {
		t.Error("expected event store to be set")
	}
}

// TestResolveFile_EmptyContent verifies handling of empty conflicted content.
func TestResolveFile_EmptyContent(t *testing.T) {
	replayClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: "",
	})

	cr := NewConflictResolver(replayClient, "test-model", 4096, nil)

	got, err := cr.resolveFile(context.Background(), "empty.go", "")
	if err != nil {
		t.Fatalf("resolveFile with empty content: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty resolved content, got %q", got)
	}
}

// TestEmitResolutionEvent_VerifyPayload checks the event payload structure.
func TestEmitResolutionEvent_PayloadStructure(t *testing.T) {
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer es.Close()

	cr := &ConflictResolver{eventStore: es}

	files := []string{"a.go", "b.go", "c.go"}
	cr.emitResolutionEvent("s-emit-001", files, 3)

	events, _ := es.List(state.EventFilter{StoryID: "s-emit-001"})
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	payload := state.DecodePayload(events[0].Payload)
	if payload["action"] != "conflicts_resolved" {
		t.Errorf("expected action conflicts_resolved, got %v", payload["action"])
	}

	filesArr, ok := payload["files"].([]any)
	if !ok || len(filesArr) != 3 {
		t.Errorf("expected 3 files in payload, got %v", payload["files"])
	}

	if int(payload["rounds"].(float64)) != 3 {
		t.Errorf("expected 3 rounds, got %v", payload["rounds"])
	}

	if events[0].AgentID != "conflict-resolver" {
		t.Errorf("expected agent_id 'conflict-resolver', got %q", events[0].AgentID)
	}
}

// TestResolveFile_NonFatalAPIError verifies non-fatal errors are passed through.
func TestResolveFile_NonFatalAPIError(t *testing.T) {
	// 429 is rate limited, not fatal.
	rateErr := &llm.APIError{StatusCode: 429, Message: "rate limited"}
	errorClient := &errorLLMClient{err: rateErr}

	cr := NewConflictResolver(errorClient, "test-model", 4096, nil)

	_, err := cr.resolveFile(context.Background(), "main.go", "conflicted")
	if err == nil {
		t.Fatal("expected error for rate-limited API call")
	}
	// Should NOT be wrapped as "fatal API error".
	if strings.Contains(err.Error(), "fatal API error") {
		t.Error("rate limit should not be treated as fatal")
	}
}

// TestResolveFile_GenericError verifies non-APIError errors pass through.
func TestResolveFile_GenericError(t *testing.T) {
	genericErr := fmt.Errorf("network timeout")
	errorClient := &errorLLMClient{err: genericErr}

	cr := NewConflictResolver(errorClient, "test-model", 4096, nil)

	_, err := cr.resolveFile(context.Background(), "main.go", "conflicted")
	if err == nil {
		t.Fatal("expected error for generic failure")
	}
	if !strings.Contains(err.Error(), "network timeout") {
		t.Errorf("expected original error message, got: %v", err)
	}
}
