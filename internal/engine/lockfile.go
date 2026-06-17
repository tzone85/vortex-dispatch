package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LockInfo holds the metadata stored inside a VXD lock file.
type LockInfo struct {
	PID       int    `json:"pid"`
	ReqID     string `json:"req_id"`
	StartedAt string `json:"started_at"`
}

// AcquireLock creates an advisory lock file at lockPath. If an existing lock
// is found, it checks whether the owning process is still alive. A dead PID
// is treated as stale and reclaimed; a live PID causes an error.
func AcquireLock(lockPath, reqID string) (LockInfo, error) {
	existing, err := readLockFile(lockPath)
	if err == nil {
		if isProcessAlive(existing.PID) {
			return LockInfo{}, fmt.Errorf(
				"another VXD process is running (PID %d, req %s, since %s)",
				existing.PID, existing.ReqID, existing.StartedAt,
			)
		}
		// Stale lock — fall through and reclaim.
	}

	info := LockInfo{
		PID:       os.Getpid(),
		ReqID:     reqID,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := writeLockFile(lockPath, info); err != nil {
		return LockInfo{}, fmt.Errorf("write lock file: %w", err)
	}

	return info, nil
}

// ForceAcquireLock unconditionally writes a new lock file, overriding any
// existing lock regardless of whether its owning process is alive.
func ForceAcquireLock(lockPath, reqID string) (LockInfo, error) {
	info := LockInfo{
		PID:       os.Getpid(),
		ReqID:     reqID,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := writeLockFile(lockPath, info); err != nil {
		return LockInfo{}, fmt.Errorf("force write lock file: %w", err)
	}

	return info, nil
}

// ReleaseLock removes the lock file at lockPath. It is safe to call even
// when the file does not exist.
func ReleaseLock(lockPath string) {
	_ = os.Remove(lockPath)
}

// ReadLock reads and returns the current lock info from lockPath.
func ReadLock(lockPath string) (LockInfo, error) {
	return readLockFile(lockPath)
}

// --- internal helpers ---

// readLockFile reads and decodes the JSON lock file at path.
func readLockFile(path string) (LockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LockInfo{}, fmt.Errorf("read lock file: %w", err)
	}

	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return LockInfo{}, fmt.Errorf("decode lock file: %w", err)
	}

	return info, nil
}

// writeLockFile encodes info as JSON and writes it atomically to path.
func writeLockFile(path string, info LockInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("encode lock info: %w", err)
	}

	// 0o600: lock info exposes PID and state paths; keep it owner-only,
	// consistent with the pidfile/token/checkpoint files.
	return os.WriteFile(path, data, 0o600)
}

// isProcessAlive is implemented per-OS (see lockfile_unix.go / lockfile_windows.go).
