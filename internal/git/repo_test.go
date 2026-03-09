package git_test

import (
	"os"
	"path/filepath"
	"testing"

	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
)

func TestCurrentBranch(t *testing.T) {
	repo := createTestRepo(t)
	branch, err := vxdgit.CurrentBranch(repo)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch == "" {
		t.Fatal("expected non-empty branch name")
	}
}

func TestBranchExists(t *testing.T) {
	repo := createTestRepo(t)
	branch, err := vxdgit.CurrentBranch(repo)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}

	if !vxdgit.BranchExists(repo, branch) {
		t.Fatalf("expected branch %s to exist", branch)
	}
	if vxdgit.BranchExists(repo, "nonexistent-branch") {
		t.Fatal("nonexistent branch should not exist")
	}
}

func TestCreateBranch(t *testing.T) {
	repo := createTestRepo(t)

	err := vxdgit.CreateBranch(repo, "feature/new")
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	if !vxdgit.BranchExists(repo, "feature/new") {
		t.Fatal("branch should exist after creation")
	}
}

func TestDeleteBranch(t *testing.T) {
	repo := createTestRepo(t)

	if err := vxdgit.CreateBranch(repo, "to-delete"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := vxdgit.DeleteBranch(repo, "to-delete"); err != nil {
		t.Fatalf("delete branch: %v", err)
	}
	if vxdgit.BranchExists(repo, "to-delete") {
		t.Fatal("branch should not exist after deletion")
	}
}

func TestScanRepo_Go(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644); err != nil {
		t.Fatal(err)
	}

	stack := vxdgit.ScanRepo(dir)
	if stack.Language != "go" {
		t.Fatalf("expected 'go', got %s", stack.Language)
	}
	if stack.BuildTool != "go" {
		t.Fatalf("expected build tool 'go', got %s", stack.BuildTool)
	}
}

func TestScanRepo_Node(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	stack := vxdgit.ScanRepo(dir)
	if stack.Language != "javascript" {
		t.Fatalf("expected 'javascript', got %s", stack.Language)
	}
}

func TestScanRepo_TypeScript(t *testing.T) {
	dir := t.TempDir()
	// Both package.json and tsconfig.json present -- TypeScript should win.
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	stack := vxdgit.ScanRepo(dir)
	if stack.Language != "typescript" {
		t.Fatalf("expected 'typescript', got %s", stack.Language)
	}
}

func TestScanRepo_Empty(t *testing.T) {
	dir := t.TempDir()
	stack := vxdgit.ScanRepo(dir)
	if stack.Language != "" {
		t.Fatalf("expected empty language for empty dir, got %s", stack.Language)
	}
}
