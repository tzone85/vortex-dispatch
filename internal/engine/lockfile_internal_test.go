package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLockFile_NonExistent(t *testing.T) {
	_, err := readLockFile("/tmp/nonexistent-lock-xyz.lock")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadLockFile_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.lock")
	os.WriteFile(path, []byte("not json"), 0o644)

	_, err := readLockFile(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWriteLockFile_Success(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	info := LockInfo{PID: 42, ReqID: "r-001", StartedAt: "2026-01-01T00:00:00Z"}

	if err := writeLockFile(path, info); err != nil {
		t.Fatalf("writeLockFile: %v", err)
	}

	got, err := readLockFile(path)
	if err != nil {
		t.Fatalf("readLockFile: %v", err)
	}
	if got.PID != 42 {
		t.Errorf("expected PID 42, got %d", got.PID)
	}
	if got.ReqID != "r-001" {
		t.Errorf("expected req ID r-001, got %s", got.ReqID)
	}
}

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Error("expected current process to be alive")
	}
}

func TestIsProcessAlive_DeadPID(t *testing.T) {
	// PID 999999 almost certainly doesn't exist
	if isProcessAlive(999999) {
		t.Skip("PID 999999 unexpectedly alive")
	}
}

func TestForceAcquireLock_OverridesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	// Write initial lock
	writeLockFile(path, LockInfo{PID: 1, ReqID: "old"})

	// Force acquire should overwrite
	info, err := ForceAcquireLock(path, "new-req")
	if err != nil {
		t.Fatalf("ForceAcquireLock: %v", err)
	}
	if info.ReqID != "new-req" {
		t.Errorf("expected new-req, got %s", info.ReqID)
	}

	// Verify file was overwritten
	got, err := readLockFile(path)
	if err != nil {
		t.Fatalf("readLockFile: %v", err)
	}
	if got.ReqID != "new-req" {
		t.Errorf("expected overwritten lock, got %s", got.ReqID)
	}
}
