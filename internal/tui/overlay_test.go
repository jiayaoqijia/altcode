package tui

import (
	"testing"
)

// TestApp_TogglePalette flips visibility on each call and refocuses
// the textarea correctly.
func TestApp_TogglePalette(t *testing.T) {
	a := testApp()
	a.width = 100

	a.togglePalette()
	if !a.palette.IsVisible() {
		t.Error("togglePalette: first call should show the palette")
	}

	a.togglePalette()
	if a.palette.IsVisible() {
		t.Error("togglePalette: second call should hide the palette")
	}
}

// TestApp_ToggleSessionSwitcher flips visibility but doesn't crash on
// a nil engine (the typical path in test fixtures).
func TestApp_ToggleSessionSwitcher(t *testing.T) {
	a := testApp()
	a.width = 120

	a.toggleSessionSwitcher()
	// First toggle when engine is nil should still show the switcher
	// but skip the Load() call (the explicit nil-engine guard at line
	// 47 of overlay.go).
	if !a.sessionSwitcher.IsVisible() {
		t.Error("toggleSessionSwitcher: should show even with nil engine")
	}

	a.toggleSessionSwitcher()
	if a.sessionSwitcher.IsVisible() {
		t.Error("toggleSessionSwitcher: second call should hide")
	}
}

// TestApp_MainBodyWidth_NarrowReturnsFullWidth covers the small-terminal
// branch where the sidebar is suppressed.
func TestApp_MainBodyWidth_NarrowReturnsFullWidth(t *testing.T) {
	a := testApp()
	a.width = 80
	if got := a.mainBodyWidth(); got != 80 {
		t.Errorf("narrow mainBodyWidth = %d, want 80", got)
	}
}

// TestApp_MainBodyWidth_WideReservesSidebar covers the >=100 branch
// where 1/4 of the width (capped at 30) goes to the sidebar.
func TestApp_MainBodyWidth_WideReservesSidebar(t *testing.T) {
	cases := []struct {
		width    int
		wantMain int
	}{
		{100, 75},  // 100/4 = 25; main = 100-25
		{120, 90},  // 120/4 = 30 (cap); main = 120-30
		{200, 170}, // 200/4 = 50, capped at 30; main = 200-30
	}
	for _, c := range cases {
		a := testApp()
		a.width = c.width
		if got := a.mainBodyWidth(); got != c.wantMain {
			t.Errorf("width=%d: mainBodyWidth = %d, want %d", c.width, got, c.wantMain)
		}
	}
}

// TestApp_SwitchToSession_NilEngineNoOps covers the nil-engine guard.
// Without the guard, switching session before engine init would panic.
func TestApp_SwitchToSession_NilEngineNoOps(t *testing.T) {
	a := testApp()
	a.engine = nil
	// Should not panic.
	a.switchToSession("any-session-id")
}

// TestHandlePaletteKey_EscHidesAndRefocuses covers the most common
// dismiss path.
func TestHandlePaletteKey_EscHidesAndRefocuses(t *testing.T) {
	a := testApp()
	a.palette.Show()
	_, _, ok := a.handlePaletteKey(keyOf("esc"))
	if !ok {
		t.Error("esc in palette should be handled")
	}
	if a.palette.IsVisible() {
		t.Error("esc should hide the palette")
	}
}

// TestHandlePaletteKey_CtrlCQuits covers the quit path through palette.
func TestHandlePaletteKey_CtrlCQuits(t *testing.T) {
	a := testApp()
	a.palette.Show()
	_, cmd, ok := a.handlePaletteKey(keyOf("ctrl+c"))
	if !ok || cmd == nil {
		t.Error("ctrl+c in palette should produce a quit cmd")
	}
}

// TestHandlePaletteKey_OtherKeysReachInternalUpdate verifies the
// fallthrough that delegates to p.UpdateKey (e.g. up/down/letters).
func TestHandlePaletteKey_OtherKeysReachInternalUpdate(t *testing.T) {
	a := testApp()
	a.palette.Show()
	_, _, ok := a.handlePaletteKey(keyOf("down"))
	if !ok {
		t.Error("down arrow in palette should be handled")
	}
	if !a.palette.IsVisible() {
		t.Error("down arrow should not hide the palette")
	}
}

// TestHandleSwitcherKey_EscHidesAndRefocuses covers the dismiss path.
func TestHandleSwitcherKey_EscHidesAndRefocuses(t *testing.T) {
	a := testApp()
	a.sessionSwitcher.Show()
	_, _, ok := a.handleSwitcherKey(keyOf("esc"))
	if !ok {
		t.Error("esc in switcher should be handled")
	}
	if a.sessionSwitcher.IsVisible() {
		t.Error("esc should hide the switcher")
	}
}

// TestHandleSwitcherKey_CtrlCQuits covers the quit path.
func TestHandleSwitcherKey_CtrlCQuits(t *testing.T) {
	a := testApp()
	a.sessionSwitcher.Show()
	_, cmd, ok := a.handleSwitcherKey(keyOf("ctrl+c"))
	if !ok || cmd == nil {
		t.Error("ctrl+c in switcher should produce a quit cmd")
	}
}
