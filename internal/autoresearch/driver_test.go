package autoresearch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/runtime"
)

// stubRuntime is a deterministic Runtime suitable for unit tests. Spawn
// is a no-op; DetectStatus returns whatever the test scripts.
type stubRuntime struct {
	spawned   []runtime.SessionConfig
	terminated []string
	statuses  []runtime.AgentStatus
	statusIdx int
}

func (s *stubRuntime) Spawn(cfg runtime.SessionConfig) error {
	s.spawned = append(s.spawned, cfg)
	return nil
}
func (s *stubRuntime) Terminate(id string) error {
	s.terminated = append(s.terminated, id)
	return nil
}
func (s *stubRuntime) SendInput(_, _ string) error              { return nil }
func (s *stubRuntime) ReadOutput(_ string, _ int) (string, error) { return "", nil }
func (s *stubRuntime) DetectStatus(_ string) (runtime.AgentStatus, error) {
	if s.statusIdx < len(s.statuses) {
		st := s.statuses[s.statusIdx]
		s.statusIdx++
		return st, nil
	}
	return runtime.StatusWorking, nil
}
func (s *stubRuntime) Name() string                { return "stub" }
func (s *stubRuntime) SupportedModels() []string   { return nil }

func newGitWorktree(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	must := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v (%s)", args, err, string(out))
		}
	}
	must("git", "init", "-q")
	must("git", "config", "user.email", "test@example.com")
	must("git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	must("git", "add", "-A")
	must("git", "commit", "-q", "-m", "init")
	return dir, "main"
}

func TestLiveAgentDriver_StatusDoneTerminatesSession(t *testing.T) {
	rt := &stubRuntime{statuses: []runtime.AgentStatus{runtime.StatusWorking, runtime.StatusDone}}
	d := &LiveAgentDriver{
		Runtime:      rt,
		Model:        "test-model",
		PollInterval: 5 * time.Millisecond,
	}
	worktree, _ := newGitWorktree(t)

	if err := d.RunAgent(context.Background(), worktree, "main", "do work", time.Second); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if len(rt.spawned) != 1 {
		t.Errorf("expected 1 spawn, got %d", len(rt.spawned))
	}
	if len(rt.terminated) != 1 {
		t.Errorf("expected 1 terminate, got %d", len(rt.terminated))
	}
}

func TestLiveAgentDriver_BudgetTimeoutTerminates(t *testing.T) {
	rt := &stubRuntime{} // never reports done
	d := &LiveAgentDriver{
		Runtime:      rt,
		Model:        "test-model",
		PollInterval: 5 * time.Millisecond,
	}
	worktree, _ := newGitWorktree(t)
	err := d.RunAgent(context.Background(), worktree, "main", "do work", 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected budget-elapsed error")
	}
	if len(rt.terminated) != 1 {
		t.Error("session must be terminated when budget elapses")
	}
}

func TestLiveAgentDriver_ContextCancelTerminates(t *testing.T) {
	rt := &stubRuntime{}
	d := &LiveAgentDriver{
		Runtime:      rt,
		Model:        "test-model",
		PollInterval: 5 * time.Millisecond,
	}
	worktree, _ := newGitWorktree(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if err := d.RunAgent(ctx, worktree, "main", "do work", time.Hour); err == nil {
		t.Fatal("expected context cancellation error")
	}
	if len(rt.terminated) != 1 {
		t.Error("session must be terminated on context cancel")
	}
}

func TestLiveAgentDriver_NilRuntime(t *testing.T) {
	d := &LiveAgentDriver{}
	if err := d.RunAgent(context.Background(), "/tmp", "main", "x", time.Second); err == nil {
		t.Error("nil runtime must error")
	}
}

func TestLiveAgentDriver_DiffAndPaths(t *testing.T) {
	worktree, _ := newGitWorktree(t)
	// Create a feature branch and a change.
	exec.Command("git", "-C", worktree, "checkout", "-q", "-b", "feat/x").Run()
	if err := os.WriteFile(filepath.Join(worktree, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runIn(worktree, "git", "add", "-A"); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := runIn(worktree, "git", "commit", "-q", "-m", "feat"); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}

	d := &LiveAgentDriver{}
	diff, err := d.Diff(worktree, "main")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if diff == "" {
		t.Error("Diff must return a non-empty patch")
	}
	paths, err := d.PathsTouched(worktree, "main")
	if err != nil {
		t.Fatalf("PathsTouched: %v", err)
	}
	want := "new.txt"
	found := false
	for _, p := range paths {
		if p == want {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s in PathsTouched, got %v", want, paths)
	}
}

func TestLiveAgentDriver_AutoCommitCapturesPartialWork(t *testing.T) {
	worktree, _ := newGitWorktree(t)
	// Write a file but DON'T commit; mimic an agent that was killed
	// mid-edit. autoCommit should sweep it into a commit.
	exec.Command("git", "-C", worktree, "checkout", "-q", "-b", "feat/x").Run()
	if err := os.WriteFile(filepath.Join(worktree, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}

	autoCommit(worktree, "feat/x")

	out, err := runIn(worktree, "git", "log", "--oneline")
	if err != nil {
		t.Fatalf("git log: %v %s", err, out)
	}
	if !contains(out, "autoresearch: agent edits") {
		t.Errorf("auto-commit must have created a commit; log:\n%s", out)
	}
}

func TestSessionNameSanitisesBranch(t *testing.T) {
	got := autoresearchSessionName("autoresearch/exp-01ABC")
	want := "ar-autoresearch-exp-01ABC"
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
	got2 := autoresearchSessionName("foo:bar.baz")
	if !contains(got2, "foo-bar-baz") {
		t.Errorf("expected colons and dots to be replaced; got %s", got2)
	}
}
