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

// smartTruncate preserves the basename (last path component) and truncates
// the prefix with "…/" if the string is too long for the available width.
// For non-path strings (commands), truncates from the right.
func smartTruncate(s string, maxLen int) string {
	if maxLen < 5 {
		maxLen = 5
	}
	// Count runes, not bytes — multi-byte UTF-8 (CJK, emoji, accented
	// characters) would otherwise truncate mid-rune and emit invalid
	// UTF-8 / replacement glyphs. Slice via the rune slice for safety.
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	// If it looks like a path, keep the basename.
	if i := strings.LastIndex(s, "/"); i >= 0 {
		baseRunes := []rune(s[i+1:])
		if len(baseRunes) < maxLen-2 {
			return "…/" + string(baseRunes)
		}
		return string(baseRunes[:maxLen-3]) + "..."
	}
	return string(runes[:maxLen-3]) + "..."
}

// toolEntry records a single tool call for the tool tree display.
type toolEntry struct {
	id        string // tool call ID for matching Start/Done pairs
	name      string
	detail    string // e.g. file path, command
	status    string // "running", "done", "error"
	elapsed   time.Duration
	startedAt time.Time // when the tool started (for live elapsed display)
	output    string    // truncated result output (diff lines, bash output)
	input     string    // raw command/input for display (Bash command, etc.)
}

// toolTree manages the list of tool calls for the current turn.
type toolTree struct {
	entries []toolEntry
	active  int    // index of currently running tool, -1 if none
	// projectRoot is captured by the App and used to absolutise file
	// paths emitted in tool output, so the OSC-8 hyperlinks render
	// correctly (file:// requires absolute paths).
	projectRoot string
}

func newToolTree() *toolTree {
	return &toolTree{active: -1}
}

// Start records a new tool call starting. The id is the unique tool call
// ID from the provider so that Done can match the correct entry even when
// multiple tools run concurrently.
func (t *toolTree) Start(id, name, detail string) {
	t.entries = append(t.entries, toolEntry{
		id:        id,
		name:      name,
		detail:    detail,
		status:    "running",
		startedAt: time.Now(),
	})
	t.active = len(t.entries) - 1
}

// Done marks the matching running tool as complete. It first tries to
// match by tool call ID (exact match); if no ID is provided or no match
// is found, it falls back to the oldest running entry.
func (t *toolTree) Done(id, title string, elapsed time.Duration) {
	idx := t.findRunningByID(id)
	if idx >= 0 {
		t.entries[idx].status = "done"
		t.entries[idx].elapsed = elapsed
		if title != "" {
			t.entries[idx].detail = title
		}
	}
	t.active = -1
}

// DoneWithOutput marks the matching running tool as complete with output.
func (t *toolTree) DoneWithOutput(id, title string, elapsed time.Duration, output string) {
	idx := t.findRunningByID(id)
	if idx >= 0 {
		t.entries[idx].status = "done"
		t.entries[idx].elapsed = elapsed
		if title != "" {
			t.entries[idx].detail = title
		}
		t.entries[idx].output = output
	}
	t.active = -1
}

// findRunningByID returns the index of the running entry with the given
// tool call ID. If id is empty or no match is found, falls back to the
// oldest running entry (preserving legacy behavior).
func (t *toolTree) findRunningByID(id string) int {
	if id != "" {
		for i, e := range t.entries {
			if e.status == "running" && e.id == id {
				return i
			}
		}
	}
	return t.findRunning()
}

// findRunning returns the index of the oldest running entry, or -1.
func (t *toolTree) findRunning() int {
	for i, e := range t.entries {
		if e.status == "running" {
			return i
		}
	}
	return -1
}

// HasRunning reports whether any tool entry is still running.
// Used by the TUI to decide whether to re-render on spinner ticks so
// elapsed-time labels update live.
func (t *toolTree) HasRunning() bool {
	return t.findRunning() >= 0
}

// DoneWithError marks the matching running tool as failed.
func (t *toolTree) DoneWithError(id, title string, elapsed time.Duration) {
	idx := t.findRunningByID(id)
	if idx >= 0 {
		t.entries[idx].status = "error"
		t.entries[idx].elapsed = elapsed
		if title != "" {
			t.entries[idx].detail = title
		}
	}
	t.active = -1
}

