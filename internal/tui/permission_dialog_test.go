package tui

import (
	"strings"
	"testing"
)

// TestPermissionDialog_NewHidden — fresh dialog is invisible.
func TestPermissionDialog_NewHidden(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	if d.IsVisible() {
		t.Error("fresh dialog should not be visible")
	}
}

// TestPermissionDialog_ShowSetsFields covers Show + IsVisible together.
func TestPermissionDialog_ShowSetsFields(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.Show("bash", "git push *")
	if !d.IsVisible() {
		t.Error("Show should make dialog visible")
	}
	if d.toolName != "bash" || d.pattern != "git push *" {
		t.Errorf("fields = (%q, %q)", d.toolName, d.pattern)
	}
}

// TestPermissionDialog_HideClears
func TestPermissionDialog_HideClears(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.Show("bash", "rm")
	d.Hide()
	if d.IsVisible() {
		t.Error("Hide should make dialog invisible")
	}
}

// TestPermissionDialog_SetWidth
func TestPermissionDialog_SetWidth(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.SetWidth(80)
	if d.width != 80 {
		t.Errorf("width = %d, want 80", d.width)
	}
}

// TestPermissionDialog_ViewHiddenIsEmpty covers the !visible branch.
func TestPermissionDialog_ViewHiddenIsEmpty(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.SetWidth(80)
	if got := d.View(); got != "" {
		t.Errorf("hidden View = %q, want empty", got)
	}
}

// TestPermissionDialog_ViewVisibleShowsToolPattern covers happy path —
// the rendered dialog must include the title, tool name, pattern, all
// four navigable labels (CC-parity arrow-key UI), and the footer hint.
func TestPermissionDialog_ViewVisibleShowsToolPattern(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.SetWidth(100)
	d.Show("write", "/etc/passwd")

	out := stripANSI(d.View())
	for _, want := range []string{
		"Permission Required",
		"write", "/etc/passwd",
		"Allow once",
		"Allow always for this pattern",
		"Allow all write calls", // tool-name interpolation in row 3
		"Deny",
		"↑↓ navigate",
		"Enter select",
		"Esc deny",
		"❯ ", // selected-row caret on the default option (Allow once)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestPermissionDialog_NavigationMovesHighlight covers up/down arrow
// navigation and the clamping behaviour at the list ends.
func TestPermissionDialog_NavigationMovesHighlight(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.Show("Bash", "ls -la")

	if d.selected != 0 {
		t.Fatalf("Show should reset selected to 0, got %d", d.selected)
	}
	d.MoveDown()
	if d.selected != 1 {
		t.Errorf("after one MoveDown, selected = %d, want 1", d.selected)
	}
	d.MoveDown()
	d.MoveDown()
	if d.selected != 3 {
		t.Errorf("after three MoveDowns, selected = %d, want 3", d.selected)
	}
	// Already at the last option — additional MoveDown should clamp.
	d.MoveDown()
	if d.selected != 3 {
		t.Errorf("MoveDown past last clamps; selected = %d, want 3", d.selected)
	}
	d.MoveUp()
	if d.selected != 2 {
		t.Errorf("after MoveUp from end, selected = %d, want 2", d.selected)
	}
	// All the way back to the start.
	d.MoveUp()
	d.MoveUp()
	d.MoveUp() // clamp at 0
	if d.selected != 0 {
		t.Errorf("MoveUp past first clamps; selected = %d, want 0", d.selected)
	}
}

// TestPermissionDialog_SelectByShortcut covers the power-user path —
// pressing a single-char shortcut must move the highlight to that
// option (so the visible state stays honest with what fires).
func TestPermissionDialog_SelectByShortcut(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.Show("Bash", "ls")

	cases := map[string]int{
		"y": 0, // Allow once
		"a": 1, // Allow always for this pattern
		"!": 2, // Allow all calls to this tool
		"n": 3, // Deny
	}
	for shortcut, wantIdx := range cases {
		ok := d.SelectByShortcut(shortcut)
		if !ok {
			t.Errorf("SelectByShortcut(%q) returned false; want true", shortcut)
		}
		if d.selected != wantIdx {
			t.Errorf("SelectByShortcut(%q) → selected=%d, want %d",
				shortcut, d.selected, wantIdx)
		}
	}

	if d.SelectByShortcut("z") {
		t.Error("SelectByShortcut(\"z\") returned true; want false for unknown shortcut")
	}
}
