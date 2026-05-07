package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// PermissionDialog renders a modal asking the user to approve a tool call.
type PermissionDialog struct {
	theme    Theme
	width    int
	toolName string
	pattern  string
	visible  bool
}

// NewPermissionDialog creates a PermissionDialog with the given theme.
func NewPermissionDialog(theme Theme) *PermissionDialog {
	return &PermissionDialog{theme: theme}
}

func (d *PermissionDialog) Show(toolName, pattern string) {
	d.toolName = toolName
	d.pattern = pattern
	d.visible = true
}

func (d *PermissionDialog) Hide()            { d.visible = false }
func (d *PermissionDialog) IsVisible() bool  { return d.visible }
func (d *PermissionDialog) SetWidth(w int)   { d.width = w }

func (d *PermissionDialog) View() string {
	if !d.visible {
		return ""
	}

	// Inner content width = dialog width - border (2) - horizontal
	// padding (4 = 2 left + 2 right). Clamp to a sensible minimum
	// so very narrow terminals don't produce a degenerate frame.
	contentWidth := d.width - 6
	if contentWidth < 24 {
		contentWidth = 24
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(d.theme.Warning).
		Padding(1, 2).
		Width(contentWidth + 2) // +2 for inside padding

	title := lipgloss.NewStyle().
		Foreground(d.theme.Warning).
		Bold(true).
		Render("Permission Required")

	// Use lipgloss truncate so multi-byte tool names / patterns don't
	// blow past the inner width on narrow terminals (round-4 review).
	toolStyled := lipgloss.NewStyle().Foreground(d.theme.Primary).
		Render(truncateRunes(d.toolName, contentWidth-6))
	patternStyled := lipgloss.NewStyle().Foreground(d.theme.Muted).
		Render(truncateRunes(d.pattern, contentWidth-9))
	body := fmt.Sprintf("\nTool: %s\nPattern: %s\n", toolStyled, patternStyled)

	// Stack each option on its own line — earlier two-column layout
	// overflowed on terminals < 60 cols. One row each is wider but
	// always fits and matches CC's prompt style.
	muted := lipgloss.NewStyle().Foreground(d.theme.Muted)
	allowAllText := "!  allow all " + truncateRunes(d.toolName, contentWidth-13)
	options := "\n" + muted.Render(
		"  y   allow once\n"+
			"  a   always allow this pattern\n"+
			"  "+allowAllText+"\n"+
			"  n   deny once\n"+
			"  Esc deny",
	)

	return border.Render(title + body + options)
}
