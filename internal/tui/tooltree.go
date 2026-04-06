package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// toolEntry records a single tool call for the tool tree display.
type toolEntry struct {
	name    string
	detail  string // e.g. file path, command
	status  string // "running", "done", "error"
	elapsed time.Duration
}

// toolTree manages the list of tool calls for the current turn.
type toolTree struct {
	entries []toolEntry
	active  int // index of currently running tool, -1 if none
}

func newToolTree() *toolTree {
	return &toolTree{active: -1}
}

// Start records a new tool call starting.
func (t *toolTree) Start(name, detail string) {
	t.entries = append(t.entries, toolEntry{
		name:   name,
		detail: detail,
		status: "running",
	})
	t.active = len(t.entries) - 1
}

// Done marks the current tool as complete.
func (t *toolTree) Done(title string, elapsed time.Duration) {
	if t.active >= 0 && t.active < len(t.entries) {
		t.entries[t.active].status = "done"
		t.entries[t.active].elapsed = elapsed
		if title != "" {
			t.entries[t.active].detail = title
		}
	}
	t.active = -1
}

// Clear resets the tree for the next turn.
func (t *toolTree) Clear() {
	t.entries = t.entries[:0]
	t.active = -1
}

// Render returns the tool tree as styled text.
func (t *toolTree) Render(theme Theme, width int) string {
	if len(t.entries) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, e := range t.entries {
		prefix := "├─"
		if i == len(t.entries)-1 {
			prefix = "└─"
		}

		var icon string
		var iconColor lipgloss.Color
		switch e.status {
		case "running":
			icon = "⟳"
			iconColor = theme.Warning
		case "done":
			icon = "✓"
			iconColor = theme.Success
		case "error":
			icon = "✗"
			iconColor = theme.Error
		}

		iconRendered := lipgloss.NewStyle().
			Foreground(iconColor).
			Render(icon)

		nameRendered := lipgloss.NewStyle().
			Foreground(theme.Primary).
			Bold(true).
			Render(e.name)

		detailRendered := ""
		if e.detail != "" {
			det := e.detail
			// Truncate long paths
			maxDet := width - len(prefix) - len(e.name) - 12
			if maxDet < 10 {
				maxDet = 10
			}
			if len(det) > maxDet {
				det = "…" + det[len(det)-maxDet+1:]
			}
			detailRendered = lipgloss.NewStyle().
				Foreground(theme.Muted).
				Render(" " + det)
		}

		timing := ""
		if e.status == "done" && e.elapsed > 0 {
			timing = lipgloss.NewStyle().
				Foreground(theme.Muted).
				Render(fmt.Sprintf(" %dms", e.elapsed.Milliseconds()))
		}

		line := lipgloss.NewStyle().
			Foreground(theme.Border).
			Render(prefix+" ") + iconRendered + " " + nameRendered + detailRendered + timing

		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}
