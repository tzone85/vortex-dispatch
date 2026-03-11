package git

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PRInfo holds metadata about a GitHub pull request.
type PRInfo struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Title  string `json:"title"`
}

// CreatePR opens a new pull request from headBranch to baseBranch
// using the gh CLI. The headBranch parameter is required so the PR
// can be created from any working directory (not just the worktree).
func CreatePR(repoDir, title, body, baseBranch, headBranch string) (PRInfo, error) {
	cmd := exec.Command("gh", "pr", "create",
		"--title", title,
		"--body", body,
		"--base", baseBranch,
		"--head", headBranch,
	)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PRInfo{}, fmt.Errorf("gh pr create: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	url := strings.TrimSpace(string(out))

	// Extract PR number from URL (e.g. https://github.com/owner/repo/pull/123)
	var number int
	if parts := strings.Split(url, "/"); len(parts) > 0 {
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			number = n
		}
	}

	return PRInfo{Number: number, URL: url}, nil
}

// MergePR squash-merges the given PR number. Branch cleanup is handled
// separately because local branches checked out in worktrees cannot be
// deleted by gh.
func MergePR(repoDir string, prNumber int) error {
	cmd := exec.Command("gh", "pr", "merge",
		fmt.Sprintf("%d", prNumber),
		"--squash",
	)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh pr merge: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GetPRStatus retrieves metadata for the given PR number.
func GetPRStatus(repoDir string, prNumber int) (PRInfo, error) {
	cmd := exec.Command("gh", "pr", "view",
		fmt.Sprintf("%d", prNumber),
		"--json", "number,url,state,title",
	)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PRInfo{}, fmt.Errorf("gh pr view: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	var info PRInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return PRInfo{}, fmt.Errorf("parse pr info: %w", err)
	}
	return info, nil
}

// PushBranch pushes the named branch to origin and sets up tracking.
func PushBranch(repoDir, branch string) error {
	cmd := exec.Command("git", "push", "-u", "origin", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteRemoteBranch removes a branch from the origin remote.
func DeleteRemoteBranch(repoDir, branch string) error {
	cmd := exec.Command("git", "push", "origin", "--delete", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push --delete: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveWorktree removes a git worktree and its local branch.
func RemoveWorktree(repoDir, worktreePath, branch string) error {
	// Remove the worktree first so the branch is no longer checked out.
	rmCmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	rmCmd.Dir = repoDir
	if out, err := rmCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// Now the local branch can be safely deleted.
	brCmd := exec.Command("git", "branch", "-D", branch)
	brCmd.Dir = repoDir
	if out, err := brCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// GHAvailable reports whether the gh CLI binary is on PATH.
func GHAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}
