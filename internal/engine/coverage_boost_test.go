package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// --- extractRepoName tests ---

func TestExtractRepoName_HTTPSUrl(t *testing.T) {
	got := extractRepoName("https://github.com/tzone85/vortex-dispatch.git")
	if got != "vortex-dispatch" {
		t.Errorf("expected vortex-dispatch, got %q", got)
	}
}

func TestExtractRepoName_SSHUrl(t *testing.T) {
	got := extractRepoName("git@github.com:tzone85/vortex-dispatch.git")
	if got != "vortex-dispatch" {
		t.Errorf("expected vortex-dispatch, got %q", got)
	}
}

func TestExtractRepoName_PlainName(t *testing.T) {
	got := extractRepoName("myrepo")
	if got != "myrepo" {
		t.Errorf("expected myrepo, got %q", got)
	}
}

func TestExtractRepoName_TrailingDotGit(t *testing.T) {
	got := extractRepoName("https://github.com/org/repo.git")
	if got != "repo" {
		t.Errorf("expected repo, got %q", got)
	}
}

func TestExtractRepoName_EmptyString(t *testing.T) {
	got := extractRepoName("")
	if got != "unnamed" {
		t.Errorf("expected unnamed, got %q", got)
	}
}

func TestExtractRepoName_NestedPathDeep(t *testing.T) {
	got := extractRepoName("https://gitlab.com/group/subgroup/project.git")
	if got != "project" {
		t.Errorf("expected project, got %q", got)
	}
}

// --- WriteMetadata tests ---

func TestWriteMetadata_Success(t *testing.T) {
	dir := t.TempDir()
	err := WriteMetadata(dir, ProjectMetadata{
		Name:      "test-project",
		RepoPath:  "/tmp/repo",
		RemoteURL: "https://github.com/test/repo",
	})
	if err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}

	// Verify file was written.
	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if meta.Name != "test-project" {
		t.Errorf("expected name test-project, got %q", meta.Name)
	}
}

func TestWriteMetadata_InvalidDir(t *testing.T) {
	err := WriteMetadata("/nonexistent/path/that/does/not/exist", ProjectMetadata{
		Name: "test",
	})
	if err == nil {
		t.Error("expected error for invalid directory")
	}
}

// --- WriteCheckpoint edge cases ---

func TestWriteCheckpoint_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.json")

	cp := Checkpoint{
		ReqID:        "r-001",
		Phase:        PhaseMerging,
		MergingStory: "s-001",
		PID:          12345,
	}
	err := WriteCheckpoint(path, cp)
	if err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// Read it back.
	got, err := ReadCheckpoint(path)
	if err != nil {
		t.Fatalf("ReadCheckpoint: %v", err)
	}
	if got.ReqID != "r-001" {
		t.Errorf("expected req_id r-001, got %q", got.ReqID)
	}
	if got.Phase != PhaseMerging {
		t.Errorf("expected phase merging, got %q", got.Phase)
	}
	if got.MergingStory != "s-001" {
		t.Errorf("expected merging_story s-001, got %q", got.MergingStory)
	}
}

func TestWriteCheckpoint_InvalidPath(t *testing.T) {
	err := WriteCheckpoint("/nonexistent/deeply/nested/path/checkpoint.json", Checkpoint{})
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

// --- Merger tests ---

func TestMerger_Merge_PushFails(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-m-push", map[string]any{
		"id": "s-m-push", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{pushErr: fmt.Errorf("push failed")}
	merger := NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)

	_, err := merger.Merge("s-m-push", "Task", "/tmp/repo", "feature")
	if err == nil {
		t.Fatal("expected error when push fails")
	}
}

func TestMerger_Merge_CreatePRFails(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-m-pr", map[string]any{
		"id": "s-m-pr", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{createPRErr: fmt.Errorf("PR creation failed")}
	merger := NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)

	_, err := merger.Merge("s-m-pr", "Task", "/tmp/repo", "feature")
	if err == nil {
		t.Fatal("expected error when PR creation fails")
	}
}

func TestMerger_Merge_AutoMergeFails(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-m-amf", map[string]any{
		"id": "s-m-amf", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{
		createPR: PRCreationResult{Number: 10, URL: "https://github.com/test/pull/10"},
		mergeErr: fmt.Errorf("merge conflict"),
	}
	merger := NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)

	result, err := merger.Merge("s-m-amf", "Task", "/tmp/repo", "feature")
	if err == nil {
		t.Fatal("expected error when auto-merge fails")
	}
	// PR should be created even though merge failed.
	if result.PRNumber != 10 {
		t.Errorf("expected PR number 10, got %d", result.PRNumber)
	}
	if result.Merged {
		t.Error("expected Merged to be false")
	}
}

