package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MarkdownRenderer renders markdown text with syntax highlighting for the TUI.
type MarkdownRenderer struct {
	width int
	cache map[string]string
}

// NewMarkdownRenderer creates a renderer for the given terminal width.
func NewMarkdownRenderer(width int) *MarkdownRenderer {
	return &MarkdownRenderer{
		width: width,
		cache: make(map[string]string),
	}
}

// Render converts markdown text to styled terminal output.
func (r *MarkdownRenderer) Render(input string) string {
	if cached, ok := r.cache[input]; ok {
		return cached
	}

	var sb strings.Builder
	lines := strings.Split(input, "\n")
	inCodeBlock := false
	codeBlockLang := ""
	var codeLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				sb.WriteString(renderCodeBlock(codeBlockLang, codeLines, r.width))
				inCodeBlock = false
				codeLines = nil
				codeBlockLang = ""
			} else {
				inCodeBlock = true
				codeBlockLang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
			}
			continue
		}

		if inCodeBlock {
			codeLines = append(codeLines, line)
			continue
		}

		sb.WriteString(renderInline(line))
		sb.WriteByte('\n')
	}

	if inCodeBlock {
		sb.WriteString(renderCodeBlock(codeBlockLang, codeLines, r.width))
		sb.WriteString(streamingStyle.Render(" streaming..."))
		sb.WriteByte('\n')
	}

	result := sb.String()
	if !inCodeBlock {
		r.cache[input] = result
	}
	return result
}

var (
	codeBlockStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1E1E2E")).
			Foreground(lipgloss.Color("#CDD6F4")).
			Padding(0, 1)

	headingStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#CBA6F7"))

	boldStyle = lipgloss.NewStyle().Bold(true)

	streamingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C7086")).
			Italic(true)

	inlineCodeStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#313244")).
			Foreground(lipgloss.Color("#CDD6F4"))
)

func renderCodeBlock(lang string, lines []string, width int) string {
	content := strings.Join(lines, "\n")
	header := ""
	if lang != "" {
		header = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C7086")).
			Render(" "+lang) + "\n"
	}
	return header + codeBlockStyle.Width(width-2).Render(content) + "\n"
}

func renderInline(line string) string {
	if strings.HasPrefix(line, "### ") {
		return headingStyle.Render(strings.TrimPrefix(line, "### "))
	}
	if strings.HasPrefix(line, "## ") {
		return headingStyle.Render(strings.TrimPrefix(line, "## "))
	}
	if strings.HasPrefix(line, "# ") {
		return headingStyle.Render(strings.TrimPrefix(line, "# "))
	}

	line = renderBold(line)
	line = renderInlineCode(line)

	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return "  " + line
	}

	return line
}

func renderBold(s string) string {
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		bold := boldStyle.Render(s[start+2 : end])
		s = s[:start] + bold + s[end+2:]
	}
	return s
}

func renderInlineCode(s string) string {
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		code := inlineCodeStyle.Render(s[start+1 : end])
		s = s[:start] + code + s[end+1:]
	}
	return s
}
