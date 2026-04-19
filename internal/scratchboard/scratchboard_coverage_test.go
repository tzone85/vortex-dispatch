package scratchboard

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- New error path ---

func TestNew_InvalidPath(t *testing.T) {
	// /dev/null is not a directory, so MkdirAll should fail
	_, err := New("/dev/null/subdir/file.jsonl")
	if err == nil {
		t.Error("expected error for invalid parent path")
	}
}

// --- Write with auto-timestamp ---

func TestWrite_AutoTimestamp(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(filepath.Join(dir, "board.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	entry := Entry{
		AgentID:  "agent-1",
		StoryID:  "s1",
		Category: "discovery",
		Content:  "Found API endpoint",
	}
	if err := sb.Write(entry); err != nil {
		t.Fatal(err)
	}

	entries, err := sb.Read("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("expected auto-generated timestamp")
	}
}

// --- Write with explicit timestamp ---

func TestWrite_ExplicitTimestamp(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(filepath.Join(dir, "board.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := Entry{
		AgentID:   "agent-1",
		StoryID:   "s1",
		Category:  "test",
		Content:   "Test",
		Timestamp: ts,
	}
	if err := sb.Write(entry); err != nil {
		t.Fatal(err)
	}

	entries, err := sb.Read("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !entries[0].Timestamp.Equal(ts) {
		t.Errorf("expected explicit timestamp, got %v", entries[0].Timestamp)
	}
}

// --- Read with default limit ---

func TestRead_DefaultLimit(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(filepath.Join(dir, "board.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 25; i++ {
		sb.Write(Entry{AgentID: "a", StoryID: "s", Category: "cat", Content: "data"})
	}

	entries, err := sb.Read("", 0) // 0 means use default limit
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MaxReadEntries {
		t.Errorf("expected %d entries with default limit, got %d", MaxReadEntries, len(entries))
	}
}

// --- Read newest first order ---

func TestRead_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(filepath.Join(dir, "board.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	sb.Write(Entry{AgentID: "a", StoryID: "s1", Category: "cat", Content: "first"})
	sb.Write(Entry{AgentID: "a", StoryID: "s2", Category: "cat", Content: "second"})
	sb.Write(Entry{AgentID: "a", StoryID: "s3", Category: "cat", Content: "third"})

	entries, err := sb.Read("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Content != "third" {
		t.Errorf("expected newest first, got %q", entries[0].Content)
	}
	if entries[2].Content != "first" {
		t.Errorf("expected oldest last, got %q", entries[2].Content)
	}
}

// --- Read with category filter ---

func TestRead_CategoryFilter_NoMatch(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(filepath.Join(dir, "board.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	sb.Write(Entry{AgentID: "a", StoryID: "s1", Category: "bug", Content: "data"})

	entries, err := sb.Read("feature", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for non-matching category, got %d", len(entries))
	}
}

// --- Snapshot coverage ---

func TestSnapshot_Empty(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(filepath.Join(dir, "board.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	snap := sb.Snapshot(10)
	if snap != "" {
		t.Errorf("expected empty snapshot, got %q", snap)
	}
}

func TestSnapshot_WithEntries(t *testing.T) {
	dir := t.TempDir()
	sb, err := New(filepath.Join(dir, "board.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	sb.Write(Entry{AgentID: "a", StoryID: "s1", Category: "api", Content: "Found endpoint /v1"})
	sb.Write(Entry{AgentID: "b", StoryID: "s2", Category: "config", Content: "Needs env var"})

	snap := sb.Snapshot(10)
	if !strings.Contains(snap, "Shared Discoveries") {
		t.Error("expected header in snapshot")
	}
	if !strings.Contains(snap, "Found endpoint") {
		t.Error("expected entry content in snapshot")
	}
}

// --- splitLines coverage ---

func TestSplitLines_NoTrailingNewline(t *testing.T) {
	data := []byte("line1\nline2")
	lines := splitLines(data)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestSplitLines_TrailingNewline(t *testing.T) {
	data := []byte("line1\nline2\n")
	lines := splitLines(data)
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestSplitLines_EmptyInput(t *testing.T) {
	lines := splitLines([]byte{})
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestSplitLines_SingleLine(t *testing.T) {
	data := []byte("oneline")
	lines := splitLines(data)
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
}
