package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadMetadata_Internal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproject")

	meta := ProjectMetadata{
		Name:     "acme-corp-api",
		RepoPath: "/home/user/acme-corp-api",
	}

	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}

	got, err := ReadMetadata(dir)
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}

	if got.Name != "acme-corp-api" {
		t.Errorf("expected name acme-corp-api, got %q", got.Name)
	}
	if got.RepoPath != "/home/user/acme-corp-api" {
		t.Errorf("expected repo path, got %q", got.RepoPath)
	}
}

func TestWriteMetadata_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep", "project")

	meta := ProjectMetadata{Name: "test"}
	if err := WriteMetadata(dir, meta); err != nil {
		t.Fatalf("WriteMetadata: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestReadMetadata_NonExistent(t *testing.T) {
	_, err := ReadMetadata("/tmp/nonexistent-project-dir-xyz")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestReadMetadata_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("not json"), 0o644)

	_, err := ReadMetadata(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
