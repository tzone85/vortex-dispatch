//go:build !windows

package engine

import (
	"os"
	"syscall"
)

// isProcessAlive returns true if a process with the given PID exists and is
// reachable via signal 0 — the standard Unix liveness probe.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
