package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPalette_SelectedEmpty returns ok=false when no items match.
func TestPalette_SelectedEmpty(t *testing.T) {
	p := NewPalette(DefaultTheme, nil)
	_, ok := p.Selected()
	if ok {
		t.Error("Selected on empty palette should return ok=false")
	}
}

// TestPalette_SelectedHappyPath returns the cursor item.
func TestPalette_SelectedHappyPath(t *testing.T) {
	cmds := []PaletteCommand{
		{Name: "/help", Description: "show help"},
		{Name: "/diff", Description: "show diff"},
	}
	p := NewPalette(DefaultTheme, cmds)
	got, ok := p.Selected()
	if !ok {
		t.Fatal("Selected at cursor 0 should be ok")
	}
	if got.Name != "/help" {
		t.Errorf("Selected = %q, want /help", got.Name)
	}
}

// TestPalette_DownArrowAdvancesCursor covers the Down branch in
// UpdateKey, including the offset-scroll when paste paletteMaxVisible.
func TestPalette_DownArrowAdvancesCursor(t *testing.T) {
	cmds := make([]PaletteCommand, 30)
	for i := range cmds {
		cmds[i] = PaletteCommand{Name: "/c" + string(rune('a'+i%26)), Description: "x"}
	}
	p := NewPalette(DefaultTheme, cmds)
	for i := 0; i < 5; i++ {
		p.UpdateKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if p.cursor != 5 {
		t.Errorf("cursor = %d, want 5 after 5 down-presses", p.cursor)
	}
}

// TestPalette_UpArrowClampedAtZero
func TestPalette_UpArrowClampedAtZero(t *testing.T) {
	cmds := []PaletteCommand{{Name: "/a"}, {Name: "/b"}}
	p := NewPalette(DefaultTheme, cmds)
	for i := 0; i < 5; i++ {
		p.UpdateKey(tea.KeyMsg{Type: tea.KeyUp})
	}
	if p.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped at top)", p.cursor)
	}
}

// TestPalette_DownClampedAtEnd
func TestPalette_DownClampedAtEnd(t *testing.T) {
	cmds := []PaletteCommand{{Name: "/a"}, {Name: "/b"}, {Name: "/c"}}
	p := NewPalette(DefaultTheme, cmds)
	for i := 0; i < 10; i++ {
		p.UpdateKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if p.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (clamped at end)", p.cursor)
	}
}

// TestPalette_EscHidesPalette via UpdateKey path.
func TestPalette_EscHidesPaletteViaUpdateKey(t *testing.T) {
	p := NewPalette(DefaultTheme, []PaletteCommand{{Name: "/a"}})
	p.Show()
	p.UpdateKey(tea.KeyMsg{Type: tea.KeyEsc})
	if p.IsVisible() {
		t.Error("UpdateKey(Esc) should hide the palette")
	}
}

// TestPalette_EnterSignalsCallerToReadSelected — UpdateKey(Enter) keeps
// the palette visible (caller decides).
func TestPalette_EnterSignalsCallerToReadSelected(t *testing.T) {
	p := NewPalette(DefaultTheme, []PaletteCommand{{Name: "/a"}})
	p.Show()
	p.UpdateKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !p.IsVisible() {
		t.Error("UpdateKey(Enter) shouldn't hide the palette — caller does that")
	}
}

// TestPalette_FilterMatchesNameAndDescription verifies fuzzy match by
// substring on either field.
func TestPalette_FilterMatchesNameAndDescription(t *testing.T) {
	cmds := []PaletteCommand{
		{Name: "/help", Description: "show help"},
		{Name: "/diff", Description: "show file changes"},
		{Name: "/version", Description: "print build info"},
	}
	p := NewPalette(DefaultTheme, cmds)
	p.filter("show")
	if len(p.filtered) != 2 {
		t.Errorf("filter 'show' got %d, want 2 (help+diff)", len(p.filtered))
	}
	p.filter("vers")
	if len(p.filtered) != 1 || p.filtered[0].Name != "/version" {
		t.Errorf("filter 'vers' = %v, want only /version", p.filtered)
	}
	p.filter("")
	if len(p.filtered) != 3 {
		t.Errorf("empty filter should restore all: got %d", len(p.filtered))
	}
}

// TestPalette_ViewHiddenIsEmpty covers the !visible early return.
func TestPalette_ViewHiddenIsEmpty(t *testing.T) {
	p := NewPalette(DefaultTheme, []PaletteCommand{{Name: "/a"}})
	p.SetWidth(80)
	if got := p.View(); got != "" {
		t.Errorf("hidden View = %q, want empty", got)
	}
}

