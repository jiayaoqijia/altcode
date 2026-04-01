package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Header renders the top bar with model info and token counts.
type Header struct {
	model      string
	tokens     int
	contextPct float64
	theme      Theme
	width      int
}

// NewHeader creates a Header with the given theme.
func NewHeader(theme Theme) *Header {
	return &Header{theme: theme}
}

func (h *Header) SetModel(model string)    { h.model = model }
func (h *Header) SetTokens(tokens int)     { h.tokens = tokens }
func (h *Header) SetContextPct(pct float64) { h.contextPct = pct }
func (h *Header) SetWidth(width int)       { h.width = width }

func (h *Header) View() string {
	logo := lipgloss.NewStyle().
		Foreground(h.theme.Primary).
		Bold(true).
		Render("altcode")

	model := lipgloss.NewStyle().
		Foreground(h.theme.Secondary).
		Render(h.model)

	info := lipgloss.NewStyle().
		Foreground(h.theme.Muted).
		Render(fmt.Sprintf("  tokens: %d  context: %.0f%%", h.tokens, h.contextPct*100))

	left := logo + "  " + model
	right := info

	gap := h.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + fmt.Sprintf("%*s", gap, "") + right
}
