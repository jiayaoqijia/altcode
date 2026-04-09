package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// formatToolDuration formats a duration in CC style: "<1s", "3s", "1m 5s".
func formatToolDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	rs := s % 60
	if rs > 0 {
		return fmt.Sprintf("%dm %ds", m, rs)
	}
	return fmt.Sprintf("%dm", m)
}

// toolEntry records a single tool call for the tool tree display.
type toolEntry struct {
	name    string
	detail  string // e.g. file path, command
	status  string // "running", "done", "error"
	elapsed time.Duration
	output  string // truncated result output (diff lines, bash output)
	input   string // raw command/input for display (Bash command, etc.)
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

// Done marks the current tool as complete with optional output.
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

// DoneWithOutput marks the tool as complete and stores truncated output.
func (t *toolTree) DoneWithOutput(title string, elapsed time.Duration, output string) {
	t.Done(title, elapsed)
	idx := len(t.entries) - 1
	if idx >= 0 {
		t.entries[idx].output = output
	}
}

// DoneWithError marks the active tool as failed.
func (t *toolTree) DoneWithError(title string, elapsed time.Duration) {
	if t.active >= 0 && t.active < len(t.entries) {
		t.entries[t.active].status = "error"
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

// collapsedGroup represents a run of consecutive completed same-name tools.
type collapsedGroup struct {
	name    string
	count   int
	elapsed time.Duration
}

// collapseEntries groups consecutive done entries of the same tool name.
// Returns a mixed list of individual entries and groups for rendering.
func collapseEntries(entries []toolEntry) []any {
	var result []any
	i := 0
	for i < len(entries) {
		e := entries[i]
		// Only collapse completed entries (not running/error)
		if e.status != "done" {
			result = append(result, e)
			i++
			continue
		}
		// Count consecutive same-name done entries
		j := i + 1
		totalElapsed := e.elapsed
		for j < len(entries) && entries[j].name == e.name && entries[j].status == "done" {
			totalElapsed += entries[j].elapsed
			j++
		}
		count := j - i
		if count >= 3 {
			result = append(result, collapsedGroup{
				name: e.name, count: count, elapsed: totalElapsed,
			})
		} else {
			for k := i; k < j; k++ {
				result = append(result, entries[k])
			}
		}
		i = j
	}
	return result
}

// toolSummaryNoun returns a human-friendly noun for collapsed tool groups.
func toolSummaryNoun(name string, count int) string {
	plural := ""
	if count != 1 {
		plural = "s"
	}
	switch strings.ToLower(name) {
	case "read":
		return fmt.Sprintf("Read %d file%s", count, plural)
	case "glob":
		return fmt.Sprintf("Listed %d pattern%s", count, plural)
	case "grep":
		return fmt.Sprintf("Searched %d pattern%s", count, plural)
	case "edit":
		return fmt.Sprintf("Edited %d file%s", count, plural)
	case "write":
		return fmt.Sprintf("Wrote %d file%s", count, plural)
	case "bash":
		return fmt.Sprintf("Ran %d command%s", count, plural)
	default:
		return fmt.Sprintf("%s ×%d", name, count)
	}
}

// Render returns the tool tree as styled text with collapsed groups.
func (t *toolTree) Render(theme Theme, width int) string {
	if len(t.entries) == 0 {
		return ""
	}

	items := collapseEntries(t.entries)
	var sb strings.Builder
	for i, item := range items {
		prefix := "├─"
		if i == len(items)-1 {
			prefix = "└─"
		}

		switch v := item.(type) {
		case collapsedGroup:
			icon := lipgloss.NewStyle().Foreground(theme.Success).Render("✓")
			summary := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true).
				Render(toolSummaryNoun(v.name, v.count))
			timing := ""
			if v.elapsed > 0 {
				timing = lipgloss.NewStyle().Foreground(theme.Muted).
					Render(" " + formatToolDuration(v.elapsed))
			}
			line := lipgloss.NewStyle().Foreground(theme.Border).
				Render(prefix+" ") + icon + " " + summary + timing
			sb.WriteString(line)
			sb.WriteByte('\n')

		case toolEntry:
			e := v
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

			iconRendered := lipgloss.NewStyle().Foreground(iconColor).Render(icon)

			// CC style: "Edit(app.go)" / "Bash(go test ./...)" not "Edit app.go"
			nameText := e.name
			if e.detail != "" {
				det := e.detail
				maxDet := width - len(prefix) - len(e.name) - 14
				if maxDet < 10 {
					maxDet = 10
				}
				if len(det) > maxDet {
					det = det[:maxDet-3] + "..."
				}
				nameText = e.name + "(" + det + ")"
			}
			nameRendered := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true).Render(nameText)
			detailRendered := "" // detail is now inside the name parens

			timing := ""
			if e.status == "done" && e.elapsed > 0 {
				timing = lipgloss.NewStyle().Foreground(theme.Muted).
					Render(" " + formatToolDuration(e.elapsed))
			}

			line := lipgloss.NewStyle().Foreground(theme.Border).
				Render(prefix+" ") + iconRendered + " " + nameRendered + detailRendered + timing
			sb.WriteString(line)
			sb.WriteByte('\n')

			// Render output below the tool entry (CC style: ⎿ output lines)
			if e.output != "" && e.status != "running" {
				outputLines := formatToolOutput(e.name, e.output, theme, width-6)
				for _, ol := range outputLines {
					sb.WriteString("   " + lipgloss.NewStyle().Foreground(theme.Border).Render("⎿") + "  " + ol + "\n")
				}
			}
		}
	}
	return sb.String()
}

// formatToolOutput formats tool result output for display below the tool entry.
// Edit/Write: shows diff lines with +/- coloring and line numbers.
// Bash: shows truncated output lines.
// Other: shows first few lines of output.
func formatToolOutput(toolName, output string, theme Theme, maxWidth int) []string {
	if output == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	maxLines := 8 // show at most 8 lines of output

	switch toolName {
	case "Edit", "Write":
		return formatDiffOutput(lines, theme, maxWidth, maxLines)
	case "Bash":
		return formatBashOutput(lines, theme, maxWidth, maxLines)
	default:
		return formatGenericOutput(lines, theme, maxWidth, maxLines)
	}
}

// formatDiffOutput renders diff-style lines with +/- coloring.
func formatDiffOutput(lines []string, theme Theme, maxWidth, maxLines int) []string {
	var result []string
	addStyle := lipgloss.NewStyle().Foreground(theme.Success)
	removeStyle := lipgloss.NewStyle().Foreground(theme.Error)
	ctxStyle := lipgloss.NewStyle().Foreground(theme.Muted)

	for _, line := range lines {
		if len(result) >= maxLines {
			result = append(result,
				ctxStyle.Render(fmt.Sprintf("… %d more lines", len(lines)-maxLines)))
			break
		}
		display := truncateStr(line, maxWidth)
		if strings.HasPrefix(line, "+") {
			result = append(result, addStyle.Render(display))
		} else if strings.HasPrefix(line, "-") {
			result = append(result, removeStyle.Render(display))
		} else {
			result = append(result, ctxStyle.Render(display))
		}
	}
	return result
}

// formatBashOutput renders command output with truncation.
func formatBashOutput(lines []string, theme Theme, maxWidth, maxLines int) []string {
	var result []string
	dim := lipgloss.NewStyle().Foreground(theme.Muted)

	for _, line := range lines {
		if len(result) >= maxLines {
			result = append(result,
				dim.Render(fmt.Sprintf("… %d more lines", len(lines)-maxLines)))
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		result = append(result, dim.Render(truncateStr(line, maxWidth)))
	}
	return result
}

// formatGenericOutput renders generic tool output.
func formatGenericOutput(lines []string, theme Theme, maxWidth, maxLines int) []string {
	var result []string
	dim := lipgloss.NewStyle().Foreground(theme.Muted)

	for _, line := range lines {
		if len(result) >= maxLines {
			result = append(result,
				dim.Render(fmt.Sprintf("… %d more lines", len(lines)-maxLines)))
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		result = append(result, dim.Render(truncateStr(trimmed, maxWidth)))
	}
	return result
}
