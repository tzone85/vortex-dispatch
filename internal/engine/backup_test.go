package engine

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateBackup_CreatesArchive(t *testing.T) {
	stateDir := t.TempDir()
	os.WriteFile(filepath.Join(stateDir, "events.jsonl"), []byte(`{"type":"REQ_SUBMITTED"}`+"\n"), 0o600)
	os.WriteFile(filepath.Join(stateDir, "store.db"), []byte("sqlite-data"), 0o600)

	outDir := t.TempDir()
	outPath, err := CreateBackup(stateDir, outDir)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("archive not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("archive is empty")
	}

	// Verify contents
	f, _ := os.Open(outPath)
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	defer gz.Close()
	tr := tar.NewReader(gz)

	files := make(map[string]bool)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		files[hdr.Name] = true
	}
	if !files["events.jsonl"] {
		t.Error("missing events.jsonl")
	}
	if !files["store.db"] {
		t.Error("missing store.db")
	}
}

func TestCreateBackup_EmptyDir(t *testing.T) {
	outPath, err := CreateBackup(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	if outPath == "" {
		t.Error("should return path even for empty dir")
	}
}

func TestCreateBackup_MissingDir(t *testing.T) {
	_, err := CreateBackup("/nonexistent/state", t.TempDir())
	if err == nil {
		t.Error("expected error for missing state dir")
	}
}
