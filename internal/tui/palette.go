package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const paletteMaxVisible = 10

// PaletteCommand is a named action shown in the command palette.
type PaletteCommand struct {
	Name        string
	Description string
	Action      func() string
}

// Palette renders a fuzzy-filtered command picker.
type Palette struct {
	theme    Theme
	width    int
	visible  bool
	cursor   int
	input    textinput.Model
	commands []PaletteCommand
	filtered []PaletteCommand
}

// NewPalette creates a Palette with the given commands.
func NewPalette(theme Theme, cmds []PaletteCommand) *Palette {
	ti := textinput.New()
	ti.Placeholder = "Type a command..."
	ti.CharLimit = 50

	return &Palette{
		theme:    theme,
		input:    ti,
		commands: cmds,
		filtered: cmds,
	}
}

// Show opens the palette and resets state.
func (p *Palette) Show() {
	p.visible = true
	p.cursor = 0
	p.input.Focus()
	p.input.Reset()
	p.filtered = p.commands
}

// Hide closes the palette and blurs the input.
func (p *Palette) Hide() {
	p.visible = false
	p.input.Blur()
}

// IsVisible reports whether the palette is open.
func (p *Palette) IsVisible() bool { return p.visible }

// SetWidth sets the palette render width.
func (p *Palette) SetWidth(w int) { p.width = w }

// Selected returns the currently highlighted command, if any.
func (p *Palette) Selected() (PaletteCommand, bool) {
	if p.cursor < 0 || p.cursor >= len(p.filtered) {
		return PaletteCommand{}, false
	}
	return p.filtered[p.cursor], true
}

// UpdateKey handles a key press and returns whether it was consumed.
func (p *Palette) UpdateKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return true
	case "down":
		limit := len(p.filtered)
		if limit > paletteMaxVisible {
			limit = paletteMaxVisible
		}
		if p.cursor < limit-1 {
			p.cursor++
		}
		return true
	case "esc":
		p.Hide()
		return true
	case "enter":
		return true // caller reads Selected()
	default:
		p.input, _ = p.input.Update(msg)
		p.filter(p.input.Value())
		return true
	}
}

func (p *Palette) filter(query string) {
	if query == "" {
		p.filtered = p.commands
		p.cursor = 0
		return
	}
	q := strings.ToLower(query)
	var out []PaletteCommand
	for _, cmd := range p.commands {
		if strings.Contains(strings.ToLower(cmd.Name), q) ||
			strings.Contains(strings.ToLower(cmd.Description), q) {
			out = append(out, cmd)
		}
	}
	p.filtered = out
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

// View renders the palette overlay.
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
		if i >= paletteMaxVisible {
			break
		}
		nameStyle := lipgloss.NewStyle().Foreground(p.theme.Primary).Bold(true)
		descStyle := lipgloss.NewStyle().Foreground(p.theme.Muted)
		if i == p.cursor {
			nameStyle = nameStyle.Background(p.theme.Border)
			descStyle = descStyle.Background(p.theme.Border)
		}
		name := nameStyle.Render(cmd.Name)
		desc := descStyle.Render("  " + cmd.Description)
		sb.WriteString(name + desc + "\n")
	}

	return border.Render(sb.String())
}
