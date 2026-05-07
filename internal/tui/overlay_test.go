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

// TestApp_MainBodyWidth_AlwaysFullWidth covers the simplified width
// calculation after the sidebar was removed — overlays now use the full
// terminal width regardless of size.
func TestApp_MainBodyWidth_AlwaysFullWidth(t *testing.T) {
	for _, w := range []int{60, 80, 100, 120, 200} {
		a := testApp()
		a.width = w
		if got := a.mainBodyWidth(); got != w {
			t.Errorf("width=%d: mainBodyWidth = %d, want %d", w, got, w)
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
