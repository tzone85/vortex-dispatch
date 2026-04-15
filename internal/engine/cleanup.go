package engine

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CleanupLogs deletes .log files in logDir older than retentionDays.
// Returns the count of deleted files. Skips if retentionDays <= 0 or dir missing.
func CleanupLogs(logDir string, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	deleted := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(logDir, entry.Name())); err == nil {
				deleted++
			}
		}
	}
	return deleted, nil
}
