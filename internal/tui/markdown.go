package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// MarkdownRenderer renders markdown text with syntax highlighting for the TUI.
//
// The cache is a bounded LRU keyed by input markdown. The previous
// implementation reallocated the entire map when it hit the bound,
// effectively defeating caching during long sessions — every entry
// would be evicted at once and the next 100 messages would each
// re-render via Glamour.
type MarkdownRenderer struct {
	width    int
	renderer *glamour.TermRenderer
	cache    map[string]string
	// order is a FIFO of cache keys in insertion order. When the
	// cache is full, the oldest key is evicted instead of dropping
	// the whole map.
	order []string
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
		order:    make([]string, 0, maxCacheEntries),
	}
}

const maxCacheEntries = 100 // bound cache to prevent memory leak in long sessions

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
	// FIFO eviction: drop the oldest entry once when full instead of
	// nuking the entire cache. Long sessions used to thrash because
	// every 100th render reset the cache and the next 100 renders
	// were all misses.
	if len(r.cache) >= maxCacheEntries && len(r.order) > 0 {
		oldest := r.order[0]
		delete(r.cache, oldest)
		r.order = r.order[1:]
	}
	r.cache[input] = result
	r.order = append(r.order, input)
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
		// Return the PROCESSED text on fallback, not the raw input.
		// Otherwise the heading lines that were converted to bold
		// markers above briefly revert to '### Header' literal style
		// when glamour errors out, causing visible formatting jumps.
		return processed
	}
	return out
}

// stripHeadingMarkers converts "### Header" to "**Header**" so glamour
// renders them as bold text instead of showing the # markers literally.
// If the heading contains backticks, just strip the # markers without
// adding ** to avoid glamour rendering conflicts.
func stripHeadingMarkers(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		var content string
		switch {
		case strings.HasPrefix(trimmed, "#### "):
			content = strings.TrimPrefix(trimmed, "#### ")
		case strings.HasPrefix(trimmed, "### "):
			content = strings.TrimPrefix(trimmed, "### ")
		case strings.HasPrefix(trimmed, "## "):
			content = strings.TrimPrefix(trimmed, "## ")
		case strings.HasPrefix(trimmed, "# "):
			content = strings.TrimPrefix(trimmed, "# ")
		default:
			continue
		}
		// Don't wrap in ** if content has backticks — causes glamour garble
		if strings.Contains(content, "`") {
			lines[i] = content
		} else {
			lines[i] = "**" + content + "**"
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
