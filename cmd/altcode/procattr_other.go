//go:build !linux

package main

import "os/exec"

// setSysProcAttr is a no-op on non-Linux platforms.
// Setsid is Linux-specific; darwin and windows need different approaches.
func setSysProcAttr(_ *exec.Cmd) {}
