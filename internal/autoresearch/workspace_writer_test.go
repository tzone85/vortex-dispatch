package autoresearch

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newGitRepoWithBranch(t *testing.T, branch string) string {
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
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	must("git", "add", "-A")
	must("git", "commit", "-q", "-m", "init")
	must("git", "branch", branch)
	return dir
}

func TestDefaultWorkspaceWriter_WriteAndCommitAddsCommitOnBranch(t *testing.T) {
	repo := newGitRepoWithBranch(t, "autoresearch/evolve-20260503")
	root := t.TempDir()
	w := DefaultWorkspaceWriter{Root: root}

	files := map[string]string{
		"program.md": "# new program\n",
	}
	if err := w.WriteAndCommit(repo, "autoresearch/evolve-20260503", "test commit", files); err != nil {
		t.Fatalf("WriteAndCommit: %v", err)
	}

	// Verify the branch has a new commit beyond the seed.
	out, err := runIn(repo, "git", "log", "--oneline", "autoresearch/evolve-20260503")
	if err != nil {
		t.Fatalf("git log: %v %s", err, out)
	}
	if !contains(out, "test commit") {
		t.Errorf("commit not visible on branch; log:\n%s", out)
	}
	// And the file content lives on that branch's tree.
	out2, err := runIn(repo, "git", "show", "autoresearch/evolve-20260503:program.md")
	if err != nil {
		t.Fatalf("git show: %v %s", err, out2)
	}
	if !contains(out2, "new program") {
		t.Errorf("program.md content missing on branch; got:\n%s", out2)
	}
}

func TestDefaultWorkspaceWriter_NoChanges_NoCommit(t *testing.T) {
	repo := newGitRepoWithBranch(t, "evolve-noop")
	root := t.TempDir()
	w := DefaultWorkspaceWriter{Root: root}

	// Write a file equal to what's already (effectively) on the branch:
	// no file → empty map yields nothing-to-commit.
	if err := w.WriteAndCommit(repo, "evolve-noop", "noop commit", map[string]string{}); err != nil {
		t.Fatalf("WriteAndCommit: %v", err)
	}
	out, _ := runIn(repo, "git", "log", "--oneline", "evolve-noop")
	if contains(out, "noop commit") {
		t.Errorf("must not commit when there are no changes; log:\n%s", out)
	}
}

func TestDefaultWorkspaceWriter_RemovesEphemeralWorktree(t *testing.T) {
	repo := newGitRepoWithBranch(t, "evolve-cleanup")
	root := t.TempDir()
	w := DefaultWorkspaceWriter{Root: root}

	if err := w.WriteAndCommit(repo, "evolve-cleanup", "x", map[string]string{"a.md": "b"}); err != nil {
		t.Fatalf("WriteAndCommit: %v", err)
	}
	// The worktree directory under root must be gone.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.IsDir() && contains(e.Name(), "evolve-cleanup") {
			t.Errorf("ephemeral worktree must be removed; found %s", e.Name())
		}
	}
}

func TestBranchToSlug(t *testing.T) {
	cases := map[string]string{
		"autoresearch/evolve-20260503": "autoresearch-evolve-20260503",
		"feat:foo.bar":                 "feat-foo-bar",
		"plain":                        "plain",
	}
	for in, want := range cases {
		if got := branchToSlug(in); got != want {
			t.Errorf("branchToSlug(%q) = %q; want %q", in, got, want)
		}
	}
}
