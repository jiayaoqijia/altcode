package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// permOption is one row in the permission dialog. The dialog
// presents these as a vertical list; the user navigates with
// up/down arrows and activates with Enter.
type permOption struct {
	label     string // human label, e.g. "Allow once"
	hint      string // dim suffix, e.g. "(this turn only)"
	shortcut  string // single-char power-user shortcut: y/a/!/n
	allow     bool   // Allow vs Deny
	persist   bool   // Allow + persist for the session
}

// PermissionDialog renders a modal asking the user to approve a
// tool call. Navigable with arrow keys (CC-parity) and confirms
// with Enter; the original y/a/!/n shortcuts still work for users
// who know them.
type PermissionDialog struct {
	theme    Theme
	width    int
	toolName string
	pattern  string
	visible  bool
	options  []permOption
	selected int // index into options, 0 = first row (default focus)
}

// NewPermissionDialog creates a PermissionDialog with the given theme.
// The 4 options are fixed: allow once, allow this pattern, allow tool,
// deny. The order matches what CC shows so muscle memory carries over.
func NewPermissionDialog(theme Theme) *PermissionDialog {
	return &PermissionDialog{
		theme:   theme,
		options: defaultPermOptions(),
	}
}

func defaultPermOptions() []permOption {
	return []permOption{
		{label: "Allow once", hint: "this turn only", shortcut: "y", allow: true, persist: false},
		{label: "Allow always for this pattern", hint: "session-wide", shortcut: "a", allow: true, persist: true},
		{label: "Allow all calls to this tool", hint: "session-wide", shortcut: "!", allow: true, persist: true},
		{label: "Deny", hint: "this turn only", shortcut: "n", allow: false, persist: false},
	}
}

// Show resets the dialog to a fresh state — selected back to 0
// (first option) so each prompt starts with the focus on Allow once,
// not wherever the user left it on the previous prompt.
func (d *PermissionDialog) Show(toolName, pattern string) {
	d.toolName = toolName
	d.pattern = pattern
	d.visible = true
	d.selected = 0
}

func (d *PermissionDialog) Hide()           { d.visible = false }
func (d *PermissionDialog) IsVisible() bool { return d.visible }
func (d *PermissionDialog) SetWidth(w int)  { d.width = w }

// MoveUp / MoveDown clamp at the list ends — wrapping confused users
// in early testing because the focus jumped from the last option
// (Deny) back to the first (Allow once) on a stray keypress.
func (d *PermissionDialog) MoveUp() {
	if d.selected > 0 {
		d.selected--
	}
}

func (d *PermissionDialog) MoveDown() {
	if d.selected < len(d.options)-1 {
		d.selected++
	}
}

// Selected returns the option the user is currently focused on, used
// by handlePermDialogKey when Enter or Space is pressed.
func (d *PermissionDialog) Selected() permOption {
	if d.selected < 0 || d.selected >= len(d.options) {
		return d.options[0]
	}
	return d.options[d.selected]
}

// SelectByShortcut moves focus to whichever option matches the given
// single-char shortcut (y/a/!/n). Returns true if a match was found.
// Used so power-user shortcuts both move the highlight AND fire the
// action, instead of bypassing the visible UI.
func (d *PermissionDialog) SelectByShortcut(s string) bool {
	for i, opt := range d.options {
		if opt.shortcut == s {
			d.selected = i
			return true
		}
	}
	return false
}

func (d *PermissionDialog) View() string {
	if !d.visible {
		return ""
	}

	contentWidth := d.width - 6
	if contentWidth < 28 {
		contentWidth = 28
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(d.theme.Warning).
		Padding(1, 2).
		Width(contentWidth + 2)

	title := lipgloss.NewStyle().
		Foreground(d.theme.Warning).
		Bold(true).
		Render("Permission Required")

	toolStyled := lipgloss.NewStyle().Foreground(d.theme.Primary).
		Render(truncateRunes(d.toolName, contentWidth-6))
	patternStyled := lipgloss.NewStyle().Foreground(d.theme.Muted).
		Render(truncateRunes(d.pattern, contentWidth-9))
	body := fmt.Sprintf("\nTool: %s\nPattern: %s\n", toolStyled, patternStyled)

	// Render each option on its own line. Selected row gets the ❯
	// caret + Primary color; others get a leading space + Muted so
	// the indentation never shifts when focus moves (off-by-one
	// shifts in the menu were a real eye-strain issue early on).
	var optsRender string
	muted := lipgloss.NewStyle().Foreground(d.theme.Muted)
	selected := lipgloss.NewStyle().Foreground(d.theme.Primary).Bold(true)
	dim := lipgloss.NewStyle().Foreground(d.theme.Muted).Italic(true)

	for i, opt := range d.options {
		// Tool name interpolation for the "allow all calls to <Tool>"
		// row so the user sees the actual tool name, not a generic
		// "this tool". Trimmed to the inner width budget.
		label := opt.label
		if i == 2 { // "Allow all calls to this tool"
			tn := truncateRunes(d.toolName, contentWidth-30)
			if tn != "" {
				label = "Allow all " + tn + " calls"
			}
		}

		hint := ""
		if opt.hint != "" {
			hint = "  " + dim.Render("("+opt.hint+")")
		}

		shortcutTag := ""
		if opt.shortcut != "" {
			shortcutTag = "  " + muted.Render("["+opt.shortcut+"]")
		}

		row := label + hint + shortcutTag
		if i == d.selected {
			optsRender += " " + selected.Render("❯ "+row) + "\n"
		} else {
			optsRender += "   " + muted.Render(row) + "\n"
		}
	}

	footer := dim.Render("↑↓ navigate · Enter select · Esc deny")

	return border.Render(title + body + "\n" + optsRender + "\n" + footer)
}
