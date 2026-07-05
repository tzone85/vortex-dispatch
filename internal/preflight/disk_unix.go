//go:build !windows

package preflight

import "golang.org/x/sys/unix"

// freeDiskBytes returns the bytes available to unprivileged processes on the
// filesystem containing path.
func freeDiskBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	// Bsize's Go type differs per platform (uint32 darwin, int64 linux).
	return st.Bavail * uint64(st.Bsize), nil
}
