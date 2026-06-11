package memory

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// GetCommitsForDate — edge cases
// ---------------------------------------------------------------------------

func TestGetCommitsForDate_InvalidDate(t *testing.T) {
	commits := GetCommitsForDate("/tmp", "not-a-date")
	if commits != nil {
		t.Errorf("expected nil for invalid date, got %v", commits)
	}
}

func TestGetCommitsForDate_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	commits := GetCommitsForDate(dir, "2026-01-01")
	if commits != nil {
		t.Errorf("expected nil for non-git dir, got %v", commits)
	}
}

func TestGetCommitsForDate_ValidGitRepo(t *testing.T) {
	dir := t.TempDir()

	// Init git repo and create a commit
	exec.Command("git", "init", dir).Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init commit").Run()

	// Get today's date
	today := "2026-04-13"
	commits := GetCommitsForDate(dir, today)
	// May or may not find commits depending on current date, but should not crash
	_ = commits
}

// ---------------------------------------------------------------------------
// SearchMemPalace — empty query
// ---------------------------------------------------------------------------

func TestSearchMemPalace_EmptyQuery(t *testing.T) {
	results, err := SearchMemPalace(context.Background(), "")
	if err != nil {
		t.Errorf("expected nil error for empty query, got: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty query, got %v", results)
	}
}

// ---------------------------------------------------------------------------
// ParseSearchResults — edge cases
// ---------------------------------------------------------------------------

func TestParseSearchResults_WhitespaceOnly(t *testing.T) {
	results := ParseSearchResults("   \n\n  ")
	if results != nil {
		t.Errorf("expected nil for whitespace-only input, got %v", results)
	}
}

func TestParseSearchResults_MissingCloseBracket(t *testing.T) {
	// Header without closing bracket should be handled gracefully
	raw := "[1 wing / room\n    Source: file.md\n    Match: 0.5\n\n    content here\n"
	results := ParseSearchResults(raw)
	// The parser should still produce a result since it finds the "]" character
	// inside the string — but "1 wing" doesn't have "]" so it might skip
	_ = results // just verify no panic
}

func TestParseSearchResults_ContentBeforeFirstHeader(t *testing.T) {
	// Content before any header should be ignored
	raw := "Some preamble text\n[1] wing / room\n    Source: file.md\n    Match: 0.5\n\n    actual content\n"
	results := ParseSearchResults(raw)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Wing != "wing" {
		t.Errorf("expected wing 'wing', got '%s'", results[0].Wing)
	}
}

// ---------------------------------------------------------------------------
// MarshalInit — edge case
// ---------------------------------------------------------------------------

func TestMarshalInit_EmptyAuditDir(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, dir, 0)

	data, err := srv.MarshalInit()
	if err != nil {
		t.Fatalf("marshal init: %v", err)
	}

	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != "init" {
		t.Errorf("expected type 'init', got %s", msg.Type)
	}
}

// ---------------------------------------------------------------------------
// Handler — returns non-nil mux
// ---------------------------------------------------------------------------

func TestHandler_ReturnsHandler(t *testing.T) {
	dir := t.TempDir()
	srv := NewServer(dir, dir, 0)

	handler := srv.Handler()
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

// ---------------------------------------------------------------------------
// sendInit — with empty timeline
// ---------------------------------------------------------------------------

func TestNewServer_Fields(t *testing.T) {
	srv := NewServer("/audit", "/repo", 8080)
	if srv.auditDir != "/audit" {
		t.Errorf("expected auditDir '/audit', got '%s'", srv.auditDir)
	}
	if srv.repoDir != "/repo" {
		t.Errorf("expected repoDir '/repo', got '%s'", srv.repoDir)
	}
	if srv.port != 8080 {
		t.Errorf("expected port 8080, got %d", srv.port)
	}
	if srv.opportunitiesDir != "/repo/docs/opportunities" {
		t.Errorf("unexpected opportunitiesDir: %s", srv.opportunitiesDir)
	}
}

// ---------------------------------------------------------------------------
// IsMemPalaceAvailable — basic functionality
// ---------------------------------------------------------------------------

func TestIsMemPalaceAvailable_DoesNotPanic(t *testing.T) {
	// Just verify it doesn't panic
	_ = IsMemPalaceAvailable()
}

// ---------------------------------------------------------------------------
// ReadChangelog — with valid entries
// ---------------------------------------------------------------------------

func TestReadChangelog_WithDates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changelog.jsonl")

	entries := []string{
		`{"date":"2026-01-01","prs_created":2,"findings_relevant":5}`,
		`{"date":"2026-01-02","prs_created":1,"findings_relevant":3}`,
	}
	data := ""
	for _, e := range entries {
		data += e + "\n"
	}
	os.WriteFile(path, []byte(data), 0o644)

	result, err := readChangelog(path)
	if err != nil {
		t.Fatalf("read changelog: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// ActivityLevel — boundary cases
// ---------------------------------------------------------------------------

func TestActivityLevel_Boundaries(t *testing.T) {
	tests := []struct {
		prs, findings, commits int
		expected               int
	}{
		{0, 0, 0, 0},   // zero
		{0, 1, 0, 1},   // total=1
		{0, 3, 0, 1},   // total=3 (boundary)
		{0, 4, 0, 2},   // total=4
		{0, 8, 0, 2},   // total=8 (boundary)
		{0, 9, 0, 3},   // total=9
		{3, 0, 0, 3},   // prs * 3 = 9
		{1, 1, 1, 2},   // 3+1+1 = 5
	}
	for _, tt := range tests {
		got := ActivityLevel(tt.prs, tt.findings, tt.commits)
		if got != tt.expected {
			t.Errorf("ActivityLevel(%d,%d,%d) = %d, want %d",
				tt.prs, tt.findings, tt.commits, got, tt.expected)
		}
	}
}
