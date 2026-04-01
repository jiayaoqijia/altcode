package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// Command is a named action shown in the command palette.
type Command struct {
	Name        string
	Description string
	Action      func() string
}

// Palette renders a fuzzy-filtered command picker.
type Palette struct {
	theme    Theme
	width    int
	visible  bool
	input    textinput.Model
	commands []Command
	filtered []Command
}

// NewPalette creates a Palette with the given commands.
func NewPalette(theme Theme, commands []Command) *Palette {
	ti := textinput.New()
	ti.Placeholder = "Type a command..."
	ti.CharLimit = 50

	return &Palette{
		theme:    theme,
		input:    ti,
		commands: commands,
		filtered: commands,
	}
}

func (p *Palette) Toggle() {
	p.visible = !p.visible
	if p.visible {
		p.input.Focus()
		p.input.Reset()
		p.filtered = p.commands
	}
}

func (p *Palette) IsVisible() bool { return p.visible }
func (p *Palette) SetWidth(w int)  { p.width = w }

func (p *Palette) Filter(query string) {
	if query == "" {
		p.filtered = p.commands
		return
	}
	q := strings.ToLower(query)
	var filtered []Command
	for _, cmd := range p.commands {
		if strings.Contains(strings.ToLower(cmd.Name), q) ||
			strings.Contains(strings.ToLower(cmd.Description), q) {
			filtered = append(filtered, cmd)
		}
	}
	p.filtered = filtered
}

func (p *Palette) View() string {
	if !p.visible {
		return ""
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.theme.Primary).
		Padding(0, 1).
		Width(p.width - 4)

	var sb strings.Builder
	sb.WriteString(p.input.View())
	sb.WriteByte('\n')

	for i, cmd := range p.filtered {
		if i >= 10 {
			break
		}
		name := lipgloss.NewStyle().Foreground(p.theme.Primary).Bold(true).Render(cmd.Name)
		desc := lipgloss.NewStyle().Foreground(p.theme.Muted).Render("  " + cmd.Description)
		sb.WriteString(name + desc + "\n")
	}

	return border.Render(sb.String())
}
