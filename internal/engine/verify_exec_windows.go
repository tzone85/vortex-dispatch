//go:build windows

package engine

import "os/exec"

// setProcessGroup is a no-op on Windows: exec.CommandContext kills the
// direct child; grandchildren are bounded by cmd.WaitDelay.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup is a no-op on Windows: there is no process group to kill,
// and the direct child has already exited when this is called (after every
// run, whichever way it ended).
func killProcessGroup(cmd *exec.Cmd) {}
