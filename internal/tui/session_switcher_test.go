package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSessionSwitcher_NewIsHidden covers the constructor.
func TestSessionSwitcher_NewIsHidden(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	if s.IsVisible() {
		t.Error("fresh switcher should not be visible")
	}
}

// TestSessionSwitcher_ShowResetsCursor covers Show resetting state.
func TestSessionSwitcher_ShowResetsCursor(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.cursor = 5
	s.Show()
	if !s.IsVisible() {
		t.Error("Show should make switcher visible")
	}
	if s.cursor != 0 {
		t.Errorf("Show should reset cursor to 0, got %d", s.cursor)
	}
}

// TestSessionSwitcher_HideClears
func TestSessionSwitcher_HideClears(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.Show()
	s.Hide()
	if s.IsVisible() {
		t.Error("Hide should make switcher invisible")
	}
}

// TestSessionSwitcher_LoadNilDBClears
func TestSessionSwitcher_LoadNilDBClears(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.entries = []SessionEntry{{ID: "stale"}}
	if err := s.Load(nil); err != nil {
		t.Errorf("Load(nil) error = %v, want nil", err)
	}
	if s.entries != nil {
		t.Errorf("Load(nil) should clear entries, got %v", s.entries)
	}
}

// TestSessionSwitcher_SelectedIDOutOfRangeReturnsEmpty
func TestSessionSwitcher_SelectedIDOutOfRangeReturnsEmpty(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	if got := s.SelectedID(); got != "" {
		t.Errorf("empty switcher SelectedID = %q, want empty", got)
	}
}

// TestSessionSwitcher_SelectedIDHappy
func TestSessionSwitcher_SelectedIDHappy(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.entries = []SessionEntry{
		{ID: "abc", Title: "first"},
		{ID: "def", Title: "second"},
	}
	s.filtered = s.entries
	s.cursor = 1
	if got := s.SelectedID(); got != "def" {
		t.Errorf("SelectedID = %q, want def", got)
	}
}

// TestSessionSwitcher_DownAdvancesCursor under the visible cap.
func TestSessionSwitcher_DownAdvancesCursor(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.entries = make([]SessionEntry, 5)
	for i := range s.entries {
		s.entries[i] = SessionEntry{ID: string(rune('a' + i))}
	}
	s.filtered = s.entries
	for i := 0; i < 3; i++ {
		s.UpdateKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if s.cursor != 3 {
		t.Errorf("cursor = %d, want 3 after 3 down-presses", s.cursor)
	}
}

// TestSessionSwitcher_DownClampedAtVisibleCap
func TestSessionSwitcher_DownClampedAtVisibleCap(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	// 20 entries; visible cap is 10.
	s.entries = make([]SessionEntry, 20)
	for i := range s.entries {
		s.entries[i] = SessionEntry{ID: string(rune('a' + i%26))}
	}
	s.filtered = s.entries
	for i := 0; i < 30; i++ {
		s.UpdateKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if s.cursor != switcherMaxVisible-1 {
		t.Errorf("cursor = %d, want %d (max-1)", s.cursor, switcherMaxVisible-1)
	}
}

// TestSessionSwitcher_UpClampedAtZero
func TestSessionSwitcher_UpClampedAtZero(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.entries = []SessionEntry{{ID: "a"}, {ID: "b"}}
	s.filtered = s.entries
	for i := 0; i < 5; i++ {
		s.UpdateKey(tea.KeyMsg{Type: tea.KeyUp})
	}
	if s.cursor != 0 {
		t.Errorf("cursor = %d, want 0", s.cursor)
	}
}

// TestSessionSwitcher_EscHides
func TestSessionSwitcher_EscHides(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.Show()
	s.UpdateKey(tea.KeyMsg{Type: tea.KeyEsc})
	if s.IsVisible() {
		t.Error("Esc should hide the switcher")
	}
}

// TestSessionSwitcher_FilterMatchesTitleAndID
func TestSessionSwitcher_FilterMatchesTitleAndID(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.entries = []SessionEntry{
		{ID: "abc123", Title: "fix login bug", Model: "claude"},
		{ID: "def456", Title: "add dashboard", Model: "codex"},
		{ID: "ghi789", Title: "update README", Model: "claude"},
	}
	s.filtered = s.entries

	s.filter("dashboard")
	if len(s.filtered) != 1 || s.filtered[0].ID != "def456" {
		t.Errorf("filter 'dashboard' = %+v, want def456 only", s.filtered)
	}

	s.filter("CLAUDE") // case-insensitive
	if len(s.filtered) != 2 {
		t.Errorf("filter 'CLAUDE' = %d, want 2", len(s.filtered))
	}

	s.filter("ghi") // ID match
	if len(s.filtered) != 1 || s.filtered[0].ID != "ghi789" {
		t.Errorf("filter 'ghi' = %+v, want ghi789", s.filtered)
	}

	s.filter("")
	if len(s.filtered) != 3 {
		t.Errorf("empty filter should restore all: got %d", len(s.filtered))
	}
}

// TestSessionSwitcher_ViewHiddenIsEmpty
func TestSessionSwitcher_ViewHiddenIsEmpty(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.SetWidth(80)
	if got := s.View(); got != "" {
		t.Errorf("hidden View = %q, want empty", got)
	}
}

// TestSessionSwitcher_ViewVisibleShowsRows
func TestSessionSwitcher_ViewVisibleShowsRows(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.SetWidth(120)
	s.entries = []SessionEntry{
		{ID: "abc12345", Title: "fix login", Model: "claude", Date: "2026-05-01 10:00"},
	}
	s.filtered = s.entries
	s.Show()
	out := stripANSI(s.View())
	for _, want := range []string{"fix login", "claude", "abc12345"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestSessionSwitcher_ViewEmptyShowsPlaceholder
func TestSessionSwitcher_ViewEmptyShowsPlaceholder(t *testing.T) {
	s := NewSessionSwitcher(DefaultTheme)
	s.SetWidth(80)
	s.Show()
	out := stripANSI(s.View())
	if !strings.Contains(out, "No sessions") {
		t.Errorf("expected placeholder:\n%s", out)
	}
}

// TestTruncateStr_RuneSafe verifies the rune+ANSI-safe truncate helper.
func TestTruncateStr_RuneSafe(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},   // shorter than max
		{"hello world", 6, "hello~"},
		// CJK: lipgloss.Width counts each CJK char as 2 columns. With n=6,
		// budget is n-1=5 cols; "中文" = 4 cols fits but "中文测" = 6 doesn't.
		{"中文测试abcdef", 6, "中文~"},
		{"x", 0, "~"},            // n=0 → "~"
	}
	for _, c := range cases {
		if got := truncateStr(c.in, c.n); got != c.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