func TestMerger_Merge_AutoMergeDisabled(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-m-noam", map[string]any{
		"id": "s-m-noam", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{
		createPR: PRCreationResult{Number: 5, URL: "https://github.com/test/pull/5"},
	}
	merger := NewMerger(config.MergeConfig{AutoMerge: false, BaseBranch: "main"}, ghOps, es, ps)

	result, err := merger.Merge("s-m-noam", "Task", "/tmp/repo", "feature")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Merged {
		t.Error("expected Merged to be false when auto-merge is disabled")
	}
	if result.PRNumber != 5 {
		t.Errorf("expected PR number 5, got %d", result.PRNumber)
	}

	// No STORY_MERGED event should exist.
	mergedEvents, _ := es.List(state.EventFilter{Type: state.EventStoryMerged, StoryID: "s-m-noam"})
	if len(mergedEvents) != 0 {
		t.Errorf("expected 0 STORY_MERGED events, got %d", len(mergedEvents))
	}
}

// --- PostExecutionPipeline with merger and review gate ---

func TestPostExecutionPipeline_WithMerger_ManualReviewGate(t *testing.T) {
	es, ps, cleanup := newPostExecTestStores(t)
	defer cleanup()

	reqEvt := state.NewEvent(state.EventReqSubmitted, "cli", "", map[string]any{
		"id": "r-manual", "title": "Req", "description": "desc",
	})
	es.Append(reqEvt)
	ps.Project(reqEvt)

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-pe-manual", map[string]any{
		"id": "s-pe-manual", "req_id": "r-manual", "title": "Task",
		"description": "desc", "complexity": 3,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	_, worktreePath := setupWorktreeWithDiff(t)

	reviewClient := llm.NewReplayClient(llm.CompletionResponse{
		Content: `{"passed": true, "comments": [], "summary": "ok"}`,
	})
	reviewer := NewReviewer(reviewClient, "test-model", 4000, es, ps)

	qaRunner := &internalMockRunner{results: map[string]mockResult{
		"go": {output: "ok", err: nil},
	}}
	qa := NewQA(QAConfig{BuildCommand: "go build ./..."}, qaRunner, es, ps)

	mockGH := &mockGitHubOps{
		createPR: PRCreationResult{Number: 99, URL: "https://github.com/test/pull/99"},
	}
	merger := NewMerger(config.MergeConfig{
		AutoMerge:  true,
		BaseBranch: "main",
	}, mockGH, es, ps)

	cfg := config.Config{
		Routing: config.RoutingConfig{MaxRetriesBeforeEscalation: 2},
		Merge:   config.MergeConfig{BaseBranch: "main", AutoMerge: true, ReviewMode: "manual"},
	}
	m := NewMonitor(nil, nil, reviewer, qa, merger, cfg, es, ps)

	// Set review gate with manual mode.
	rg := NewReviewGate(es)
	m.SetReviewGate(rg)

	ag := ActiveAgent{
		Assignment: Assignment{
			StoryID:     "s-pe-manual",
			AgentID:     "agent-1",
			SessionName: "vxd-test-manual",
			Branch:      "vxd/s-pe-manual",
		},
		WorktreePath: worktreePath,
	}

	m.postExecutionPipeline(context.Background(), ag, worktreePath)

	// Should have created PR and paused for approval.
	awaitEvents, _ := es.List(state.EventFilter{Type: state.EventStoryAwaitingApproval, StoryID: "s-pe-manual"})
	if len(awaitEvents) != 1 {
		t.Errorf("expected 1 STORY_AWAITING_APPROVAL, got %d", len(awaitEvents))
	}

	// Story should be in awaiting_approval state.
	story, _ := ps.GetStory("s-pe-manual")
	if story.Status != "awaiting_approval" {
		t.Errorf("expected story status awaiting_approval, got %q", story.Status)
	}
}

// --- RebaseWithResolution with more internal paths ---

func TestRebaseWithResolution_NonConflictError(t *testing.T) {
	// Create a repo where rebase fails with a non-conflict error.
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	cmd.Run()

	runGitIn := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.CombinedOutput()
	}

	runGitIn("config", "user.email", "test@test.com")
	runGitIn("config", "user.name", "Test")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644)
	runGitIn("add", ".")
	runGitIn("commit", "-m", "init")

	cr := NewConflictResolver(nil, "test-model", 4096, nil)

	// Try to rebase onto a non-existent ref — should fail with non-conflict error.
	err := cr.RebaseWithResolution(context.Background(), "s-test", dir, "nonexistent-ref")
	if err == nil {
		t.Error("expected error for non-existent upstream ref")
	}
}

// --- Merger.MergeExistingPR tests ---

func TestMerger_MergeExistingPR_Success(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	// Create story with PR number.
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-mepr", map[string]any{
		"id": "s-mepr", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	// Set PR number via PR_CREATED event.
	prEvt := state.NewEvent(state.EventStoryPRCreated, "merger", "s-mepr", map[string]any{
		"pr_number": 50,
		"pr_url":    "https://github.com/test/pull/50",
		"branch":    "feature",
	})
	es.Append(prEvt)
	ps.Project(prEvt)

	ghOps := &mockGitHubOps{}
	merger := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	err := merger.MergeExistingPR("s-mepr", "/tmp/repo")
	if err != nil {
		t.Fatalf("MergeExistingPR: %v", err)
	}

	// Verify STORY_MERGED event.
	mergedEvents, _ := es.List(state.EventFilter{Type: state.EventStoryMerged, StoryID: "s-mepr"})
	if len(mergedEvents) != 1 {
		t.Errorf("expected 1 STORY_MERGED, got %d", len(mergedEvents))
	}
}

