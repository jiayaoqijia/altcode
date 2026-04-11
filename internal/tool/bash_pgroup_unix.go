//go:build !windows

package tool

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup makes the spawned process the leader of a new
// process group so we can later signal the entire group instead of
// just the parent. Required for cleaning up background children
// spawned via shell '&' or pipelines.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the entire process group rooted at
// the given command. Negative PID means "the process group with this
// pgid" — kill(2) semantics.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Fall back to killing just the parent PID.
		return cmd.Process.Kill()
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
