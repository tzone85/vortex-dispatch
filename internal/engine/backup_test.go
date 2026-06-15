package engine

import (
	"archive/tar"
	"compress/gzip"
	"io"
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

// TestCreateBackup_RecursesNested pins the recursive-walk behaviour added
// to CreateBackup. The prior implementation skipped any top-level
// directory entry, so nested state (projects/<name>/events.jsonl,
// logs/<day>/agent.log, artifacts/<id>/diff.patch) was silently
// omitted despite the docstring calling this a "project state archive".
// The fix walks the tree and includes both directory headers and file
// bodies.
func TestCreateBackup_RecursesNested(t *testing.T) {
	stateDir := t.TempDir()

	// Build a nested layout that mirrors a real ~/.vxd:
	//   events.jsonl                      (top-level file)
	//   projects/clientA/events.jsonl     (nested file)
	//   projects/clientA/store.db         (nested file)
	//   logs/2026-06-15/agent.log         (deeper nested file)
	mustWrite := func(rel, body string) {
		full := filepath.Join(stateDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", full, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	mustWrite("events.jsonl", `{"type":"REQ_SUBMITTED"}`+"\n")
	mustWrite("projects/clientA/events.jsonl", `{"type":"STORY_CREATED"}`+"\n")
	mustWrite("projects/clientA/store.db", "sqlite-data")
	mustWrite("logs/2026-06-15/agent.log", "tmux capture")

	outPath, err := CreateBackup(stateDir, t.TempDir())
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	files := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		buf, _ := io.ReadAll(tr)
		files[hdr.Name] = string(buf)
	}

	want := map[string]string{
		"events.jsonl":                    `{"type":"REQ_SUBMITTED"}` + "\n",
		"projects/clientA/events.jsonl":   `{"type":"STORY_CREATED"}` + "\n",
		"projects/clientA/store.db":       "sqlite-data",
		"logs/2026-06-15/agent.log":       "tmux capture",
	}
	for name, body := range want {
		got, ok := files[name]
		if !ok {
			t.Errorf("archive missing %s (nested file was silently skipped before the recursion fix)", name)
			continue
		}
		if got != body {
			t.Errorf("archive %s body = %q, want %q", name, got, body)
		}
	}
}
