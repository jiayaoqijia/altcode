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

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(d.theme.Warning).
		Padding(1, 2).
		Width(d.width - 4)

	title := lipgloss.NewStyle().
		Foreground(d.theme.Warning).
		Bold(true).
		Render("Permission Required")

	body := fmt.Sprintf("\nTool: %s\nPattern: %s\n",
		lipgloss.NewStyle().Foreground(d.theme.Primary).Render(d.toolName),
		lipgloss.NewStyle().Foreground(d.theme.Muted).Render(d.pattern),
	)

	options := lipgloss.NewStyle().Foreground(d.theme.Muted).Render(
		"\n  y  allow once       n  deny once\n" +
			"  a  always allow     !  allow all " + d.toolName,
	)

	return border.Render(title + body + options)
}
