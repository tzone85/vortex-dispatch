package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWaveContext_NoFile(t *testing.T) {
	dir := t.TempDir()
	ctx := ReadWaveContext(dir)
	if ctx != "" {
		t.Errorf("expected empty string for missing file, got %q", ctx)
	}
}

func TestReadWaveContext_WithFile(t *testing.T) {
	dir := t.TempDir()
	content := "# Wave Context\n\n### s-001: Build store\n\nFiles: store.go\n"
	os.WriteFile(filepath.Join(dir, waveContextFileName), []byte(content), 0o644)

	ctx := ReadWaveContext(dir)
	if !strings.Contains(ctx, "s-001") {
		t.Error("expected wave context to contain s-001")
	}
}

func TestReadWaveContext_Truncation(t *testing.T) {
	dir := t.TempDir()
	// Create a very large context file
	content := "# Wave Context\n\n"
	for i := 0; i < 100; i++ {
		content += "### s-" + string(rune('a'+i%26)) + ": Story " + string(rune('0'+i%10)) + "\n\nLots of content here that fills up space.\n\n"
	}
	os.WriteFile(filepath.Join(dir, waveContextFileName), []byte(content), 0o644)

	ctx := ReadWaveContext(dir)
	if len(ctx) > 4200 { // 4000 + some overhead from truncation prefix
		t.Errorf("expected truncated context, got %d chars", len(ctx))
	}
}

func TestAppendToWaveContext_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, waveContextFileName)

	appendToWaveContext(path, "s-001", "### s-001: Built store\n\n")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "s-001") {
		t.Error("expected file to contain s-001")
	}
	if !strings.Contains(string(data), "Wave Context") {
		t.Error("expected file to contain header")
	}
}

func TestAppendToWaveContext_NoDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, waveContextFileName)

	appendToWaveContext(path, "s-001", "### s-001: Built store\n\n")
	appendToWaveContext(path, "s-001", "### s-001: Built store\n\n") // duplicate

	data, _ := os.ReadFile(path)
	count := strings.Count(string(data), "s-001:")
	if count != 1 {
		t.Errorf("expected 1 occurrence of s-001, got %d", count)
	}
}

func TestExtractFuncName(t *testing.T) {
	tests := []struct {
		line, want string
	}{
		{"func FooBar(x int) error", "FooBar"},
		{"func (s *Store) Get(key string) (string, error)", "Get"},
		{"func main()", "main"},
		{"func (r *Router) HandleFunc(pattern string, handler func())", "HandleFunc"},
	}
	for _, tt := range tests {
		got := extractFuncName(tt.line)
		if got != tt.want {
			t.Errorf("extractFuncName(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestFormatFileList(t *testing.T) {
	result := formatFileList("store/store.go\nstore/store_test.go\n")
	if !strings.Contains(result, "`store/store.go`") {
		t.Error("expected formatted file list")
	}
}

func TestFormatFileList_Empty(t *testing.T) {
	result := formatFileList("")
	if !strings.Contains(result, "no files detected") {
		t.Errorf("expected 'no files detected', got %q", result)
	}
}
