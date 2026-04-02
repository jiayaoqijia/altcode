//go:build !windows

package internal_test

import (
	"runtime"
	"testing"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Skipping: test uses Unix shell scripts")
	}
}
