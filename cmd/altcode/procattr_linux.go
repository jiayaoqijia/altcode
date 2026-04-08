//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr detaches the child into its own process group so it
// survives terminal close (Gap 1: process persistence).
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
