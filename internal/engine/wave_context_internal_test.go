package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractFuncName_RegularFunc(t *testing.T) {
	got := extractFuncName("func HelloWorld(x int) error")
	if got != "HelloWorld" {
		t.Errorf("expected HelloWorld, got %q", got)
	}
}

func TestExtractFuncName_MethodFunc(t *testing.T) {
	got := extractFuncName("func (s *Store) Get(key string) (string, error)")
	if got != "Get" {
		t.Errorf("expected Get, got %q", got)
	}
}

func TestExtractFuncName_NoParen(t *testing.T) {
	got := extractFuncName("func incomplete")
	if got != "" {
		t.Errorf("expected empty for incomplete func, got %q", got)
	}
}

func TestExtractFuncName_UnclosedReceiver(t *testing.T) {
	got := extractFuncName("func (s *Store broken")
	if got != "" {
		t.Errorf("expected empty for unclosed receiver, got %q", got)
	}
}

func TestFormatFileList_WithFiles(t *testing.T) {
	got := formatFileList("main.go\nutils.go\nconfig.yaml")
	if !strings.Contains(got, "- `main.go`") {
		t.Error("expected main.go in list")
	}
	if !strings.Contains(got, "- `utils.go`") {
		t.Error("expected utils.go in list")
	}
	if !strings.Contains(got, "- `config.yaml`") {
		t.Error("expected config.yaml in list")
	}
}

func TestFormatFileList_EmptyInput(t *testing.T) {
	got := formatFileList("")
	if got != "- (no files detected)" {
		t.Errorf("expected fallback for empty, got %q", got)
	}
}

func TestFormatFileList_WhitespaceOnly(t *testing.T) {
	got := formatFileList("   \n  \n  ")
	if got != "- (no files detected)" {
		t.Errorf("expected fallback for whitespace, got %q", got)
	}
}

func TestExtractSignatures_GoFile(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "model.go")
	os.WriteFile(goFile, []byte(`package engine

type UserService struct {
	db *sql.DB
}

func NewUserService(db *sql.DB) *UserService {
	return &UserService{db: db}
}

func (s *UserService) GetUser(id string) (User, error) {
	return User{}, nil
}

func helper() {}
`), 0o644)

	got := extractSignatures(dir, "model.go")
	if !strings.Contains(got, "NewUserService") {
		t.Error("expected exported func NewUserService in signatures")
	}
	if !strings.Contains(got, "GetUser") {
		t.Error("expected exported method GetUser in signatures")
	}
	if strings.Contains(got, "helper") {
		t.Error("unexported func helper should not appear")
	}
	if !strings.Contains(got, "type UserService struct") {
		t.Error("expected type declaration in signatures")
	}
}

func TestExtractSignatures_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "model_test.go")
	os.WriteFile(testFile, []byte(`package engine

func TestSomething(t *testing.T) {}
`), 0o644)

	got := extractSignatures(dir, "model_test.go")
	if got != "" {
		t.Errorf("expected empty for test files, got %q", got)
	}
}

func TestExtractSignatures_NonGoFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("key: val"), 0o644)

	got := extractSignatures(dir, "config.yaml")
	if got != "" {
		t.Errorf("expected empty for non-Go files, got %q", got)
	}
}

func TestExtractSignatures_EmptyInput(t *testing.T) {
	dir := t.TempDir()
	got := extractSignatures(dir, "")
	if got != "" {
		t.Errorf("expected empty for empty input, got %q", got)
	}
}

func TestReadWaveContext_NonExistent(t *testing.T) {
	got := ReadWaveContext(t.TempDir())
	if got != "" {
		t.Errorf("expected empty for nonexistent file, got %q", got)
	}
}

func TestReadWaveContext_SmallFile(t *testing.T) {
	dir := t.TempDir()
	content := "# Wave Context\n\n### s-001: Task 1\nSome details\n"
	os.WriteFile(filepath.Join(dir, waveContextFileName), []byte(content), 0o644)

	got := ReadWaveContext(dir)
	if got != content {
		t.Errorf("expected content to match, got:\n%s", got)
	}
}

func TestReadWaveContext_LargeFileTruncated(t *testing.T) {
	dir := t.TempDir()
	// Build a file larger than 4000 chars
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("### s-")
		for j := 0; j < 50; j++ {
			b.WriteString("x")
		}
		b.WriteString("\nSome padding content here.\n\n")
	}
	content := b.String()
	os.WriteFile(filepath.Join(dir, waveContextFileName), []byte(content), 0o644)

	got := ReadWaveContext(dir)
	if len(got) > 4100 { // some overhead from truncation header
		t.Errorf("expected truncated content, got length %d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Error("expected truncation notice")
	}
}

func TestAppendToWaveContext_NewFile(t *testing.T) {
	dir := t.TempDir()
	ctxPath := filepath.Join(dir, waveContextFileName)

	appendToWaveContext(ctxPath, "s-001", "### s-001: My Story\n\nDetails\n")

	data, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "Wave Context") {
		t.Error("expected header in new file")
	}
	if !strings.Contains(string(data), "s-001") {
		t.Error("expected story entry")
	}
}

func TestAppendToWaveContext_NoDuplicate(t *testing.T) {
	dir := t.TempDir()
	ctxPath := filepath.Join(dir, waveContextFileName)

	appendToWaveContext(ctxPath, "s-001", "### s-001: My Story\n")
	appendToWaveContext(ctxPath, "s-001", "### s-001: My Story\n") // duplicate

	data, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	count := strings.Count(string(data), "s-001:")
	if count != 1 {
		t.Errorf("expected s-001 once, found %d times", count)
	}
}

func TestAppendToWaveContext_MultipleStories(t *testing.T) {
	dir := t.TempDir()
	ctxPath := filepath.Join(dir, waveContextFileName)

	appendToWaveContext(ctxPath, "s-001", "### s-001: First\n")
	appendToWaveContext(ctxPath, "s-002", "### s-002: Second\n")

	data, err := os.ReadFile(ctxPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "s-001:") {
		t.Error("expected first story")
	}
	if !strings.Contains(string(data), "s-002:") {
		t.Error("expected second story")
	}
}
