package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// MarkdownRenderer renders markdown text with syntax highlighting for the TUI.
type MarkdownRenderer struct {
	width    int
	renderer *glamour.TermRenderer
	cache    map[string]string
}

// NewMarkdownRenderer creates a renderer for the given terminal width.
func NewMarkdownRenderer(width int) *MarkdownRenderer {
	// Use explicit dark style — WithAutoStyle() sends OSC 11 to query
	// the terminal background color, and the response can leak into the
	// viewport as "]11;rgb:1e1e/1e1e/1e1e\" on some terminals.
	r, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	return &MarkdownRenderer{
		width:    width,
		renderer: r,
		cache:    make(map[string]string),
	}
}

// Render converts markdown text to styled terminal output.
func (r *MarkdownRenderer) Render(input string) string {
	if cached, ok := r.cache[input]; ok {
		return cached
	}

	// Detect incomplete code blocks for streaming indicator.
	if isStreaming(input) {
		out := r.renderWithGlamour(input)
		out += streamingStyle.Render(" streaming...")
		return out
	}

	result := r.renderWithGlamour(input)
	r.cache[input] = result
	return result
}

// renderWithGlamour delegates to the glamour renderer with
// a raw-text fallback on error. Preprocesses to strip heading markers
// since glamour renders them literally (unlike CC which hides them).
func (r *MarkdownRenderer) renderWithGlamour(input string) string {
	if r.renderer == nil {
		return input
	}
	// Strip markdown heading markers — CC shows bold colored text, not ### Header
	processed := stripHeadingMarkers(input)
	out, err := r.renderer.Render(processed)
	if err != nil {
		return input
	}
	return out
}

// stripHeadingMarkers converts "### Header" to "**Header**" so glamour
// renders them as bold text instead of showing the # markers literally.
func stripHeadingMarkers(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "# ") {
			lines[i] = "**" + strings.TrimPrefix(trimmed, "# ") + "**"
		} else if strings.HasPrefix(trimmed, "## ") {
			lines[i] = "**" + strings.TrimPrefix(trimmed, "## ") + "**"
		} else if strings.HasPrefix(trimmed, "### ") {
			lines[i] = "**" + strings.TrimPrefix(trimmed, "### ") + "**"
		} else if strings.HasPrefix(trimmed, "#### ") {
			lines[i] = "**" + strings.TrimPrefix(trimmed, "#### ") + "**"
		}
	}
	return strings.Join(lines, "\n")
}

// isStreaming returns true when the input contains an unclosed
// fenced code block, indicating the model is still generating.
func isStreaming(input string) bool {
	fences := 0
	for _, line := range strings.Split(input, "\n") {
		if strings.HasPrefix(line, "```") {
			fences++
		}
	}
	return fences%2 != 0
}

var streamingStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#6C7086")).
	Italic(true)
