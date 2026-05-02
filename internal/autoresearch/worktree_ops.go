package autoresearch

import (
	"github.com/tzone85/vortex-dispatch/internal/git"
)

// DefaultWorktreeOps is the production WorktreeOps backed by internal/git.
//
// Create idempotently provisions a worktree at the given path on a fresh
// branch. Remove deletes both the worktree and the branch via the standard
// internal/git helper.
type DefaultWorktreeOps struct{}

// Create provisions a worktree at worktreePath on the given branch in repoDir.
func (DefaultWorktreeOps) Create(repoDir, worktreePath, branch string) error {
	return git.CreateWorktree(repoDir, worktreePath, branch)
}

// Remove tears down a worktree and its branch in one shot.
func (DefaultWorktreeOps) Remove(repoDir, worktreePath, branch string) error {
	return git.RemoveWorktree(repoDir, worktreePath, branch)
}