// DoneWithErrorOutput marks the matching running tool as failed and
// stores the error message for display.
func (t *toolTree) DoneWithErrorOutput(id, title string, elapsed time.Duration, errMsg string) {
	idx := t.findRunningByID(id)
	if idx >= 0 {
		t.entries[idx].status = "error"
		t.entries[idx].elapsed = elapsed
		if title != "" {
			t.entries[idx].detail = title
		}
		t.entries[idx].output = errMsg
	}
	t.active = -1
}

// Clear resets the tree for the next turn.
func (t *toolTree) Clear() {
	t.entries = t.entries[:0]
	t.active = -1
}

// SweepRunning drops any still-running entries. Called at the end of a
// turn when we've rendered the final snapshot — anything left in "running"
// state is a zombie from a tool call whose ToolResult event never arrived.
func (t *toolTree) SweepRunning() {
	out := t.entries[:0]
	for _, e := range t.entries {
		if e.status != "running" {
			out = append(out, e)
		}
	}
	t.entries = out
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

// RenderLive returns the tree during active execution — NO collapsing.
// This prevents visual height jumping when consecutive tools complete.
//
// Uses ⏺ bullets (not tree-branch chars) so the live tree reads cleanly
// even when no parent message is visible directly above it. The earlier
// ├─/└─ rendering looked orphaned when intermediate assistant text or
// info messages appeared between the user prompt and the tools — see
// DeepSeek-TUI's flatter "stream of bullets" visual style.
func (t *toolTree) RenderLive(theme Theme, width int) string {
	if len(t.entries) == 0 {
		return ""
	}
	// Convert entries to []any without collapsing
	items := make([]any, len(t.entries))
	for i, e := range t.entries {
		items[i] = e
	}
	return t.renderItems(items, theme, width, false /*flat bullets, not tree*/)
}

// Render returns the tool tree with collapsed groups (for final display
// after the turn ends). Uses bullet (⏺) prefix — the persisted tree
// is wrapped in an info message, so tree-branch chars (├─/└─) would
// dangle without a parent line and read confusingly.
func (t *toolTree) Render(theme Theme, width int) string {
	if len(t.entries) == 0 {
		return ""
	}
	items := collapseEntries(t.entries)
	return t.renderItems(items, theme, width, false /*persisted*/)
}

func (t *toolTree) renderItems(items []any, theme Theme, width int, tree bool) string {
	var sb strings.Builder
	for i, item := range items {
		var prefix string
		if tree {
			prefix = "├─"
			if i == len(items)-1 {
				prefix = "└─"
			}
		} else {
			// Persisted-snapshot mode: every tool is a top-level bullet.
			prefix = "⏺"
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

			// Dim completed entries to create visual hierarchy —
			// active tool pops, completed fades into background
			nameColor := theme.Primary
			if e.status == "done" && i < len(items)-1 {
				nameColor = theme.Muted // dim historical tools
			}

			nameText := e.name
			if e.detail != "" {
				// Tool titles sometimes repeat the tool name (e.g. "read /path").
				// Strip the duplicate prefix — case-insensitive — so we don't
				// render "read(read /path)" or "Read(Read /path)".
				detail := e.detail
				lowerDetail := strings.ToLower(detail)
				lowerName := strings.ToLower(e.name)
				if strings.HasPrefix(lowerDetail, lowerName+" ") {
					detail = strings.TrimSpace(detail[len(e.name)+1:])
				} else if strings.HasPrefix(lowerDetail, lowerName+":") {
					detail = strings.TrimSpace(detail[len(e.name)+1:])
				}
				// Width math must use display columns, not bytes —
				// `lipgloss.Width("├─")` is 2 (cells) but `len("├─")`
				// is 6 (bytes), and Unicode tool names like
				// `mcp__rüm__open` get over-truncated under byte
				// counting. Guard against negative budgets so a
				// narrow column doesn't clamp to 5 chars.
				avail := width - lipgloss.Width(prefix) - lipgloss.Width(e.name) - 14
				if avail < 10 {
					avail = 10
				}
				det := smartTruncate(detail, avail)
				nameText = e.name + "(" + det + ")"
			}
			nameRendered := lipgloss.NewStyle().Foreground(nameColor).Bold(e.status == "running").Render(nameText)
			detailRendered := "" // detail is now inside the name parens

			timing := ""
			if e.status == "done" && e.elapsed > 0 {
				timing = lipgloss.NewStyle().Foreground(theme.Muted).
					Render(" " + formatToolDuration(e.elapsed))
			}
			// Running tool: show elapsed time only. We intentionally don't
			// advertise the tool's max timeout here — users were reading
			// "timeout 2m" on every running tool as if a timeout had fired.
			if e.status == "running" && !e.startedAt.IsZero() {
				runElapsed := time.Since(e.startedAt)
				if runElapsed >= time.Second {
					timing = lipgloss.NewStyle().Foreground(theme.Warning).
						Render(" " + formatToolDuration(runElapsed))
				}
			}

			line := lipgloss.NewStyle().Foreground(theme.Border).
				Render(prefix+" ") + iconRendered + " " + nameRendered + detailRendered + timing
			sb.WriteString(line)
			sb.WriteByte('\n')

			// Render output below the tool entry (CC style: ⎿ output lines).
			// Project root is captured at render time and passed to the
			// output formatter so file:line refs in tool output become
			// OSC-8 hyperlinks (DeepSeek-TUI #374). Empty root falls
			// back to relative file:// URIs (some terminals still
			// render those as clickable anyway).
			if e.output != "" && e.status != "running" {
				connColor := theme.Border
				if e.status == "error" {
					connColor = theme.Error // red connector for errors
				}
				outputLines := formatToolOutput(e.name, e.output, theme, max(10, width-6), t.projectRoot)
				for _, ol := range outputLines {
					sb.WriteString("   " + lipgloss.NewStyle().Foreground(connColor).Render("⎿") + "  " + ol + "\n")
				}
			}
		}
	}
	return sb.String()
}

// formatToolOutput formats tool result output for display below the tool entry.
// Edit/Write: shows diff lines with +/- coloring and line numbers.
// Bash: shows truncated output lines (with file:line OSC-8 hyperlinks).
// Other: shows first few lines of output (with file:line OSC-8 hyperlinks).
func formatToolOutput(toolName, output string, theme Theme, maxWidth int, projectRoot string) []string {
	if output == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	maxLines := 8 // show at most 8 lines of output

	switch toolName {
	case "Edit", "Write":
		return formatDiffOutput(lines, theme, maxWidth, maxLines)
	case "Bash":
		return formatBashOutput(lines, theme, maxWidth, maxLines, projectRoot)
	default:
		return formatGenericOutput(lines, theme, maxWidth, maxLines, projectRoot)
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
			// CC-style collapsed output hint
			result = append(result,
				ctxStyle.Render(fmt.Sprintf("… +%d lines", len(lines)-maxLines)))
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

// formatBashOutput renders command output with truncation. File-path
// refs (path:line[:col]) get wrapped in OSC-8 hyperlinks before the
// dim styling — the terminal nests color around the link cleanly.
func formatBashOutput(lines []string, theme Theme, maxWidth, maxLines int, projectRoot string) []string {
	var result []string
	dim := lipgloss.NewStyle().Foreground(theme.Muted)

	for _, line := range lines {
		if len(result) >= maxLines {
			result = append(result,
				dim.Render(fmt.Sprintf("… +%d lines", len(lines)-maxLines)))
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Truncate FIRST (visual-width math doesn't account for OSC-8
		// escapes — lipgloss.Width returns 0 for them, but the regex
		// matches the truncated text just as well as the full line).
		truncated := truncateStr(line, maxWidth)
		result = append(result, dim.Render(LinkifyFileRefs(truncated, projectRoot)))
	}
	return result
}

// formatGenericOutput renders generic tool output, wrapping file:line
// refs in OSC-8 hyperlinks so users can click straight to the source.
func formatGenericOutput(lines []string, theme Theme, maxWidth, maxLines int, projectRoot string) []string {
	var result []string
	dim := lipgloss.NewStyle().Foreground(theme.Muted)

	for _, line := range lines {
		if len(result) >= maxLines {
			result = append(result,
				dim.Render(fmt.Sprintf("… +%d lines", len(lines)-maxLines)))
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		truncated := truncateStr(trimmed, maxWidth)
		result = append(result, dim.Render(LinkifyFileRefs(truncated, projectRoot)))
	}
	return result
}
