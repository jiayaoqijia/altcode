package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderInlineDiff colorizes a unified diff string for the TUI.
// Added lines get green, removed lines get red, context stays muted.
func renderInlineDiff(diff string, theme Theme, maxLines int) string {
	if diff == "" {
		return ""
	}

	lines := strings.Split(diff, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	addStyle := lipgloss.NewStyle().Foreground(theme.DiffAdd)
	delStyle := lipgloss.NewStyle().Foreground(theme.DiffDel)
	hunkStyle := lipgloss.NewStyle().Foreground(theme.Secondary).Bold(true)
	ctxStyle := lipgloss.NewStyle().Foreground(theme.Muted)

	var sb strings.Builder
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			sb.WriteString(hunkStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			sb.WriteString(hunkStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			sb.WriteString(addStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			sb.WriteString(delStyle.Render(line))
		default:
			sb.WriteString(ctxStyle.Render(line))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
