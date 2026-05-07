package tui

import (
	"fmt"
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
	Group       string
	Action      func() string
}

// Palette renders a fuzzy-filtered command picker.
type Palette struct {
	theme    Theme
	width    int
	visible  bool
	cursor   int
	offset   int // first visible row in p.filtered (scroll position)
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
	p.offset = 0
	p.input.Focus()
	p.input.Reset()
	p.filtered = p.commands
}

// ShowWithQuery opens the palette with an initial filter.
func (p *Palette) ShowWithQuery(query string) {
	p.Show()
	p.input.SetValue(query)
	p.filter(query)
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
			if p.cursor < p.offset {
				p.offset = p.cursor
			}
		}
		return true
	case "down":
		// Cap against the filtered list, NOT the visible window —
		// previously the cursor stopped at index 9 even when 50
		// matches existed, making items past the first page
		// completely unreachable.
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
			if p.cursor >= p.offset+paletteMaxVisible {
				p.offset = p.cursor - paletteMaxVisible + 1
			}
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
		p.offset = 0
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
	// Reset scroll on filter change so the user always sees the top.
	p.offset = 0
}

// truncateRunes returns s truncated to at most max display runes,
// appending an ellipsis when truncated. Uses []rune slicing so multi-byte
// characters (CJK, accented) don't get split.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max < 1 {
		return ""
	}
	return string(r[:max-1]) + "…"
}

// collapseWhitespace replaces every run of whitespace (incl. newlines)
// with a single space. Skill descriptions often contain embedded
// newlines from their SKILL.md frontmatter, which Lipgloss would
// otherwise turn into multi-row entries in the palette.
func collapseWhitespace(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				sb.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		sb.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(sb.String())
}

// View renders the palette overlay.
func (p *Palette) View() string {
	if !p.visible {
		return ""
	}

	// Capped width — never wider than 72, centered in viewport
	boxWidth := p.width - 8
	if boxWidth > 72 {
		boxWidth = 72
	}
	if boxWidth < 32 {
		boxWidth = 32
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.theme.Primary).
		Background(p.theme.HeaderBg).
		Padding(0, 1).
		Width(boxWidth)

	var sb strings.Builder
	sb.WriteString(p.input.View())
	sb.WriteByte('\n')

	// Compute the per-line content budget: box minus the rounded border
	// (2 cells) and horizontal padding (2 cells). Without explicit
	// truncation, lipgloss soft-wraps long descriptions on character
	// boundaries, producing splits like "application/s" or "a/sks".
	contentWidth := boxWidth - 4
	if contentWidth < 16 {
		contentWidth = 16
	}

	// Empty state — without this, filtering for a non-matching query
	// showed an empty box with no feedback. Users typed gibberish and
	// thought the palette was broken.
	if len(p.filtered) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(p.theme.Muted).Italic(true)
		sb.WriteString(emptyStyle.Render("  no matches"))
		sb.WriteByte('\n')
		box := border.Render(sb.String())
		return lipgloss.PlaceHorizontal(p.width, lipgloss.Center, box)
	}

	// Render the visible window starting at p.offset. Cursor scrolls
	// with the arrow keys (UpdateKey adjusts offset to keep the
	// selection in view). Without this, items past index 9 used to
	// be unreachable because the loop also bounded i < paletteMaxVisible.
	end := p.offset + paletteMaxVisible
	if end > len(p.filtered) {
		end = len(p.filtered)
	}
	for i := p.offset; i < end; i++ {
		cmd := p.filtered[i]
		if i == p.offset || cmd.Group != p.filtered[i-1].Group {
			group := cmd.Group
			if group == "" {
				group = "Commands"
			}
			sb.WriteString(lipgloss.NewStyle().
				Foreground(p.theme.Muted).
				Bold(true).
				Render(group))
			sb.WriteByte('\n')
		}
		nameStyle := lipgloss.NewStyle().Foreground(p.theme.Primary).Bold(true)
		descStyle := lipgloss.NewStyle().Foreground(p.theme.Muted)
		if i == p.cursor {
			nameStyle = nameStyle.Background(p.theme.Primary).Foreground(lipgloss.Color("#000000"))
			descStyle = descStyle.Background(p.theme.Primary).Foreground(lipgloss.Color("#000000"))
		}
		// Truncate description so the whole line (name + 2 spaces + desc)
		// fits within contentWidth. Names are short (~16 cells max), so
		// the description gets the remainder.
		nameRunes := []rune(cmd.Name)
		descBudget := contentWidth - len(nameRunes) - 2
		if descBudget < 8 {
			descBudget = 8
		}
		descText := truncateRunes(collapseWhitespace(cmd.Description), descBudget)

		name := nameStyle.Render(cmd.Name)
		desc := descStyle.Render("  " + descText)
		sb.WriteString(name + desc + "\n")
	}
	// Show "+N more" footer when there are items below the visible window.
	if end < len(p.filtered) {
		moreStyle := lipgloss.NewStyle().Foreground(p.theme.Muted).Italic(true)
		sb.WriteString(moreStyle.Render(fmt.Sprintf("  +%d more (↓)", len(p.filtered)-end)))
		sb.WriteByte('\n')
	}

	box := border.Render(sb.String())
	// Center the palette box in the available width
	return lipgloss.PlaceHorizontal(p.width, lipgloss.Center, box)
}
