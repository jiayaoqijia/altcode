//go:build windows

package tool

import "os/exec"

// configureProcessGroup is a no-op on Windows; process groups work
// differently and the bash tool isn't a primary Windows path.
func configureProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup just kills the parent process on Windows.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
