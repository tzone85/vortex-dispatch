package engine

import (
	"fmt"
	"os/exec"
	"strings"
)

func captureFileTree(worktreePath string) string {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitDiff returns the git diff for committed changes in a worktree.
// It compares HEAD against the merge-base with the base branch so that it
// captures all changes since the worktree branch diverged, rather than
// only the last commit (which misses truly-empty agent runs).
//
// Tries merge-base candidates in order: origin/<base>, local <base>, then
// falls back to the repo root commit so repos without a remote still work.
func gitDiff(worktreePath string) (string, error) {
	return gitDiffForBase(worktreePath, "")
}

func gitDiffForBase(worktreePath, baseBranch string) (string, error) {
	// Try merge-base candidates in order of preference.
	// Include both main and master to support repos using either convention.
	var candidates []string
	if baseBranch != "" {
		candidates = []string{"origin/" + baseBranch, baseBranch}
	} else {
		candidates = []string{"origin/main", "origin/master", "main", "master"}
	}
	var mbOut []byte
	var mbErr error
	for _, ref := range candidates {
		mbCmd := exec.Command("git", "merge-base", "HEAD", ref)
		mbCmd.Dir = worktreePath
		mbOut, mbErr = mbCmd.Output()
		if mbErr == nil {
			break
		}
	}
	if mbErr != nil {
		// No merge-base found — fall back to the root commit of the
		// current branch so we diff all changes since the initial commit.
		rootCmd := exec.Command("git", "rev-list", "--max-parents=0", "HEAD")
		rootCmd.Dir = worktreePath
		rootOut, rootErr := rootCmd.Output()
		if rootErr != nil {
			return "", fmt.Errorf("git diff: cannot find merge-base or root commit: %w", rootErr)
		}
		mbOut = rootOut
	}

	mergeBase := strings.TrimSpace(string(mbOut))
	cmd := exec.Command("git", "diff", mergeBase, "HEAD")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}

	// Filter out diffs that only touch .gitignore (written by
	// ensureGitignorePatterns before this check). A diff limited to
	// .gitignore means the agent produced no real code changes.
	if isGitignoreOnlyDiff(worktreePath, mergeBase) {
		return "", nil
	}

	return string(out), nil
}

// vxdArtifactPatterns are files created by VXD infrastructure, not by the
// agent's actual work. A diff containing ONLY these files means the agent
// produced no real code changes.
var vxdArtifactPatterns = []string{
	".gitignore",
	"CLAUDE.md",
	".vxd-prompts/",
	".serena/",
	"dry-run-simulation.txt",
}

// isArtifactFile returns true if the file path matches a VXD infrastructure artifact.
func isArtifactFile(path string) bool {
	for _, pattern := range vxdArtifactPatterns {
		if path == pattern || strings.HasPrefix(path, pattern) {
			return true
		}
	}
	return false
}

// isGitignoreOnlyDiff returns true when the only files changed between
// mergeBase and HEAD are VXD infrastructure artifacts (not real code).
func isGitignoreOnlyDiff(worktreePath, mergeBase string) bool {
	cmd := exec.Command("git", "diff", "--name-only", mergeBase, "HEAD")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	files := strings.TrimSpace(string(out))
	if files == "" {
		return false // no files changed — caller already handles empty diff
	}
	for _, f := range strings.Split(files, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !isArtifactFile(f) {
			return false
		}
	}
	return true
}
