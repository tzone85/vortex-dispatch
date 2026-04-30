package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- helpers ---

// initBareAndClone creates a bare remote repo + a working clone.
// Returns (clonePath, barePath). Both are inside t.TempDir().
func initBareAndClone(t *testing.T, branch string) (string, string) {
	t.Helper()
	base := t.TempDir()
	bare := filepath.Join(base, "remote.git")
	clone := filepath.Join(base, "local")

	// Create bare repo with explicit initial branch
	run(t, "", "git", "init", "--bare", "--initial-branch="+branch, bare)

	// Clone it
	run(t, "", "git", "clone", bare, clone)

	// Configure user for commits
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")

	os.WriteFile(filepath.Join(clone, "README.md"), []byte("# Test\n"), 0644)
	run(t, clone, "git", "add", "README.md")
	run(t, clone, "git", "commit", "-m", "initial commit")
	run(t, clone, "git", "push", "-u", "origin", branch)

	return clone, bare
}

// run is declared in gitdiff_test.go (same package).
// runNoFail is used for cases where we expect failure.
func runNoFail(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ===================================================================
// Fix 1: pullMainAfterMerge
// ===================================================================

func TestPullMainAfterMerge_SyncsLocalCheckout(t *testing.T) {
	clone, bare := initBareAndClone(t, "main")

	// Simulate a PR merge: push a new commit directly to the bare remote
	// from a separate clone (mimicking GitHub merging a PR).
	prClone := filepath.Join(t.TempDir(), "pr-clone")
	run(t, "", "git", "clone", bare, prClone)
	run(t, prClone, "git", "config", "user.email", "test@test.com")
	run(t, prClone, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(prClone, "feature.js"), []byte("console.log('new feature');\n"), 0644)
	run(t, prClone, "git", "add", "feature.js")
	run(t, prClone, "git", "commit", "-m", "feat: add feature from PR")
	run(t, prClone, "git", "push", "origin", "main")

	// Verify local clone does NOT have the new file yet
	if _, err := os.Stat(filepath.Join(clone, "feature.js")); err == nil {
		t.Fatal("feature.js should NOT exist before pull")
	}

	// Run the fix
	pullMainAfterMerge(clone)

	// Verify local clone now HAS the new file
	if _, err := os.Stat(filepath.Join(clone, "feature.js")); err != nil {
		t.Fatal("feature.js should exist after pullMainAfterMerge")
	}
}

func TestPullMainAfterMerge_WorksWithMasterBranch(t *testing.T) {
	clone, bare := initBareAndClone(t, "master")

	// Push new commit to remote
	prClone := filepath.Join(t.TempDir(), "pr-clone")
	run(t, "", "git", "clone", bare, prClone)
	run(t, prClone, "git", "config", "user.email", "test@test.com")
	run(t, prClone, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(prClone, "api.js"), []byte("module.exports = {};\n"), 0644)
	run(t, prClone, "git", "add", "api.js")
	run(t, prClone, "git", "commit", "-m", "feat: add api")
	run(t, prClone, "git", "push", "origin", "master")

	// Before pull
	if _, err := os.Stat(filepath.Join(clone, "api.js")); err == nil {
		t.Fatal("api.js should NOT exist before pull")
	}

	pullMainAfterMerge(clone)

	// After pull
	if _, err := os.Stat(filepath.Join(clone, "api.js")); err != nil {
		t.Fatal("api.js should exist after pullMainAfterMerge (master branch)")
	}
}

func TestPullMainAfterMerge_CleansUpArtifacts(t *testing.T) {
	clone, _ := initBareAndClone(t, "main")

	// Create VXD artifacts in repo root
	os.WriteFile(filepath.Join(clone, "WAVE_CONTEXT.md"), []byte("# Wave Context\nstory completed\n"), 0644)
	os.WriteFile(filepath.Join(clone, "REQUIREMENT.md"), []byte("# Req\n"), 0644)
	os.WriteFile(filepath.Join(clone, ".vxd-fix-gaps.md"), []byte("fix this\n"), 0644)

	pullMainAfterMerge(clone)

	// All artifacts should be removed
	for _, f := range []string{"WAVE_CONTEXT.md", "REQUIREMENT.md", ".vxd-fix-gaps.md"} {
		if _, err := os.Stat(filepath.Join(clone, f)); err == nil {
			t.Errorf("%s should be cleaned up after pullMainAfterMerge", f)
		}
	}
}

func TestPullMainAfterMerge_EnsuresGitignore(t *testing.T) {
	clone, _ := initBareAndClone(t, "main")

	pullMainAfterMerge(clone)

	gi, err := os.ReadFile(filepath.Join(clone, ".gitignore"))
	if err != nil {
		t.Fatal("gitignore should exist after pullMainAfterMerge")
	}
	content := string(gi)

	for _, pattern := range []string{"CLAUDE.md", "WAVE_CONTEXT.md", "vxd.yaml", ".vxd-prompts/"} {
		if !strings.Contains(content, pattern) {
			t.Errorf("gitignore should contain %q", pattern)
		}
	}
}

func TestPullMainAfterMerge_EmptyRepoDir(t *testing.T) {
	// Should not panic on empty string
	pullMainAfterMerge("")
}

func TestPullMainAfterMerge_NonFatalOnDirtyWorkingTree(t *testing.T) {
	clone, bare := initBareAndClone(t, "main")

	// Push a remote change
	prClone := filepath.Join(t.TempDir(), "pr-clone")
	run(t, "", "git", "clone", bare, prClone)
	run(t, prClone, "git", "config", "user.email", "test@test.com")
	run(t, prClone, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(prClone, "remote.txt"), []byte("remote\n"), 0644)
	run(t, prClone, "git", "add", "remote.txt")
	run(t, prClone, "git", "commit", "-m", "remote change")
	run(t, prClone, "git", "push", "origin", "main")

	// Create local uncommitted changes (dirty working tree)
	os.WriteFile(filepath.Join(clone, "local-wip.txt"), []byte("work in progress\n"), 0644)
	run(t, clone, "git", "add", "local-wip.txt")

	// Should not panic — pull may fail but function should handle gracefully
	pullMainAfterMerge(clone)
}

// ===================================================================
// Fix 2: ensureGitignorePatterns
// ===================================================================

func TestEnsureGitignorePatterns_CreatesGitignoreIfMissing(t *testing.T) {
	dir := t.TempDir()

	ensureGitignorePatterns(dir)

	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal("gitignore should be created")
	}
	content := string(gi)
	for _, pat := range []string{"CLAUDE.md", "WAVE_CONTEXT.md", "vxd.yaml", ".vxd-prompts/"} {
		if !strings.Contains(content, pat) {
			t.Errorf("gitignore missing pattern %q", pat)
		}
	}
}

