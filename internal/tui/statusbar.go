package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// statusBarInfo holds the data displayed in the bottom status bar.
type statusBarInfo struct {
	Model      string
	Session    string
	TokensIn   int
	TokensOut  int
	CostUSD    float64
	ToolActive string // currently running tool, or ""
}

// renderStatusBar creates a rich status bar like OpenCode's.
// Shows: [mode] │ model │ tokens │ cost │ tool activity
func (a *App) renderStatusBar(info statusBarInfo) string {
	t := a.theme
	width := a.width

	// Left side: mode indicator
	modeStyle := lipgloss.NewStyle().
		Background(t.Primary).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1)
	mode := modeStyle.Render("altcode")

	if a.vimMode {
		mode = modeStyle.Background(t.Warning).Render("NORMAL")
	}

	// Segments
	segments := []string{}

	if info.Model != "" {
		segments = append(segments, lipgloss.NewStyle().
			Foreground(t.Secondary).
			Render(info.Model))
	}

	if info.TokensIn+info.TokensOut > 0 {
		tokenStr := fmt.Sprintf("%s in / %s out",
			formatTokens(info.TokensIn), formatTokens(info.TokensOut))
		segments = append(segments, lipgloss.NewStyle().
			Foreground(t.Muted).
			Render(tokenStr))
	}

	if info.CostUSD > 0 {
		segments = append(segments, lipgloss.NewStyle().
			Foreground(t.Success).
			Render(fmt.Sprintf("$%.4f", info.CostUSD)))
	}

	if info.ToolActive != "" {
		segments = append(segments, lipgloss.NewStyle().
			Foreground(t.Warning).
			Bold(true).
			Render("⟳ "+info.ToolActive))
	}

	sep := lipgloss.NewStyle().Foreground(t.Border).Render(" │ ")
	right := strings.Join(segments, sep)

	// Fill the gap
	gap := width - lipgloss.Width(mode) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}
	filler := strings.Repeat(" ", gap)

	bar := lipgloss.NewStyle().
		Background(t.HeaderBg).
		Width(width).
		Render(mode + filler + right + " ")

	return bar
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
