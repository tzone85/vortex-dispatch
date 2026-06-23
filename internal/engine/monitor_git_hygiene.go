package engine

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func simulateDryRunChanges(worktreePath, storyID string) {
	simFile := filepath.Join(worktreePath, "dry-run-simulation.txt")
	content := fmt.Sprintf("[DRY RUN] Simulated changes for story %s\nThis file would be replaced by real agent output.\n", storyID)
	if err := os.WriteFile(simFile, []byte(content), 0o644); err != nil {
		log.Printf("[dry-run] failed to write simulation file: %v", err)
		return
	}

	// Stage and commit
	addCmd := exec.Command("git", "add", "dry-run-simulation.txt")
	addCmd.Dir = worktreePath
	if err := addCmd.Run(); err != nil {
		log.Printf("[dry-run] git add failed: %v", err)
		return
	}

	commitCmd := exec.Command("git", "commit", "-m", fmt.Sprintf("[dry-run] simulated changes for %s", storyID))
	commitCmd.Dir = worktreePath
	_ = commitCmd.Run() // ignore error (may already be committed)

	log.Printf("[dry-run] simulated changes committed for %s", storyID)
}

// autoCommit stages and commits any uncommitted changes in the worktree.
// This is a safety net for agents that produce code but exit without
// committing. VXD artifacts (.vxd-prompts, CLAUDE.md, .serena) are excluded.
func autoCommit(worktreePath, storyID string) {
	// Check for uncommitted changes (staged or unstaged).
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = worktreePath
	statusOut, err := statusCmd.CombinedOutput()
	if err != nil || len(strings.TrimSpace(string(statusOut))) == 0 {
		return // nothing to commit
	}

	log.Printf("[pipeline] auto-committing uncommitted work for %s", storyID)

	// Ensure VXD artifacts are in .gitignore so they are never committed.
	ensureGitignorePatterns(worktreePath)

	// Stage all non-ignored changes.
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = worktreePath
	if out, err := addCmd.CombinedOutput(); err != nil {
		log.Printf("[pipeline] git add failed for %s: %v (%s)", storyID, err, strings.TrimSpace(string(out)))
		return
	}

	// Commit with a descriptive message.
	commitCmd := exec.Command("git", "commit", "-m",
		fmt.Sprintf("feat(%s): auto-commit agent work\n\nVXD auto-committed changes that the agent left uncommitted.", storyID))
	commitCmd.Dir = worktreePath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		log.Printf("[pipeline] auto-commit failed for %s: %v (%s)", storyID, err, strings.TrimSpace(string(out)))
		return
	}

	log.Printf("[pipeline] auto-commit succeeded for %s", storyID)
}

