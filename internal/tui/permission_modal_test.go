package tui

import (
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/event"
)

// TestOnPermissionRequest_DefaultShowsDialog verifies the modal is
// the DEFAULT flow (round-4 CC fix). The response channel must not
// auto-write; the dialog becomes visible; the pending request is
// stashed for the key handler.
func TestOnPermissionRequest_DefaultShowsDialog(t *testing.T) {
	// Explicitly clear any opt-out env var inherited from the user
	// environment. We rely on default behaviour (no env vars set)
	// to surface the modal.
	t.Setenv("ALTCODE_AUTO_APPROVE", "")
	a := testApp()

	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Pattern:  "bash:rm -rf /",
			Response: respCh,
		},
	}
	a.handleEvent(ev)

	if a.permDialog == nil || !a.permDialog.IsVisible() {
		t.Error("modal should be visible by default (CC parity)")
	}
	if a.pendingPermission == nil {
		t.Error("pendingPermission should hold the request channel")
	}
	// Response channel must NOT have auto-fired.
	select {
	case resp := <-respCh:
		t.Errorf("modal flow shouldn't auto-write response, got %+v", resp)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

// TestHandlePermDialogKey_AllowOnce sends 'y' and verifies an Allow
// (non-persistent) response lands on the channel + the modal closes.
func TestHandlePermDialogKey_AllowOnce(t *testing.T) {
	a := testApp()
	respCh := make(chan event.PermResponse, 1)
	a.permDialog.Show("write", "write:/tmp/x")
	a.pendingPermission = &event.PermReq{ToolName: "write", Response: respCh}

	handled, _ := a.handlePermDialogKey("y")
	if !handled {
		t.Fatal("'y' should be handled by the modal key router")
	}
	resp := <-respCh
	if resp.Action != event.Allow {
		t.Errorf("action = %v, want Allow", resp.Action)
	}
	if resp.Persistent {
		t.Error("'y' should be one-shot allow, not persistent")
	}
	if a.permDialog.IsVisible() {
		t.Error("modal should close after answer")
	}
	if a.pendingPermission != nil {
		t.Error("pendingPermission should be nil after answer")
	}
}

func TestHandlePermDialogKey_AlwaysAllow(t *testing.T) {
	a := testApp()
	respCh := make(chan event.PermResponse, 1)
	a.permDialog.Show("write", "write:/tmp/x")
	a.pendingPermission = &event.PermReq{ToolName: "write", Response: respCh}

	a.handlePermDialogKey("a")
	resp := <-respCh
	if !resp.Persistent {
		t.Errorf("'a' should be persistent allow, got Persistent=%v", resp.Persistent)
	}
}

func TestHandlePermDialogKey_DenyOnEsc(t *testing.T) {
	a := testApp()
	respCh := make(chan event.PermResponse, 1)
	a.permDialog.Show("bash", "bash:rm")
	a.pendingPermission = &event.PermReq{ToolName: "bash", Response: respCh}

	a.handlePermDialogKey("esc")
	resp := <-respCh
	if resp.Action != event.Deny {
		t.Errorf("esc should deny, got %v", resp.Action)
	}
}

func TestHandlePermDialogKey_DeniesOnN(t *testing.T) {
	a := testApp()
	respCh := make(chan event.PermResponse, 1)
	a.permDialog.Show("bash", "bash:rm")
	a.pendingPermission = &event.PermReq{ToolName: "bash", Response: respCh}

	a.handlePermDialogKey("n")
	resp := <-respCh
	if resp.Action != event.Deny {
		t.Errorf("'n' should deny, got %v", resp.Action)
	}
}

func TestHandlePermDialogKey_NoOpWhenHidden(t *testing.T) {
	a := testApp()
	a.permDialog.Hide()
	a.pendingPermission = nil

	handled, _ := a.handlePermDialogKey("y")
	if handled {
		t.Error("hidden modal should not handle keys")
	}
}

func TestHandlePermDialogKey_UnknownKeyFallsThrough(t *testing.T) {
	a := testApp()
	respCh := make(chan event.PermResponse, 1)
	a.permDialog.Show("bash", "bash:ls")
	a.pendingPermission = &event.PermReq{ToolName: "bash", Response: respCh}

	handled, _ := a.handlePermDialogKey("z")
	if handled {
		t.Error("unknown key should not be claimed by the modal")
	}
	// Response channel still empty.
	select {
	case <-respCh:
		t.Error("unknown key should not write a response")
	default:
		// expected
	}
}

// TestOnPermissionRequest_AutoApproveSkipsDialog verifies the explicit
// opt-out: ALTCODE_AUTO_APPROVE=1 → silent auto-allow + info note,
// no modal. Round-4 default-flip preserves YOLO mode for users who
// want it.
func TestOnPermissionRequest_AutoApproveSkipsDialog(t *testing.T) {
	t.Setenv("ALTCODE_AUTO_APPROVE", "1")
	a := testApp()
	respCh := make(chan event.PermResponse, 1)
	ev := event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Pattern:  "bash:ls",
			Response: respCh,
		},
	}
	a.handleEvent(ev)

	if a.permDialog != nil && a.permDialog.IsVisible() {
		t.Error("ALTCODE_AUTO_APPROVE=1 should suppress the modal")
	}
	select {
	case resp := <-respCh:
		if resp.Action != event.Allow {
			t.Errorf("auto-approve should send Allow, got %v", resp.Action)
		}
	case <-time.After(50 * time.Millisecond):
		t.Error("auto-approve should write response immediately")
	}
}