// TestPalette_ViewVisibleRendersBox covers the happy path.
func TestPalette_ViewVisibleRendersBox(t *testing.T) {
	cmds := []PaletteCommand{
		{Name: "/help", Description: "show help"},
		{Name: "/diff", Description: "show diff"},
	}
	p := NewPalette(DefaultTheme, cmds)
	p.SetWidth(80)
	p.Show()
	out := stripANSI(p.View())
	for _, want := range []string{"/help", "/diff"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPalette_ViewRendersGroupLabelsWithoutSelectingThem(t *testing.T) {
	cmds := []PaletteCommand{
		{Name: "/help", Description: "show help", Group: "Chat"},
		{Name: "/workspace", Description: "start workspace", Group: "Workspace"},
	}
	p := NewPalette(DefaultTheme, cmds)
	p.SetWidth(80)
	p.Show()

	out := stripANSI(p.View())
	for _, want := range []string{"Chat", "Workspace", "/help", "/workspace"} {
		if !strings.Contains(out, want) {
			t.Fatalf("palette missing %q:\n%s", want, out)
		}
	}
	if got, ok := p.Selected(); !ok || got.Name != "/help" {
		t.Fatalf("selected command = (%q, %v), want /help true", got.Name, ok)
	}
	p.UpdateKey(tea.KeyMsg{Type: tea.KeyDown})
	if got, ok := p.Selected(); !ok || got.Name != "/workspace" {
		t.Fatalf("cursor should move command-to-command, got (%q, %v)", got.Name, ok)
	}
}

func TestPalette_FilterKeepsGroupLabelsAndCursorOnCommands(t *testing.T) {
	cmds := []PaletteCommand{
		{Name: "/help", Description: "show help", Group: "Chat"},
		{Name: "/model", Description: "show model", Group: "Config"},
		{Name: "/metadata", Description: "show or toggle message metadata", Group: "Config"},
		{Name: "/memory", Description: "manage memory", Group: "Config"},
	}
	p := NewPalette(DefaultTheme, cmds)
	p.SetWidth(80)
	p.Show()

	p.filter("met")
	out := stripANSI(p.View())
	if !strings.Contains(out, "Config") || strings.Contains(out, "Chat") {
		t.Fatalf("filtered palette should show only matching group labels:\n%s", out)
	}
	if got, ok := p.Selected(); !ok || got.Name != "/metadata" {
		t.Fatalf("filtered selected command = (%q, %v), want /metadata true", got.Name, ok)
	}

	p.UpdateKey(tea.KeyMsg{Type: tea.KeyDown})
	if got, ok := p.Selected(); !ok || got.Name != "/metadata" {
		t.Fatalf("cursor should stay on command rows after filtered down, got (%q, %v)", got.Name, ok)
	}
}

func TestPaletteBuiltins_HaveGroupsAndMetadata(t *testing.T) {
	cmds := buildPaletteCommands(nil)
	seen := map[string]PaletteCommand{}
	for _, cmd := range cmds {
		seen[cmd.Name] = cmd
		if cmd.Group == "" {
			t.Fatalf("palette command %s is missing a group", cmd.Name)
		}
	}
	if seen["/metadata"].Name == "" {
		t.Fatalf("palette missing /metadata")
	}
}

// TestPalette_ViewEmptyShowsNoMatches covers the no-matches branch.
func TestPalette_ViewEmptyShowsNoMatches(t *testing.T) {
	p := NewPalette(DefaultTheme, []PaletteCommand{{Name: "/a"}})
	p.SetWidth(80)
	p.Show()
	p.filter("__nothing_matches_xyz__")
	out := stripANSI(p.View())
	if !strings.Contains(out, "no matches") {
		t.Errorf("expected 'no matches' message:\n%s", out)
	}
}

// TestCollapseWhitespace_TrimsRunsAndNewlines covers all branches of
// collapseWhitespace: leading/trailing trim, internal collapse, mixed
// whitespace types.
func TestCollapseWhitespace_TrimsRunsAndNewlines(t *testing.T) {
	cases := map[string]string{
		"  hello\n\nworld  ":             "hello world",
		"a\tb\tc":                        "a b c",
		"  ":                             "",
		"\n\r\nfoo\n":                    "foo",
		"already clean":                  "already clean",
		"multiple   internal     spaces": "multiple internal spaces",
	}
	for in, want := range cases {
		if got := collapseWhitespace(in); got != want {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTruncateRunes_HandlesUnicodeAndEdgeCases covers the rune-safe
// truncate helper.
func TestTruncateRunes_HandlesUnicodeAndEdgeCases(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"}, // shorter than max
		{"hello", 5, "hello"},  // exactly max
		{"hello world", 6, "hello…"},
		{"中文测试", 3, "中文…"},
		{"x", 0, ""},  // max=0 → empty
		{"x", -1, ""}, // negative max
	}
	for _, c := range cases {
		if got := truncateRunes(c.in, c.max); got != c.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}