// stripVXDArtifactsFromBranch removes VXD infrastructure files (CLAUDE.md,
// WAVE_CONTEXT.md, .vxd-prompts/, etc.) from the worktree branch via
// git rm --cached, then amends the commit. This prevents agent-committed
// artifacts from appearing in PRs, which would overwrite the project's
// real CLAUDE.md with the agent-directive version.
func stripVXDArtifactsFromBranch(worktreePath, storyID string) {
	// AGENTS.md is here alongside CLAUDE.md because VXD now dual-writes
	// the agent directive to both files at spawn time (Codex / Gemini CLI
	// look at AGENTS.md, Claude Code at CLAUDE.md). Without strip the
	// agent-directive copy would be committed onto the story branch and
	// overwrite the project's own AGENTS.md on merge — the same failure
	// mode CLAUDE.md hit in the 2026-04-30 incident.
	artifacts := []string{
		"CLAUDE.md",
		"AGENTS.md",
		"WAVE_CONTEXT.md",
		"REQUIREMENT.md",
		".vxd-prompts",
		".vxd-db",
		".serena",
		".superpowers",
	}

	// Detect base branch for restoring artifacts to their original state.
	baseBranch := "main"
	for _, candidate := range []string{"origin/main", "origin/master"} {
		check := exec.Command("git", "rev-parse", "--verify", candidate)
		check.Dir = worktreePath
		if err := check.Run(); err == nil {
			baseBranch = candidate
			break
		}
	}

	var restored []string
	for _, art := range artifacts {
		artPath := filepath.Join(worktreePath, art)
		if _, err := os.Stat(artPath); err != nil {
			continue
		}

		// Check if this file exists on the base branch (i.e., it's a
		// project file the agent overwrote, like CLAUDE.md).
		checkBase := exec.Command("git", "cat-file", "-e", baseBranch+":"+art)
		checkBase.Dir = worktreePath
		existsOnBase := checkBase.Run() == nil

		if existsOnBase {
			// Restore the base branch version so the merge is a no-op
			// for this file. The agent's changes are discarded.
			restoreCmd := exec.Command("git", "checkout", baseBranch, "--", art)
			restoreCmd.Dir = worktreePath
			if out, err := restoreCmd.CombinedOutput(); err != nil {
				log.Printf("[pipeline] git checkout %s -- %s for %s: %v (%s)", baseBranch, art, storyID, err, strings.TrimSpace(string(out)))
				continue
			}
		} else {
			// File doesn't exist on base — it was created by VXD/agent.
			// Remove it completely so it doesn't appear in the PR.
			// --ignore-unmatch makes an absent artifact a clean no-op instead of
			// an exit-128 "pathspec did not match any files" log line.
			rmCmd := exec.Command("git", "rm", "-rf", "--ignore-unmatch", art)
			rmCmd.Dir = worktreePath
			if out, err := rmCmd.CombinedOutput(); err != nil {
				log.Printf("[pipeline] git rm %s for %s: %v (%s)", art, storyID, err, strings.TrimSpace(string(out)))
				continue
			}
		}
		restored = append(restored, art)
	}

	if len(restored) == 0 {
		return
	}

	// Stage changes and amend the commit
	stageCmd := exec.Command("git", "add", "-A")
	stageCmd.Dir = worktreePath
	_, _ = stageCmd.CombinedOutput() // best-effort stage; amend below surfaces real failures

	amendCmd := exec.Command("git", "commit", "--amend", "--no-edit")
	amendCmd.Dir = worktreePath
	if out, err := amendCmd.CombinedOutput(); err != nil {
		log.Printf("[pipeline] amend after artifact strip for %s: %v (%s)", storyID, err, strings.TrimSpace(string(out)))
	} else {
		log.Printf("[pipeline] stripped %d VXD artifact(s) from branch for %s: %v", len(restored), storyID, restored)
	}
}

