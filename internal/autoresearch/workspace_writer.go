package autoresearch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultWorkspaceWriter creates an ephemeral worktree on the target
// branch, writes one or more files into it, commits, then removes the
// worktree. Used by ProgramMDEvolver to persist a rewritten program.md
// onto the evolution branch before pushing.
//
// `Root` is the parent directory under which ephemeral worktrees are
// created. If empty, a sibling of repoDir is used.
type DefaultWorkspaceWriter struct {
	Root string
}

// WriteAndCommit creates an ephemeral worktree on `branch` (must already
// exist locally), writes each file relative to the worktree root, then
// `git add -A` + `git commit`, then removes the worktree.
//
// Returns nil even when there are no changes to commit (idempotent).
func (w DefaultWorkspaceWriter) WriteAndCommit(repoDir, branch, message string, files map[string]string) error {
	root := w.Root
	if root == "" {
		root = filepath.Join(filepath.Dir(repoDir), ".vxd-autoresearch-tmp")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", root, err)
	}
	worktreePath := filepath.Join(root, "evolve-"+branchToSlug(branch))

	// Use existing-branch checkout since `branch` was just created in the
	// repoDir's branch list. `git worktree add <path> <branch>` checks out
	// an existing branch into a new worktree without creating a new one.
	if err := addExistingBranchWorktree(repoDir, worktreePath, branch); err != nil {
		return fmt.Errorf("worktree add %s: %w", worktreePath, err)
	}

	// Always tear the worktree down even on error. We must NOT delete the
	// branch — the evolver will push it after this function returns.
	defer func() {
		_, _ = runIn(repoDir, "git", "worktree", "remove", "--force", worktreePath)
	}()

	for relPath, content := range files {
		full := filepath.Join(worktreePath, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
	}

	if _, err := runIn(worktreePath, "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	// `git commit` errors when there is nothing to commit; treat that as a
	// no-op so callers can be idempotent.
	out, err := runIn(worktreePath, "git", "commit", "-m", message)
	if err != nil {
		if isNothingToCommit(out) {
			return nil
		}
		return fmt.Errorf("git commit: %w (%s)", err, out)
	}
	return nil
}

// addExistingBranchWorktree wires `git worktree add <path> <branch>`
// without the `-b` flag (which would try to create the branch again).
func addExistingBranchWorktree(repoDir, worktreePath, branch string) error {
	if _, err := os.Stat(worktreePath); err == nil {
		// Stale worktree dir from a prior failed run; remove it.
		_ = os.RemoveAll(worktreePath)
	}
	out, err := runIn(repoDir, "git", "worktree", "add", worktreePath, branch)
	if err != nil {
		return fmt.Errorf("git worktree add %s %s: %w (%s)", worktreePath, branch, err, out)
	}
	return nil
}

func isNothingToCommit(out string) bool {
	return strings.Contains(out, "nothing to commit") ||
		strings.Contains(out, "no changes added to commit")
}

// branchToSlug renders a branch name as a filesystem-safe directory leaf.
func branchToSlug(branch string) string {
	out := make([]byte, 0, len(branch))
	for i := 0; i < len(branch); i++ {
		c := branch[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
