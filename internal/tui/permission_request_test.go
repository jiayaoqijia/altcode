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

// TestOnPermissionRequest_InfoEmittedOncePerTool guards against the
// info-message spam regression: a 6-bash-call turn was producing 6
// identical "[auto-allow] bash" lines that drowned the actual tool
// tree. The handler now tracks per-tool-name and surfaces ONCE.
func TestOnPermissionRequest_InfoEmittedOncePerTool(t *testing.T) {
	a := testApp()

	for i := 0; i < 5; i++ {
		respCh := make(chan event.PermResponse, 1)
		a.handleEvent(event.Event{
			Type: event.PermissionRequest,
			Permission: &event.PermReq{
				ToolName: "bash",
				Pattern:  "bash:ls",
				Response: respCh,
			},
		})
		// Drain the response so each loop matches a real tool dispatch.
		<-respCh
	}

	infoCount := 0
	for _, m := range a.messages {
		if m.role == roleInfo && strings.Contains(m.content, "auto-allow") &&
			strings.Contains(m.content, "bash") {
			infoCount++
		}
	}
	if infoCount != 1 {
		t.Errorf("got %d auto-allow info messages, want 1 (per-tool dedup)", infoCount)
	}

	// A different tool name must still surface its own first-time note.
	respCh := make(chan event.PermResponse, 1)
	a.handleEvent(event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "write",
			Pattern:  "write:/tmp/x",
			Response: respCh,
		},
	})
	<-respCh

	writeNotices := 0
	for _, m := range a.messages {
		if m.role == roleInfo && strings.Contains(m.content, "auto-allow") &&
			strings.Contains(m.content, "write") {
			writeNotices++
		}
	}
	if writeNotices != 1 {
		t.Errorf("got %d write notices, want 1", writeNotices)
	}
}