func TestEnsureGitignorePatterns_DoesNotDuplicate(t *testing.T) {
	dir := t.TempDir()

	// Run twice
	ensureGitignorePatterns(dir)
	ensureGitignorePatterns(dir)

	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	content := string(gi)

	// Count occurrences of CLAUDE.md — should be exactly 1
	count := strings.Count(content, "CLAUDE.md")
	if count != 1 {
		t.Errorf("CLAUDE.md appears %d times (expected 1, idempotent)", count)
	}
}

func TestEnsureGitignorePatterns_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()

	// Pre-existing gitignore
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n.env\n"), 0644)

	ensureGitignorePatterns(dir)

	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	content := string(gi)

	if !strings.Contains(content, "node_modules/") {
		t.Error("pre-existing entries should be preserved")
	}
	if !strings.Contains(content, "CLAUDE.md") {
		t.Error("VXD patterns should be appended")
	}
}

// ===================================================================
// Fix 3: stripVXDArtifactsFromBranch
// ===================================================================

func TestStripVXDArtifacts_RemovesCLAUDEMD(t *testing.T) {
	clone, _ := initBareAndClone(t, "main")

	// Create a feature branch with agent work + CLAUDE.md
	run(t, clone, "git", "checkout", "-b", "vxd/story-001")
	os.WriteFile(filepath.Join(clone, "feature.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(clone, "CLAUDE.md"), []byte("# Agent Directive\nDo not brainstorm.\n"), 0644)
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "feat: add feature + agent CLAUDE.md")

	// Verify CLAUDE.md is in the commit
	tracked := run(t, clone, "git", "ls-files")
	if !strings.Contains(tracked, "CLAUDE.md") {
		t.Fatal("CLAUDE.md should be tracked before strip")
	}

	// Strip
	stripVXDArtifactsFromBranch(clone, "story-001")

	// Verify CLAUDE.md is NOT in the commit anymore
	tracked = run(t, clone, "git", "ls-files")
	if strings.Contains(tracked, "CLAUDE.md") {
		t.Error("CLAUDE.md should NOT be tracked after stripVXDArtifactsFromBranch")
	}

	// Verify feature.go IS still tracked
	if !strings.Contains(tracked, "feature.go") {
		t.Error("feature.go should still be tracked after strip")
	}

	// CLAUDE.md doesn't exist on base (no initial CLAUDE.md in this test),
	// so it gets fully removed by stripVXDArtifactsFromBranch.
}

func TestStripVXDArtifacts_RemovesMultipleArtifacts(t *testing.T) {
	clone, _ := initBareAndClone(t, "main")

	run(t, clone, "git", "checkout", "-b", "vxd/story-002")
	os.WriteFile(filepath.Join(clone, "app.js"), []byte("console.log('app');\n"), 0644)
	os.WriteFile(filepath.Join(clone, "CLAUDE.md"), []byte("agent directive\n"), 0644)
	os.WriteFile(filepath.Join(clone, "WAVE_CONTEXT.md"), []byte("wave context\n"), 0644)
	os.MkdirAll(filepath.Join(clone, ".vxd-prompts"), 0755)
	os.WriteFile(filepath.Join(clone, ".vxd-prompts", "prompt.txt"), []byte("prompt\n"), 0644)
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "feat: work + artifacts")

	stripVXDArtifactsFromBranch(clone, "story-002")

	tracked := run(t, clone, "git", "ls-files")
	for _, art := range []string{"CLAUDE.md", "WAVE_CONTEXT.md", ".vxd-prompts/prompt.txt"} {
		if strings.Contains(tracked, art) {
			t.Errorf("%s should NOT be tracked after strip", art)
		}
	}
	if !strings.Contains(tracked, "app.js") {
		t.Error("app.js should still be tracked")
	}
}

