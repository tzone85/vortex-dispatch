//go:build !windows

package engine

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup runs the test runner in its own process group and, on
// context cancellation, kills the whole group so pkg.test binaries and node
// children do not outlive the gate. The direct child is killed as well, in
// case it moved itself out of the group. A group and child that are already
// gone (ESRCH) are reported as os.ErrProcessDone, which exec treats as
// success rather than wrapping it as "exec: canceling Cmd".
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		groupErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		childErr := syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(groupErr, syscall.ESRCH) && errors.Is(childErr, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if groupErr != nil && !errors.Is(groupErr, syscall.ESRCH) {
			return groupErr
		}
		return nil
	}
}

// killProcessGroup kills whatever is left of the runner's process group once
// the run has been read, however it ended: a child that inherited the output
// pipe and held it past cmd.WaitDelay, or one a failing runner left behind
// (Wait reports the ExitError, never ErrWaitDelay, for those). Best-effort;
// the group is usually already gone.
//
// Wait has reaped the leader by now, so in principle the kernel could have
// recycled its pid as another group's leader and the signal would land there.
// The window is between Wait returning and this call — microseconds, and it
// needs the pid table to wrap in that time.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
