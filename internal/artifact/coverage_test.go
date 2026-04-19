package artifact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Init method
// ---------------------------------------------------------------------------

func TestStore_Init_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.Init("story-init-test"); err != nil {
		t.Fatalf("init: %v", err)
	}

	storyDir := filepath.Join(dir, "story-init-test")
	info, err := os.Stat(storyDir)
	if err != nil {
		t.Fatalf("stat story dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestStore_Init_Idempotent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	// Init twice should not error
	store.Init("story-idem")
	if err := store.Init("story-idem"); err != nil {
		t.Errorf("second init should succeed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NewStore — edge cases
// ---------------------------------------------------------------------------

func TestNewStore_CreatesBaseDir(t *testing.T) {
	dir := t.TempDir()
	baseDir := filepath.Join(dir, "deep", "nested", "artifacts")

	store, err := NewStore(baseDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	info, err := os.Stat(baseDir)
	if err != nil {
		t.Fatalf("stat base dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

// ---------------------------------------------------------------------------
// WriteRaw — different artifact types get different extensions
// ---------------------------------------------------------------------------

func TestStore_WriteRaw_GitDiffExtension(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.WriteRaw("story-ext", TypeGitDiff, "diff content"); err != nil {
		t.Fatalf("write raw: %v", err)
	}

	// Should use .patch extension
	path := filepath.Join(dir, "story-ext", string(TypeGitDiff)+".patch")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "diff content" {
		t.Errorf("expected 'diff content', got %s", string(data))
	}
}

func TestStore_WriteRaw_RawLogExtension(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.WriteRaw("story-log", TypeRawLog, "log content"); err != nil {
		t.Fatalf("write raw: %v", err)
	}

	// Should use .txt extension for non-diff types
	path := filepath.Join(dir, "story-log", string(TypeRawLog)+".txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "log content" {
		t.Errorf("expected 'log content', got %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Write — error case: unmarshalable data
// ---------------------------------------------------------------------------

func TestStore_Write_MarshalError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	// chan cannot be marshaled to JSON
	err = store.Write("story-bad", TypeLaunchConfig, make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshalable data")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("expected marshal error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Append — error case: unmarshalable data
// ---------------------------------------------------------------------------

func TestStore_Append_MarshalError(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	err = store.Append("story-bad-append", TypeTraceEvents, make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshalable data")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("expected marshal error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Read — error case: nonexistent file
// ---------------------------------------------------------------------------

func TestStore_Read_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	_, err = store.Read("nonexistent-story", "file.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// List — directory listing edge case
// ---------------------------------------------------------------------------

func TestStore_List_WithSubdirectories(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	storyDir := filepath.Join(dir, "story-subdir")
	os.MkdirAll(storyDir, 0o755)
	os.MkdirAll(filepath.Join(storyDir, "subdir"), 0o755) // subdirectory
	os.WriteFile(filepath.Join(storyDir, "file1.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(storyDir, "file2.txt"), []byte("data"), 0o644)

	names, err := store.List("story-subdir")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// Should only list files, not directories
	if len(names) != 2 {
		t.Errorf("expected 2 files (no subdirs), got %d: %v", len(names), names)
	}
}

// ---------------------------------------------------------------------------
// Type constants
// ---------------------------------------------------------------------------

func TestTypeConstants_AreDistinct(t *testing.T) {
	types := []Type{TypeLaunchConfig, TypeTraceEvents, TypeGitDiff, TypeQAResult, TypeReviewResult, TypeRawLog}
	seen := make(map[Type]bool)
	for _, ty := range types {
		if seen[ty] {
			t.Errorf("duplicate type constant: %s", ty)
		}
		seen[ty] = true
	}
}
