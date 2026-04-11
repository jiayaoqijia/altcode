//go:build !windows

package hooks

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup makes the spawned hook the leader of a new
// process group so we can SIGKILL the entire group on timeout.
// Without this, sh -c "..." gets killed but its grandchildren survive
// and can outlive the engine, accumulating across long sessions.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the entire process group rooted at
// cmd. Negative pid is the POSIX convention for "this whole pgroup".
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
