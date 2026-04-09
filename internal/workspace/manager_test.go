package workspace

import (
	"context"
	"strings"
	"testing"
)

func TestBuildManagerPrompt(t *testing.T) {
	outputs := map[string]string{
		"architect":   "designed auth module",
		"implementer": "wrote auth.go with JWT",
	}
	ctx := context.Background()
	prompt := BuildManagerPrompt(
		ctx, "add JWT auth", outputs, nil)

	if !strings.Contains(prompt, "add JWT auth") {
		t.Error("prompt missing original task")
	}
	if !strings.Contains(prompt, "=== architect ===") {
		t.Error("prompt missing architect section")
	}
	if !strings.Contains(prompt, "=== implementer ===") {
		t.Error("prompt missing implementer section")
	}
	if !strings.Contains(prompt, "designed auth module") {
		t.Error("prompt missing architect output")
	}
	if !strings.Contains(prompt, "Merge all changes") {
		t.Error("prompt missing merge instruction")
	}
}

func TestTruncateWorkerOutput(t *testing.T) {
	short := "hello world"
	if got := TruncateWorkerOutput(short); got != short {
		t.Errorf("short string changed: %q", got)
	}

	long := strings.Repeat("x", 10*1024)
	got := TruncateWorkerOutput(long)
	if len(got) > maxWorkerOutputBytes+20 {
		t.Errorf(
			"truncated output too long: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "[...truncated]") {
		t.Error("truncated output missing suffix")
	}
}