// stripBinariesFromBranch removes compiled binary files committed by agents.
// It uses `git diff --numstat HEAD~1 HEAD` to detect binaries (lines with
// "-\t-\t<path>" prefix) and removes them from the branch via git rm, then
// appends them to .gitignore and amends the last commit.
//
// This prevents binary blobs from appearing in PRs, bloating the repository,
// and triggering conflict-resolver "prompt too long" errors.
func stripBinariesFromBranch(worktreePath, storyID string) {
	numstatCmd := exec.Command("git", "diff", "--numstat", "HEAD~1", "HEAD")
	numstatCmd.Dir = worktreePath
	out, err := numstatCmd.CombinedOutput()
	if err != nil {
		// Single-commit history or other git error — skip silently.
		return
	}

	var binaries []string
	for _, line := range strings.Split(string(out), "\n") {
		// Binary files appear as "-\t-\t<path>" in --numstat output.
		if !strings.HasPrefix(line, "-\t-\t") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) == 3 {
			binaries = append(binaries, strings.TrimSpace(parts[2]))
		}
	}
	if len(binaries) == 0 {
		return
	}

	// Remove binaries from the index.
	for _, b := range binaries {
		rmCmd := exec.Command("git", "rm", "-f", "--cached", b)
		rmCmd.Dir = worktreePath
		if rmOut, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
			// Try unconditional rm in case the file is not cached.
			rmCmd2 := exec.Command("git", "rm", "-f", b)
			rmCmd2.Dir = worktreePath
			if out2, err2 := rmCmd2.CombinedOutput(); err2 != nil {
				log.Printf("[pipeline] git rm %s for %s: %v (%s)", b, storyID, err2, strings.TrimSpace(string(out2)))
				continue
			}
		} else {
			_ = rmOut
		}
	}

	// Append to .gitignore so they don't come back. Log on failure: a silent
	// write failure here means the subsequent `git add .gitignore` + amend
	// reports success while the ignore entries were never written, so the
	// stripped binaries can reappear on the next commit.
	giPath := filepath.Join(worktreePath, ".gitignore")
	giData, _ := os.ReadFile(giPath)
	appendix := "\n# auto-detected binaries (stripped by vxd)\n" + strings.Join(binaries, "\n") + "\n"
	if err := os.WriteFile(giPath, append(giData, []byte(appendix)...), 0o644); err != nil {
		log.Printf("[hygiene] failed to append binary patterns to %s: %v (stripped binaries may reappear)", giPath, err)
	}

	stageCmd := exec.Command("git", "add", ".gitignore")
	stageCmd.Dir = worktreePath
	_, _ = stageCmd.CombinedOutput() // best-effort stage; amend below surfaces real failures

	amendCmd := exec.Command("git", "commit", "--amend", "--no-edit")
	amendCmd.Dir = worktreePath
	if amendOut, amendErr := amendCmd.CombinedOutput(); amendErr != nil {
		log.Printf("[pipeline] amend after binary strip for %s: %v (%s)", storyID, amendErr, strings.TrimSpace(string(amendOut)))
	} else {
		log.Printf("[pipeline] stripped %d binary file(s) from branch for %s: %v", len(binaries), storyID, binaries)
	}
}

// pullMainAfterMerge fetches and fast-forward merges the base branch into
// the local checkout after all PRs have been merged. This ensures the repo
// directory reflects the actual merged state so that subsequent tools
// (evaluators, linters, other agents) see the completed work.
func pullMainAfterMerge(repoDir string) {
	pullBaseAfterMerge(repoDir, "")
}

func pullBaseAfterMerge(repoDir, baseBranch string) {
	if repoDir == "" {
		return
	}

	// Pre-clean VXD-only working-tree leftovers that would block ff-pull.
	// These files may be untracked (written by VXD, never committed) or
	// tracked+modified (e.g. from a prior partial run). Handle both cases:
	//   git clean -f <file>  — removes untracked files
	//   git checkout -- <f>  — discards tracked modifications (restores HEAD)
	// Both commands are best-effort; errors are intentionally ignored.
	for _, artifact := range []string{
		"WAVE_CONTEXT.md",
		"REQUIREMENT.md",
		".vxd-fix-gaps.md",
	} {
		// Discard tracked modifications first (no-op if file is untracked).
		checkoutCmd := exec.Command("git", "-C", repoDir, "checkout", "--", artifact)
		_ = checkoutCmd.Run()
		// Remove any remaining untracked copy.
		cleanCmd := exec.Command("git", "-C", repoDir, "clean", "-f", artifact)
		_ = cleanCmd.Run()
		// Belt-and-suspenders: also remove from disk if both git ops were no-ops.
		p := filepath.Join(repoDir, artifact)
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p) // best-effort; logged on next line
		}
		log.Printf("[auto-resume] pre-cleaned %s from repo root (best-effort)", artifact)
	}

	// Ensure gitignore covers VXD artifacts for the main repo (not just worktrees).
	ensureGitignorePatterns(repoDir)

	branches := []string{baseBranch}
	if baseBranch == "" {
		branches = []string{"main", "master"}
	}
	for _, branch := range branches {
		if branch == "" {
			continue
		}
		cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
		cmd.Dir = repoDir
		if err := cmd.Run(); err == nil {
			gitPullWithStash(repoDir, branch)
			return
		}
	}
	log.Printf("[auto-resume] could not detect base branch for pull")
}