func TestStripVXDArtifacts_NoopWhenNoArtifacts(t *testing.T) {
	clone, _ := initBareAndClone(t, "main")

	run(t, clone, "git", "checkout", "-b", "vxd/story-003")
	os.WriteFile(filepath.Join(clone, "clean.go"), []byte("package main\n"), 0644)
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "feat: clean commit")

	commitBefore := run(t, clone, "git", "rev-parse", "HEAD")

	// Strip should be a no-op
	stripVXDArtifactsFromBranch(clone, "story-003")

	commitAfter := run(t, clone, "git", "rev-parse", "HEAD")
	if commitBefore != commitAfter {
		t.Error("commit hash should not change when no artifacts to strip")
	}
}

func TestStripVXDArtifacts_PreservesProjectCLAUDEMDOnMain(t *testing.T) {
	// This is the critical scenario: project has a real CLAUDE.md on main,
	// agent adds its own version. After strip + merge, project's version
	// should survive.
	clone, _ := initBareAndClone(t, "main")

	// Add a real project CLAUDE.md to main
	projectCLAUDE := "# SampleApp\n\nThis is the project documentation.\n\n## Architecture\nExpress + MongoDB\n"
	os.WriteFile(filepath.Join(clone, "CLAUDE.md"), []byte(projectCLAUDE), 0644)
	run(t, clone, "git", "add", "CLAUDE.md")
	run(t, clone, "git", "commit", "-m", "docs: add project CLAUDE.md")
	run(t, clone, "git", "push", "origin", "main")

	// Create agent branch
	run(t, clone, "git", "checkout", "-b", "vxd/story-004")
	os.WriteFile(filepath.Join(clone, "handler.js"), []byte("module.exports = {};\n"), 0644)
	// Agent overwrites CLAUDE.md with its directive
	agentCLAUDE := "# Agent Directive\nDo not brainstorm.\n"
	os.WriteFile(filepath.Join(clone, "CLAUDE.md"), []byte(agentCLAUDE), 0644)
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "feat: add handler (agent)")

	// Strip artifacts
	stripVXDArtifactsFromBranch(clone, "story-004")

	// CLAUDE.md exists on main, so strip should RESTORE it to main's version
	// (not delete it). The branch diff for CLAUDE.md becomes a no-op.
	data, err := os.ReadFile(filepath.Join(clone, "CLAUDE.md"))
	if err != nil {
		t.Fatal("CLAUDE.md should still exist on disk (restored to base version)")
	}
	if string(data) != projectCLAUDE {
		t.Errorf("CLAUDE.md should be restored to base version\ngot: %s\nwant: %s", string(data), projectCLAUDE)
	}

	// Now simulate merge: checkout main, merge the branch
	run(t, clone, "git", "checkout", "main")

	// Merge the stripped branch
	run(t, clone, "git", "merge", "vxd/story-004", "--no-edit")

	// After merge, project CLAUDE.md should STILL be the original
	data, err = os.ReadFile(filepath.Join(clone, "CLAUDE.md"))
	if err != nil {
		t.Fatal("project CLAUDE.md should survive merge")
	}
	if string(data) != projectCLAUDE {
		t.Errorf("CRITICAL: project CLAUDE.md was overwritten after merge!\ngot: %s\nwant: %s", string(data), projectCLAUDE)
	}

	// handler.js should exist after merge
	if _, err := os.Stat(filepath.Join(clone, "handler.js")); err != nil {
		t.Error("handler.js should exist after merge (real work preserved)")
	}
}

// ===================================================================
// Integration: Full pipeline simulation
// ===================================================================

