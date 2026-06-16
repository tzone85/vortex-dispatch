//go:build !windows

package dashstart

import (
	"os/exec"
	"syscall"
)

// ApplyDetach sets the platform-specific syscall attributes that let the
// spawned daemon survive parent-shell teardown (e.g. closing the terminal
// that ran `vxd req`). On Unix this means a fresh session via setsid.
func ApplyDetach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
