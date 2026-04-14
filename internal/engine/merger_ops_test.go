package engine

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// mockGitHubOps implements GitHubOps for testing.
type mockGitHubOps struct {
	pushErr     error
	createPRErr error
	createPR    PRCreationResult
	mergeErr    error
}

func (m *mockGitHubOps) PushBranch(_, _ string) error {
	return m.pushErr
}

func (m *mockGitHubOps) CreatePR(_, _, _, _, _ string) (PRCreationResult, error) {
	return m.createPR, m.createPRErr
}

func (m *mockGitHubOps) MergePR(_ string, _ int) error {
	return m.mergeErr
}

func newMergerTestStores(t *testing.T) (state.EventStore, *state.SQLiteStore, func()) {
	t.Helper()
	dir := t.TempDir()
	es, err := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("create event store: %v", err)
	}
	ps, err := state.NewSQLiteStore(filepath.Join(dir, "proj.db"))
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	cleanup := func() {
		es.Close()
		ps.Close()
	}
	return es, ps, cleanup
}

func TestCreatePROnly_Success(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	// Create story
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{
		createPR: PRCreationResult{Number: 42, URL: "https://github.com/test/pr/42"},
	}
	m := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	result, err := m.CreatePROnly("s-001", "My Task", "/tmp/repo", "feat/s-001")
	if err != nil {
		t.Fatalf("CreatePROnly: %v", err)
	}
	if result.PRNumber != 42 {
		t.Errorf("expected PR number 42, got %d", result.PRNumber)
	}
	if result.Merged {
		t.Error("expected Merged=false for CreatePROnly")
	}

	// Verify PR_CREATED event
	events, _ := es.List(state.EventFilter{Type: state.EventStoryPRCreated, StoryID: "s-001"})
	if len(events) != 1 {
		t.Errorf("expected 1 PR_CREATED event, got %d", len(events))
	}
}

func TestCreatePROnly_PushFails(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	ghOps := &mockGitHubOps{pushErr: fmt.Errorf("push failed")}
	m := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	_, err := m.CreatePROnly("s-001", "Task", "/tmp/repo", "feat/s-001")
	if err == nil {
		t.Error("expected error when push fails")
	}
}

func TestCreatePROnly_CreatePRFails(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	ghOps := &mockGitHubOps{createPRErr: fmt.Errorf("create PR failed")}
	m := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	_, err := m.CreatePROnly("s-001", "Task", "/tmp/repo", "feat/s-001")
	if err == nil {
		t.Error("expected error when create PR fails")
	}
}

func TestMergeExistingPR_Success(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	// Create story with PR
	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	prEvt := state.NewEvent(state.EventStoryPRCreated, "merger", "s-001", map[string]any{
		"pr_number": 42, "pr_url": "https://github.com/pr/42", "branch": "feat/s-001",
	})
	es.Append(prEvt)
	ps.Project(prEvt)

	ghOps := &mockGitHubOps{}
	m := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	err := m.MergeExistingPR("s-001", "/tmp/repo")
	if err != nil {
		t.Fatalf("MergeExistingPR: %v", err)
	}

	// Verify STORY_MERGED event
	events, _ := es.List(state.EventFilter{Type: state.EventStoryMerged, StoryID: "s-001"})
	if len(events) != 1 {
		t.Errorf("expected 1 MERGED event, got %d", len(events))
	}
}

func TestMergeExistingPR_NoPR(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{}
	m := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	err := m.MergeExistingPR("s-001", "/tmp/repo")
	if err == nil {
		t.Error("expected error for story with no PR")
	}
}

func TestMergeExistingPR_MergeFails(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	prEvt := state.NewEvent(state.EventStoryPRCreated, "merger", "s-001", map[string]any{
		"pr_number": 42, "pr_url": "https://github.com/pr/42", "branch": "feat/s-001",
	})
	es.Append(prEvt)
	ps.Project(prEvt)

	ghOps := &mockGitHubOps{mergeErr: fmt.Errorf("merge conflict")}
	m := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	err := m.MergeExistingPR("s-001", "/tmp/repo")
	if err == nil {
		t.Error("expected error when merge fails")
	}
}

func TestMergeExistingPR_UnknownStory(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	ghOps := &mockGitHubOps{}
	m := NewMerger(config.MergeConfig{BaseBranch: "main"}, ghOps, es, ps)

	err := m.MergeExistingPR("nonexistent", "/tmp/repo")
	if err == nil {
		t.Error("expected error for unknown story")
	}
}

func TestMerge_WithAutoMerge(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{
		createPR: PRCreationResult{Number: 10, URL: "https://github.com/pr/10"},
	}
	m := NewMerger(config.MergeConfig{AutoMerge: true, BaseBranch: "main"}, ghOps, es, ps)

	result, err := m.Merge("s-001", "Task", "/tmp/repo", "feat/s-001")
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !result.Merged {
		t.Error("expected Merged=true with AutoMerge")
	}
	if result.PRNumber != 10 {
		t.Errorf("expected PR 10, got %d", result.PRNumber)
	}
}

func TestMerge_WithoutAutoMerge(t *testing.T) {
	es, ps, cleanup := newMergerTestStores(t)
	defer cleanup()

	storyEvt := state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "Task", "description": "desc", "complexity": 2,
	})
	es.Append(storyEvt)
	ps.Project(storyEvt)

	ghOps := &mockGitHubOps{
		createPR: PRCreationResult{Number: 10, URL: "https://github.com/pr/10"},
	}
	m := NewMerger(config.MergeConfig{AutoMerge: false, BaseBranch: "main"}, ghOps, es, ps)

	result, err := m.Merge("s-001", "Task", "/tmp/repo", "feat/s-001")
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Merged {
		t.Error("expected Merged=false without AutoMerge")
	}
}
