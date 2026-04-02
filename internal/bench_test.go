//go:build !windows

package internal_test

import (
	"os/exec"
	"testing"
	"time"
)

func TestStartupTime(t *testing.T) {
	build := exec.Command("go", "build", "-mod=mod", "-ldflags=-s -w", "-o", "/tmp/altcode-bench", "./cmd/altcode")
	build.Dir = ".."
	if err := build.Run(); err != nil {
		t.Fatalf("Build: %v", err)
	}

	start := time.Now()
	cmd := exec.Command("/tmp/altcode-bench", "--version")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)

	t.Logf("Startup time (--version): %v", elapsed)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Startup too slow: %v (target <100ms)", elapsed)
	}
}