func TestMerger_MergeExistingPR_NoPR(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-nopr", map[string]any{
		"id": "s-nopr", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{}
	merger := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	err := merger.MergeExistingPR("s-nopr", "/tmp/repo")
	if err == nil {
		t.Fatal("expected error when story has no PR")
	}
}

func TestMerger_MergeExistingPR_StoryNotFound(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	ghOps := &mockGitHubOps{}
	merger := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	err := merger.MergeExistingPR("nonexistent", "/tmp/repo")
	if err == nil {
		t.Fatal("expected error for non-existent story")
	}
}

// --- Merger.CreatePROnly tests ---

func TestMerger_CreatePROnly_Success(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-pronly", map[string]any{
		"id": "s-pronly", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{
		createPR: PRCreationResult{Number: 77, URL: "https://github.com/test/pull/77"},
	}
	merger := NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)

	result, err := merger.CreatePROnly("s-pronly", "Task", "/tmp/repo", "feature")
	if err != nil {
		t.Fatalf("CreatePROnly: %v", err)
	}
	if result.Merged {
		t.Error("expected Merged to be false for CreatePROnly")
	}
	if result.PRNumber != 77 {
		t.Errorf("expected PR number 77, got %d", result.PRNumber)
	}
}

func TestMerger_CreatePROnly_PushFails(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	ghOps := &mockGitHubOps{pushErr: fmt.Errorf("push failed")}
	merger := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	_, err := merger.CreatePROnly("s-x", "Task", "/tmp/repo", "feature")
	if err == nil {
		t.Fatal("expected error when push fails")
	}
}

// --- Merger.buildPRBody tests ---

func TestMerger_BuildPRBody_WithTemplate(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-tmpl", map[string]any{
		"id": "s-tmpl", "req_id": "r-001", "title": "Task",
		"description": "detailed desc", "complexity": 3,
		"acceptance_criteria": "must pass tests",
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	merger := NewMerger(config.MergeConfig{
		BaseBranch: "main",
		PRTemplate: "Story: {story_id}\nDesc: {description}\nAC: {acceptance_criteria}",
	}, nil, es, ps)

	body := merger.buildPRBody("s-tmpl", "Task")
	if body == "" {
		t.Fatal("expected non-empty PR body")
	}
	if !contains(body, "s-tmpl") {
		t.Error("expected story ID in body")
	}
	if !contains(body, "detailed desc") {
		t.Error("expected description in body")
	}
}

func TestMerger_BuildPRBody_NoTemplate(t *testing.T) {
	merger := NewMerger(config.MergeConfig{BaseBranch: "main"}, nil, nil, nil)

	body := merger.buildPRBody("s-001", "My Story")
	if body == "" {
		t.Fatal("expected non-empty PR body")
	}
	if !contains(body, "s-001") {
		t.Error("expected story ID in default body")
	}
}

// --- ExecRunner.Run test ---

func TestExecRunner_Run_Success(t *testing.T) {
	runner := &ExecRunner{}
	output, err := runner.Run(context.Background(), t.TempDir(), "echo", "hello")
	if err != nil {
		t.Fatalf("ExecRunner.Run: %v", err)
	}
	if !contains(output, "hello") {
		t.Errorf("expected output to contain hello, got %q", output)
	}
}

func TestExecRunner_Run_Failure(t *testing.T) {
	runner := &ExecRunner{}
	_, err := runner.Run(context.Background(), t.TempDir(), "false")
	if err == nil {
		t.Error("expected error for failing command")
	}
}

// --- countRetries edge case ---

func TestCountRetries_NoEvents(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	builder := NewReportBuilder(es, ps, config.Config{})
	count, err := builder.countRetries("s-nonexistent")
	if err != nil {
		t.Fatalf("countRetries: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 retries for nonexistent story, got %d", count)
	}
}

func TestCountRetries_WithEvents(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	// Emit some failure events.
	for i := 0; i < 3; i++ {
		es.Append(state.NewEvent(state.EventStoryReviewFailed, "reviewer", "s-retry-count", map[string]any{
			"reason": "failure",
		}))
	}
	es.Append(state.NewEvent(state.EventStoryQAFailed, "qa", "s-retry-count", map[string]any{
		"reason": "qa failure",
	}))

	builder := NewReportBuilder(es, ps, config.Config{})
	count, err := builder.countRetries("s-retry-count")
	if err != nil {
		t.Fatalf("countRetries: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 retries, got %d", count)
	}
}
