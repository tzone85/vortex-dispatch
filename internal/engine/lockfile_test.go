package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/engine"
)

func TestAcquireLock_CreatesFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")

	info, err := engine.AcquireLock(lockPath, "req-001")
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	if info.PID != os.Getpid() {
		t.Fatalf("expected PID %d, got %d", os.Getpid(), info.PID)
	}
	if info.ReqID != "req-001" {
		t.Fatalf("expected ReqID req-001, got %s", info.ReqID)
	}
	if info.StartedAt == "" {
		t.Fatal("expected non-empty StartedAt")
	}

	// Verify file exists on disk.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lock file was not created")
	}
}

func TestAcquireLock_BlocksSecondAcquire(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")

	_, err := engine.AcquireLock(lockPath, "req-001")
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}

	// Second acquire with same (live) PID should fail.
	_, err = engine.AcquireLock(lockPath, "req-002")
	if err == nil {
		t.Fatal("expected error on second AcquireLock, got nil")
	}
}

func TestReleaseLock_RemovesFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")

	_, err := engine.AcquireLock(lockPath, "req-001")
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	engine.ReleaseLock(lockPath)

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("lock file still exists after release")
	}
}

func TestReleaseLock_SafeWhenNoFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")
	// Should not panic when file doesn't exist.
	engine.ReleaseLock(lockPath)
}

func TestAcquireLock_ReclaimsStalePID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")

	// Write a lock with a PID that almost certainly doesn't exist.
	stale := engine.LockInfo{
		PID:       999999,
		ReqID:     "req-stale",
		StartedAt: "2026-01-01T00:00:00Z",
	}
	data, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal stale lock: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	// Acquire should succeed because PID 999999 is dead.
	info, err := engine.AcquireLock(lockPath, "req-002")
	if err != nil {
		t.Fatalf("AcquireLock over stale lock: %v", err)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("expected current PID %d, got %d", os.Getpid(), info.PID)
	}
	if info.ReqID != "req-002" {
		t.Fatalf("expected ReqID req-002, got %s", info.ReqID)
	}
}

func TestForceAcquireLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")

	// Acquire a lock normally.
	_, err := engine.AcquireLock(lockPath, "req-001")
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	// Force acquire should override even a live PID.
	info, err := engine.ForceAcquireLock(lockPath, "req-force")
	if err != nil {
		t.Fatalf("ForceAcquireLock: %v", err)
	}
	if info.ReqID != "req-force" {
		t.Fatalf("expected ReqID req-force, got %s", info.ReqID)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("expected current PID, got %d", info.PID)
	}
}

func TestReadLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")

	original, err := engine.AcquireLock(lockPath, "req-read")
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}

	info, err := engine.ReadLock(lockPath)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}

	if info.PID != original.PID {
		t.Fatalf("PID mismatch: %d vs %d", info.PID, original.PID)
	}
	if info.ReqID != original.ReqID {
		t.Fatalf("ReqID mismatch: %s vs %s", info.ReqID, original.ReqID)
	}
	if info.StartedAt != original.StartedAt {
		t.Fatalf("StartedAt mismatch: %s vs %s", info.StartedAt, original.StartedAt)
	}
}

func TestReadLock_NoFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "vxd.lock")

	_, err := engine.ReadLock(lockPath)
	if err == nil {
		t.Fatal("expected error reading non-existent lock, got nil")
	}
}
