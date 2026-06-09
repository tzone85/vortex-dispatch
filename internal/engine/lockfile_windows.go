//go:build windows

package engine

import (
	"golang.org/x/sys/windows"
)

// isProcessAlive returns true when OpenProcess succeeds for pid. Windows has
// no Unix-style signal 0 probe; instead we open a query handle and close it
// immediately. ERROR_INVALID_PARAMETER (the typical "no such PID" result) and
// ERROR_ACCESS_DENIED (process exists but we lack rights) are both treated
// honestly: the latter still indicates a live process, so we report alive.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err == nil {
		_ = windows.CloseHandle(h)
		return true
	}
	if err == windows.ERROR_ACCESS_DENIED {
		return true
	}
	return false
}
