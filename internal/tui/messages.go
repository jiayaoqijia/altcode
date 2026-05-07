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
	roleTrace
	roleInfo
	roleThinking
)

// chatMessage holds a rendered message with its role for styling.
type chatMessage struct {
	role    messageRole
	content string
	meta    string // e.g. "gpt-5.4 · 1.2s" or tool name
}

// renderMessage returns a chat message in the lightweight chat flow.
func (a *App) renderMessage(msg chatMessage) string {
	width := a.width - 2
	if width < 20 {
		width = 20
	}

	// Tool-tree snapshot info messages already start with their own
	// `⏺` bullet (set by tooltree.Render). The default roleInfo path
	// would wrap them with a `┃ ◈` border, producing the noisy stack
	// `┃ ◈ ⏺ ✓ read(...)`. Detect snapshots by their leading marker
	// and render them as plain text — the `⏺` bullets are sufficient
	// visual signal on their own. Single-line info messages don't
	// match (no leading `⏺`) so the existing border still wraps them.
	if msg.role == roleInfo && strings.HasPrefix(msg.content, "⏺") {
		return strings.TrimRight(msg.content, " \n")
	}

	var rendered string
	if msg.role == roleTool && looksLikeDiff(msg.content) {
		rendered = renderInlineDiff(msg.content, a.theme, 15)
	} else if msg.role == roleInfo || msg.role == roleUser || msg.role == roleTrace {
		// Info messages and user input are plain-text — skip markdown
		// rendering so embedded newlines (Ctrl+J multi-line input,
		// pasted code blocks) stay intact. Glamour collapses adjacent
		// lines into paragraphs which would otherwise turn a 5-line
		// pasted snippet into one wrapped line. Only render markdown
		// when the content is explicitly wrapped in a code fence,
		// which glamour preserves.
		if a.mdRenderer != nil && strings.Contains(msg.content, "```") {
			rendered = a.mdRenderer.Render(msg.content)
		} else {
			rendered = msg.content
		}
	} else if a.mdRenderer != nil && msg.role != roleTool {
		rendered = a.mdRenderer.Render(msg.content)
	} else {
		rendered = msg.content
	}

	rendered = strings.TrimRight(rendered, " \n")

	muted := lipgloss.NewStyle().Foreground(a.theme.Muted)
	metaLine := ""
	if msg.meta != "" && a.showMessageMeta {
		metaLine = "\n" + muted.Italic(true).Render("  "+msg.meta)
	}

	switch msg.role {
	case roleUser:
		return lipgloss.NewStyle().
			Foreground(a.theme.Secondary).
			Width(width).
			Render("> "+rendered) + metaLine
	case roleAssistant:
		return lipgloss.NewStyle().
			Width(width).
			Render(rendered) + metaLine
	case roleTool:
		return muted.Width(width).Render(rendered) + metaLine
	case roleTrace:
		return muted.Width(width).Render(rendered) + metaLine
	case roleInfo:
		return muted.Width(width).Render(rendered) + metaLine
	case roleThinking:
		// CC-style: show first 3 lines + collapsible hint
		lines := strings.Split(rendered, "\n")
		if len(lines) > 4 {
			rendered = strings.Join(lines[:3], "\n") +
				"\n" + lipgloss.NewStyle().Foreground(a.theme.Muted).Italic(true).
				Render(fmt.Sprintf("… +%d lines of reasoning", len(lines)-3))
		}
		return muted.Italic(true).Width(width).Render(rendered) + metaLine
	}
	return rendered + metaLine
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
