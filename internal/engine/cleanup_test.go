package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupLogs_DeletesExpiredLogs(t *testing.T) {
	logDir := t.TempDir()

	oldLog := filepath.Join(logDir, "old-story.log")
	if err := os.WriteFile(oldLog, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-40 * 24 * time.Hour)
	os.Chtimes(oldLog, oldTime, oldTime)

	recentLog := filepath.Join(logDir, "recent-story.log")
	if err := os.WriteFile(recentLog, []byte("recent"), 0o600); err != nil {
		t.Fatal(err)
	}
	recentTime := time.Now().Add(-5 * 24 * time.Hour)
	os.Chtimes(recentLog, recentTime, recentTime)

	otherFile := filepath.Join(logDir, "notes.txt")
	if err := os.WriteFile(otherFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(otherFile, oldTime, oldTime)

	deleted, err := CleanupLogs(logDir, 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Error("old log should be deleted")
	}
	if _, err := os.Stat(recentLog); err != nil {
		t.Error("recent log should be kept")
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Error("non-log file should be kept")
	}
}

func TestCleanupLogs_ZeroRetentionSkips(t *testing.T) {
	logDir := t.TempDir()
	f := filepath.Join(logDir, "old.log")
	os.WriteFile(f, []byte("x"), 0o600)
	old := time.Now().Add(-100 * 24 * time.Hour)
	os.Chtimes(f, old, old)

	deleted, _ := CleanupLogs(logDir, 0)
	if deleted != 0 {
		t.Error("zero retention should skip")
	}
}

func TestCleanupLogs_MissingDir(t *testing.T) {
	deleted, err := CleanupLogs("/nonexistent/path/logs", 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Error("missing dir should delete nothing")
	}
}
