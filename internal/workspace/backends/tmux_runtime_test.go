package backends

import (
	"fmt"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/workspace"
)

func TestTmuxRuntime_Name(t *testing.T) {
	rt := NewTmuxRuntime("test-session")
	if rt.Name() != "tmux" {
		t.Errorf("Name() = %q, want %q", rt.Name(), "tmux")
	}
}

func TestTmuxRuntime_HandleFormat(t *testing.T) {
	rt := NewTmuxRuntime("ws-abc123")

	// Simulate what Spawn does internally to verify the format.
	rt.mu.Lock()
	idx := rt.counter
	rt.counter++
	handleID := fmt.Sprintf("tmux:%s:%d", rt.session, idx)
	rt.panes[handleID] = "%42"
	rt.mu.Unlock()

	if !strings.HasPrefix(handleID, "tmux:ws-abc123:") {
		t.Errorf(
			"handle %q missing expected prefix", handleID)
	}
	parts := strings.SplitN(handleID, ":", 3)
	if len(parts) != 3 {
		t.Fatalf(
			"expected 3 colon-separated parts, got %d: %q",
			len(parts), handleID)
	}
	if parts[0] != "tmux" {
		t.Errorf("prefix = %q, want %q", parts[0], "tmux")
	}
	if parts[1] != "ws-abc123" {
		t.Errorf("session = %q, want %q", parts[1], "ws-abc123")
	}
	if parts[2] != "0" {
		t.Errorf("index = %q, want %q", parts[2], "0")
	}
}

func TestTmuxRuntime_KillUnknownHandle(t *testing.T) {
	rt := NewTmuxRuntime("test-session")
	h := workspace.RuntimeHandle{ID: "tmux:test-session:999"}
	err := rt.Kill(h)
	if err != nil {
		t.Errorf("Kill unknown handle: %v", err)
	}
}

func TestTmuxRuntime_IsRunningUnknownHandle(t *testing.T) {
	rt := NewTmuxRuntime("test-session")
	h := workspace.RuntimeHandle{ID: "tmux:test-session:999"}
	alive, err := rt.IsRunning(h)
	if err != nil {
		t.Errorf("IsRunning error: %v", err)
	}
	if alive {
		t.Error("expected false for unknown handle")
	}
}