// ensureGitignorePatterns appends VXD artifact patterns to .gitignore if
// they are not already present, preventing CLAUDE.md, .vxd-prompts/,
// .serena/, and other tool artifacts from being committed by agents.
// gitPullWithStash performs a fast-forward pull of the given branch.
// If the working tree is dirty it stashes first, pulls, then pops.
// If the stash itself fails it skips the pull cleanly rather than logging
// a noisy "failed" message that implies a real error.
func gitPullWithStash(repoDir, branch string) {
	// Check for dirty working tree — MUST be run in repoDir or the dirty
	// check is performed in the daemon's CWD (the VXD source repo, often
	// clean) instead of the user's project. Without Dir, a real dirty
	// tree was reported clean, the ff-pull failed, and we logged
	// "pull non-fatal" while the project sat broken.
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = repoDir
	statusOut, err := statusCmd.Output()
	statusOut2 := ""
	if err == nil {
		statusOut2 = strings.TrimSpace(string(statusOut))
	}
	dirty := statusOut2 != ""

	if dirty {
		stash := exec.Command("git", "stash", "push", "-u", "-m", "vxd-pull-stash")
		stash.Dir = repoDir
		if stashErr := stash.Run(); stashErr != nil {
			// Count dirty files for the log message.
			dirtyCount := len(strings.Split(statusOut2, "\n"))
			log.Printf("[auto-resume] working tree dirty (%d files) — skipping %s pull; manual: cd %s && git pull --ff-only origin %s",
				dirtyCount, branch, repoDir, branch)
			return
		}
		log.Printf("[auto-resume] stashed dirty working tree before pulling %s", branch)
	}

	pull := exec.Command("git", "pull", "--ff-only", "origin", branch)
	pull.Dir = repoDir
	if out, pullErr := pull.CombinedOutput(); pullErr != nil {
		log.Printf("[auto-resume] pull %s non-fatal: %v — %s", branch, pullErr, strings.TrimSpace(string(out)))
	} else {
		log.Printf("[auto-resume] pulled latest %s into local checkout", branch)
	}

	if dirty {
		pop := exec.Command("git", "stash", "pop")
		pop.Dir = repoDir
		if popErr := pop.Run(); popErr != nil {
			log.Printf("[auto-resume] stash pop after pull: %v (manual: cd %s && git stash pop)", popErr, repoDir)
		}
	}
}

func ensureGitignorePatterns(worktreePath string) {
	vxdPatterns := []string{
		"CLAUDE.md",
		"AGENTS.md",
		"WAVE_CONTEXT.md",
		"REQUIREMENT.md",
		"vxd.yaml",
		".vxd-prompts/",
		".serena/",
		"firebase-debug.log",
	}

	giPath := worktreePath + "/.gitignore"
	existing, _ := os.ReadFile(giPath)
	content := string(existing)

	var toAdd []string
	for _, pat := range vxdPatterns {
		if !strings.Contains(content, pat) {
			toAdd = append(toAdd, pat)
		}
	}
	if len(toAdd) == 0 {
		return
	}

	appendix := "\n# VXD agent artifacts (auto-added)\n" + strings.Join(toAdd, "\n") + "\n"
	if err := os.WriteFile(giPath, append(existing, []byte(appendix)...), 0o644); err != nil {
		log.Printf("[gitignore] failed to update %s: %v", giPath, err)
	}
}

// captureFileTree returns a compact listing of tracked files in the worktree.
// This gives the reviewer context about what already exists so it doesn't
// hallucinate about "missing" files that weren't part of the diff.
