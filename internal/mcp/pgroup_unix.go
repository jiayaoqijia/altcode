//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// configureMCPProcessGroup makes the spawned MCP server the leader of
// a new process group so Close can SIGKILL the entire group when the
// server ignores EOF on stdin. Without this, helper subprocesses
// spawned by the server outlive altcode.
func configureMCPProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killMCPProcessGroup sends SIGKILL to the entire process group rooted
// at cmd. Negative pid is the POSIX convention for "this whole pgroup".
func killMCPProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
