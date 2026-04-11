//go:build windows

package mcp

import "os/exec"

// configureMCPProcessGroup is a no-op on Windows. The exec.CommandContext
// kill semantics already terminate the direct child; finer-grained
// process tree control would require Job Objects.
func configureMCPProcessGroup(cmd *exec.Cmd) {}

// killMCPProcessGroup falls back to killing only the direct child.
func killMCPProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
