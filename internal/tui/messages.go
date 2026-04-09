package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// messageRole identifies who sent a message in the chat.
type messageRole int

const (
	roleUser messageRole = iota
	roleAssistant
	roleTool
	roleInfo
	roleThinking
)

// chatMessage holds a rendered message with its role for styling.
type chatMessage struct {
	role    messageRole
	content string
	meta    string // e.g. "gpt-5.4 · 1.2s" or tool name
}

// renderMessage returns the message styled with a colored left border,
// matching OpenCode's visual pattern.
func (a *App) renderMessage(msg chatMessage) string {
	width := a.width - 4 // border + padding
	if width < 20 {
		width = 20
	}

	var rendered string
	if msg.role == roleTool && looksLikeDiff(msg.content) {
		rendered = renderInlineDiff(msg.content, a.theme, 15)
	} else if a.mdRenderer != nil && msg.role != roleTool {
		rendered = a.mdRenderer.Render(msg.content)
	} else {
		rendered = msg.content
	}

	// Trim trailing whitespace
	rendered = strings.TrimRight(rendered, " \n")

	var borderColor lipgloss.Color
	var icon string
	// Unified icon set — all geometric, same visual weight
	switch msg.role {
	case roleUser:
		borderColor = a.theme.Secondary
		icon = "❯"
	case roleAssistant:
		borderColor = a.theme.Primary
		icon = "●"
	case roleTool:
		borderColor = a.theme.Muted
		icon = "◇"
	case roleInfo:
		borderColor = a.theme.Warning
		icon = "◈"
	case roleThinking:
		borderColor = a.theme.Muted
		icon = "💭"
		// Collapse long thinking blocks
		if len(rendered) > 200 {
			lines := strings.SplitN(rendered, "\n", 4)
			if len(lines) > 3 {
				rendered = strings.Join(lines[:3], "\n") + "\n..."
			}
		}
	}

	// Meta line below message (model · duration · token count)
	metaLine := ""
	if msg.meta != "" {
		metaLine = "\n" + lipgloss.NewStyle().
			Foreground(a.theme.Muted).
			Italic(true).
			Render("╰ "+msg.meta)
	}

	// Icon + content
	header := lipgloss.NewStyle().
		Foreground(borderColor).
		Bold(true).
		Render(icon)

	body := header + " " + rendered + metaLine

	// Left border
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(width).
		Render(body)
}

// looksLikeDiff checks if content appears to be a unified diff.
func looksLikeDiff(s string) bool {
	return strings.HasPrefix(s, "---") ||
		strings.HasPrefix(s, "+++") ||
		strings.HasPrefix(s, "@@") ||
		(strings.Contains(s, "\n+") && strings.Contains(s, "\n-"))
}

// renderToolActivity returns a styled tool call indicator.
func (a *App) renderToolActivity(toolName, detail string) string {
	spinner := lipgloss.NewStyle().
		Foreground(a.theme.Warning).
		Bold(true).
		Render("⟳")

	name := lipgloss.NewStyle().
		Foreground(a.theme.Primary).
		Bold(true).
		Render(toolName)

	det := ""
	if detail != "" {
		det = lipgloss.NewStyle().
			Foreground(a.theme.Muted).
			Render(" " + detail)
	}

	return fmt.Sprintf("  %s %s%s", spinner, name, det)
}
