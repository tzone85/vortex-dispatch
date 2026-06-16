//go:build windows

package dashstart

import (
	"os/exec"
	"syscall"
)

// createNewProcessGroup is the Windows flag that detaches a child from the
// parent's console process group, mirroring setsid on Unix. Defined here so
// the package self-contains its detach behavior without importing the cli
// package's platform helpers.
const createNewProcessGroup = 0x00000200

// ApplyDetach sets the platform-specific syscall attributes that let the
// spawned daemon survive parent-shell teardown. On Windows that means
// CREATE_NEW_PROCESS_GROUP plus DETACHED_PROCESS so closing the parent
// console doesn't take the daemon down.
func ApplyDetach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
}
