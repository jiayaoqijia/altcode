package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/event"
)

// TestOnPermissionRequest_AutoAllowsAndSurfacesInfo is the regression
// test for the 1-hour TUI hang. Before the fix, the engine's
// askPermission emitted a PermissionRequest event whose Response
// channel was never written by the TUI — every ActionAsk tool call
// deadlocked the agent loop forever.
func TestOnPermissionRequest_AutoAllowsAndSurfacesInfo(t *testing.T) {
	a := testApp()

	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "write",
			Pattern:  "write:/tmp/foo",
			Response: respCh,
		},
	}

	a.handleEvent(ev)

	// Response must arrive within a small window — the handler is
	// non-blocking, so this should be instant.
	select {
	case resp := <-respCh:
		if resp.Action != event.Allow {
			t.Errorf("got action %v, want Allow", resp.Action)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("permission response never arrived — handler still deadlocks")
	}

	// User-facing notice must mention the tool name so users notice
	// the auto-allow happened.
	if len(a.messages) == 0 {
		t.Fatal("no info message appended")
	}
	last := a.messages[len(a.messages)-1]
	if last.role != roleInfo {
		t.Errorf("notice role = %v, want roleInfo", last.role)
	}
	if !strings.Contains(last.content, "auto-allow") || !strings.Contains(last.content, "write") {
		t.Errorf("notice missing tool/auto-allow markers: %q", last.content)
	}
}

// TestOnPermissionRequest_NilPermissionFieldNoOps guards against a
// malformed event: the engine should never send PermissionRequest with
// a nil Permission, but if it does the handler must not panic.
func TestOnPermissionRequest_NilPermissionFieldNoOps(t *testing.T) {
	a := testApp()
	ev := event.Event{Type: event.PermissionRequest, Permission: nil}
	// Should not panic.
	a.handleEvent(ev)
	if len(a.messages) != 0 {
		t.Errorf("nil permission produced spurious info: %+v", a.messages)
	}
}

// TestOnPermissionRequest_NilResponseChannelNoOps — same defensive
// guard for a nil response channel.
func TestOnPermissionRequest_NilResponseChannelNoOps(t *testing.T) {
	a := testApp()
	ev := event.Event{
		Type:       event.PermissionRequest,
		Permission: &event.PermReq{ToolName: "write", Response: nil},
	}
	a.handleEvent(ev)
	if len(a.messages) != 0 {
		t.Errorf("nil response channel produced spurious info: %+v", a.messages)
	}
}
