package autoresearch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/runtime"
)

// LiveAgentDriver is the production AgentDriver. It spawns an agent in a
// tmux session via the configured runtime.Runtime, polls until the session
// reports done (or budget elapses), then auto-commits any uncommitted
// changes the agent left in the worktree.
//
// Diff and PathsTouched delegate to git plumbing inside the worktree.
type LiveAgentDriver struct {
	Runtime      runtime.Runtime  // tmux-backed CLIRuntime in production
	Model        string           // e.g. "claude-sonnet-4-20250514"
	SystemPrompt string           // optional system prompt; the per-experiment prompt is the Goal
	PollInterval time.Duration    // how often to check status; defaults to 5s
	LogDir       string           // where to drop tmux capture logs (one per experiment)
	Now          func() time.Time // testable clock
}

// RunAgent spawns the agent on the worktree, polls until done or context
// expires, then auto-commits any leftover edits onto `branch`.
//
// On context cancellation/budget timeout, the tmux session is forcibly
// terminated and any partial work is still auto-committed so the diff
// reflects what the agent actually wrote — runner uses that to decide
// kept vs discarded.
func (d *LiveAgentDriver) RunAgent(ctx context.Context, worktree, branch, prompt string, budget time.Duration) error {
	if d.Runtime == nil {
		return fmt.Errorf("LiveAgentDriver: runtime is nil")
	}
	sessionName := autoresearchSessionName(branch)
	logFile := ""
	if d.LogDir != "" {
		_ = os.MkdirAll(d.LogDir, 0o755)
		logFile = filepath.Join(d.LogDir, sessionName+".log")
	}

	cfg := runtime.SessionConfig{
		WorkDir:      worktree,
		Model:        d.Model,
		SystemPrompt: d.SystemPrompt,
		Goal:         prompt,
		SessionName:  sessionName,
		LogFile:      logFile,
	}
	if err := d.Runtime.Spawn(cfg); err != nil {
		return fmt.Errorf("spawn %s: %w", sessionName, err)
	}

	poll := d.PollInterval
	if poll <= 0 {
		poll = 5 * time.Second
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	deadline := d.now().Add(budget)
	for {
		select {
		case <-ctx.Done():
			_ = d.Runtime.Terminate(sessionName)
			autoCommit(worktree, branch)
			return ctx.Err()
		case <-ticker.C:
			st, err := d.Runtime.DetectStatus(sessionName)
			if err == nil && (st == runtime.StatusDone || st == runtime.StatusTerminated) {
				_ = d.Runtime.Terminate(sessionName)
				autoCommit(worktree, branch)
				return nil
			}
			if d.now().After(deadline) {
				_ = d.Runtime.Terminate(sessionName)
				autoCommit(worktree, branch)
				return fmt.Errorf("LiveAgentDriver: budget elapsed after %s", budget)
			}
		}
	}
}

// Diff returns the agent's combined diff vs baseRef. baseRef is typically
// "origin/main" or "main"; the runner passes "origin/<base-branch>".
//
// Empty string means the agent produced no commits and no uncommitted
// changes — runner treats that as "no_diff", a Bayes loss.
func (d *LiveAgentDriver) Diff(worktree, baseRef string) (string, error) {
	if !exists(worktree) {
		return "", fmt.Errorf("worktree not present: %s", worktree)
	}
	out, err := runIn(worktree, "git", "diff", baseRef+"...HEAD")
	if err != nil {
		// Fall back to two-dot range if the merge base resolution fails
		// (e.g. on a freshly-init'd worktree where branches haven't diverged).
		out2, err2 := runIn(worktree, "git", "diff", baseRef)
		if err2 != nil {
			return "", err
		}
		return strings.TrimSpace(out2), nil
	}
	return strings.TrimSpace(out), nil
}

// PathsTouched lists files modified vs baseRef (relative paths).
func (d *LiveAgentDriver) PathsTouched(worktree, baseRef string) ([]string, error) {
	if !exists(worktree) {
		return nil, fmt.Errorf("worktree not present: %s", worktree)
	}
	out, err := runIn(worktree, "git", "diff", "--name-only", baseRef+"...HEAD")
	if err != nil {
		out2, err2 := runIn(worktree, "git", "diff", "--name-only", baseRef)
		if err2 != nil {
			return nil, err
		}
		out = out2
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

func (d *LiveAgentDriver) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// autoCommit stages and commits any uncommitted changes the agent left
// behind. Idempotent: if there's nothing to commit, returns nil silently.
//
// Mirrors the autoresearch invariant that diffs reflect what the agent
// actually wrote, regardless of whether the agent finished or was killed.
func autoCommit(worktree, branch string) {
	if !exists(worktree) {
		return
	}
	// Switch to the experiment branch in case the agent detached.
	_, _ = runIn(worktree, "git", "checkout", branch)

	// Anything to commit?
	out, err := runIn(worktree, "git", "status", "--porcelain")
	if err != nil || strings.TrimSpace(out) == "" {
		return
	}
	_, _ = runIn(worktree, "git", "add", "-A")
	_, _ = runIn(worktree, "git", "commit", "-m", "autoresearch: agent edits", "--allow-empty")
}

func runIn(dir string, args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// autoresearchSessionName turns a branch name into a tmux-safe session id.
// tmux disallows ":" and "."; we substitute with "-".
func autoresearchSessionName(branch string) string {
	r := strings.NewReplacer("/", "-", ":", "-", ".", "-")
	return "ar-" + r.Replace(branch)
}