func TestIntegration_FullPipelineArtifactProtection(t *testing.T) {
	clone, bare := initBareAndClone(t, "main")

	// 1. Project starts with its own CLAUDE.md
	projectDoc := "# MyProject\nFull project documentation here.\n"
	os.WriteFile(filepath.Join(clone, "CLAUDE.md"), []byte(projectDoc), 0644)
	run(t, clone, "git", "add", "CLAUDE.md")
	run(t, clone, "git", "commit", "-m", "docs: project CLAUDE.md")
	run(t, clone, "git", "push", "origin", "main")

	// 2. VXD creates a worktree branch and agent works
	run(t, clone, "git", "checkout", "-b", "vxd/s-001")
	os.WriteFile(filepath.Join(clone, "src/app.js"), []byte("// app\n"), 0644)
	os.MkdirAll(filepath.Join(clone, "src"), 0755)
	os.WriteFile(filepath.Join(clone, "src/app.js"), []byte("// app\n"), 0644)
	// Agent writes VXD artifacts
	os.WriteFile(filepath.Join(clone, "CLAUDE.md"), []byte("# VXD Agent\nDo not brainstorm.\n"), 0644)
	os.WriteFile(filepath.Join(clone, "WAVE_CONTEXT.md"), []byte("# Context\nStory 1 done.\n"), 0644)
	os.MkdirAll(filepath.Join(clone, ".vxd-prompts"), 0755)
	os.WriteFile(filepath.Join(clone, ".vxd-prompts/prompt.txt"), []byte("implement feature\n"), 0644)
	run(t, clone, "git", "add", "-A")
	run(t, clone, "git", "commit", "-m", "feat: implement feature")

	// 3. VXD post-execution: autoCommit then strip
	stripVXDArtifactsFromBranch(clone, "s-001")

	// 4. Verify VXD-only artifacts are removed; CLAUDE.md is restored to base version
	tracked := run(t, clone, "git", "ls-files")
	for _, bad := range []string{"WAVE_CONTEXT.md", ".vxd-prompts"} {
		if strings.Contains(tracked, bad) {
			t.Errorf("VXD artifact %s should be removed from branch", bad)
		}
	}
	// CLAUDE.md should be tracked but restored to project's original version
	if !strings.Contains(tracked, "CLAUDE.md") {
		t.Error("CLAUDE.md should still be tracked (restored to base version)")
	}
	data, _ := os.ReadFile(filepath.Join(clone, "CLAUDE.md"))
	if string(data) != projectDoc {
		t.Errorf("CLAUDE.md should be restored to project version, got: %q", string(data))
	}

	// 5. Push and merge (simulate GitHub)
	run(t, clone, "git", "push", "-u", "origin", "vxd/s-001")

	// Merge from a separate clone (like GitHub would)
	mergeClone := filepath.Join(t.TempDir(), "merge")
	run(t, "", "git", "clone", bare, mergeClone)
	run(t, mergeClone, "git", "config", "user.email", "test@test.com")
	run(t, mergeClone, "git", "config", "user.name", "Test")
	run(t, mergeClone, "git", "merge", "origin/vxd/s-001", "--no-edit")
	run(t, mergeClone, "git", "push", "origin", "main")

	// 6. VXD runs pullMainAfterMerge
	run(t, clone, "git", "checkout", "main")

	// Write WAVE_CONTEXT.md to repo root (as VXD does during the run)
	os.WriteFile(filepath.Join(clone, "WAVE_CONTEXT.md"), []byte("stale context\n"), 0644)

	pullMainAfterMerge(clone)

	// 7. Final verification
	// - Project CLAUDE.md should be original
	data, _ = os.ReadFile(filepath.Join(clone, "CLAUDE.md"))
	if string(data) != projectDoc {
		t.Errorf("CRITICAL: project CLAUDE.md was corrupted!\ngot: %q\nwant: %q", string(data), projectDoc)
	}

	// - WAVE_CONTEXT.md should be cleaned up
	if _, err := os.Stat(filepath.Join(clone, "WAVE_CONTEXT.md")); err == nil {
		t.Error("WAVE_CONTEXT.md should be cleaned up after pullMainAfterMerge")
	}

	// - src/app.js should exist (real work merged)
	if _, err := os.Stat(filepath.Join(clone, "src/app.js")); err != nil {
		t.Error("src/app.js should exist after merge (real work preserved)")
	}

	// - gitignore should cover VXD artifacts
	gi, _ := os.ReadFile(filepath.Join(clone, ".gitignore"))
	if !strings.Contains(string(gi), "WAVE_CONTEXT.md") {
		t.Error("gitignore should include WAVE_CONTEXT.md")
	}
}
