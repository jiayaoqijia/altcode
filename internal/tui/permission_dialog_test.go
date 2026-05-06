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
// the rendered dialog must include the title, tool name, pattern, and
// the four action labels.
func TestPermissionDialog_ViewVisibleShowsToolPattern(t *testing.T) {
	d := NewPermissionDialog(DefaultTheme)
	d.SetWidth(100)
	d.Show("write", "/etc/passwd")

	out := stripANSI(d.View())
	for _, want := range []string{
		"Permission Required",
		"write", "/etc/passwd",
		"allow once", "deny once",
		"always allow",
		"allow all write",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
