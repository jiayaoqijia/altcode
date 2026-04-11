//go:build windows

package hooks

import "os/exec"

// configureProcessGroup is a no-op on Windows. The exec.CommandContext
// kill semantics already terminate the direct child; finer-grained
// process tree control would require Job Objects which we don't need
// in altcode hooks today.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup falls back to killing only the direct child.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
