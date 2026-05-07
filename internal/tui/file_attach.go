package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jiayaoqijia/altcode/internal/completions"
)

const filePopupMaxVisible = 8

// filePopup tracks state for the @ file completion popup.
type filePopup struct {
	visible bool
	query   string
	matches []completions.Match
	cursor  int
}

// extractAtQuery finds the last @-mention in text and returns the
// query portion after it. Returns ("", false) when no active mention.
//
// Skips @ characters that look like they're inside an email address
// (preceded by an alphanumeric or '.'). Otherwise typing
// "ping user@example.com about" pops the file completer up because
// the LAST @ matches `user@example.com`. Real file mentions are
// preceded by whitespace or are at the start of the input.
func extractAtQuery(text string) (string, bool) {
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return "", false
	}
	// Reject email-like @: previous byte is a word char or '.'.
	if idx > 0 {
		prev := text[idx-1]
		if prev == '.' ||
			(prev >= 'a' && prev <= 'z') ||
			(prev >= 'A' && prev <= 'Z') ||
			(prev >= '0' && prev <= '9') {
			return "", false
		}
	}
	// @ at end of string means user just typed it.
	after := text[idx+1:]
	// If there is a space after the query, the mention is complete.
	if strings.Contains(after, " ") || strings.Contains(after, "\n") {
		return "", false
	}
	return after, true
}

// updateFilePopup refreshes popup matches based on input text.
func (a *App) updateFilePopup() {
	text := a.input.Value()
	query, active := extractAtQuery(text)
	if !active {
		a.filePopup.visible = false
		a.filePopup.matches = nil
		a.filePopup.cursor = 0
		return
	}
	root := a.projectRoot
	if root == "" {
		a.filePopup.visible = false
		return
	}
	prevQuery := a.filePopup.query
	prevCursor := a.filePopup.cursor
	a.filePopup.query = query
	a.filePopup.matches = completions.Complete(root, query, 10)
	a.filePopup.visible = len(a.filePopup.matches) > 0
	a.filePopup.cursor = 0
	if a.filePopup.visible && query == prevQuery && prevCursor < len(a.filePopup.matches) {
		a.filePopup.cursor = prevCursor
	}
}

// acceptFileCompletion replaces @query with the selected path,
// preserving any trailing text after the query.
func (a *App) acceptFileCompletion() bool {
	if !a.filePopup.visible {
		return false
	}
	if a.filePopup.cursor >= len(a.filePopup.matches) {
		return false
	}
	selected := a.filePopup.matches[a.filePopup.cursor]
	text := a.input.Value()
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return false
	}
	// Find the end of the @query — first whitespace after @ or end of text.
	// Without this, typing `look at @ma<TAB> for bug` would lose ` for bug`
	// because we'd splice on idx alone instead of (idx, queryEnd).
	end := idx + 1
	for end < len(text) && text[end] != ' ' && text[end] != '\n' && text[end] != '\t' {
		end++
	}
	replacement := selected.Path
	newText := text[:idx] + replacement + text[end:]
	a.input.SetValue(newText)
	a.rememberFileMention(selected.Path)
	a.filePopup.visible = false
	a.filePopup.matches = nil
	a.filePopup.cursor = 0
	return true
}

func (a *App) rememberFileMention(path string) {
	if path == "" {
		return
	}
	for _, existing := range a.fileMentions {
		if existing == path {
			return
		}
	}
	a.fileMentions = append(a.fileMentions, path)
}

func (a *App) fileMentionPathsForDisplay(text string) []string {
	if text == "" || len(a.fileMentions) == 0 {
		return nil
	}
	paths := make([]string, 0, len(a.fileMentions))
	seen := make(map[string]struct{}, len(a.fileMentions))
	for _, path := range a.fileMentions {
		if _, ok := seen[path]; ok || !strings.Contains(text, path) {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) > len(paths[j])
	})
	return paths
}

func (a *App) renderFileMentionChips(view string) string {
	for _, path := range a.fileMentionPathsForDisplay(a.input.Value()) {
		view = strings.ReplaceAll(view, path, a.fileMentionChip(path))
	}
	return view
}

func (a *App) fileMentionChip(path string) string {
	name := filepath.Base(strings.TrimRight(path, "/"))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = path
	}
	icon := lipgloss.NewStyle().
		Foreground(a.theme.Secondary).
		Bold(true).
		Render("▧")
	label := lipgloss.NewStyle().
		Foreground(a.theme.Foreground).
		Render(name)
	return icon + " " + label
}

// dismissFilePopup hides the popup without accepting.
func (a *App) dismissFilePopup() {
	a.filePopup.visible = false
	a.filePopup.matches = nil
	a.filePopup.cursor = 0
}

// filePopupView renders the small completion popup below input.
func (a *App) filePopupView() string {
	if !a.filePopup.visible || len(a.filePopup.matches) == 0 {
		return ""
	}
	width := a.mainBodyWidth()
	if width <= 0 {
		width = a.width
	}
	popupWidth := min(max(1, width-6), 60)
	if popupWidth < 24 {
		popupWidth = max(1, width)
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(a.theme.Secondary).
		Padding(0, 1).
		Width(popupWidth)

	var sb strings.Builder
	headerStyle := lipgloss.NewStyle().
		Foreground(a.theme.Muted).
		Italic(true)
	sb.WriteString(headerStyle.Render("files matching @"+a.filePopup.query) + "\n")

	for i, m := range a.filePopup.matches {
		if i >= filePopupMaxVisible {
			break
		}
		pathStyle := lipgloss.NewStyle().Foreground(a.theme.Primary)
		if i == a.filePopup.cursor {
			pathStyle = pathStyle.
				Background(a.theme.Border).
				Bold(true)
		}
		label := m.Path
		if m.IsDir {
			label += "/"
		}
		sb.WriteString(pathStyle.Render(label) + "\n")
	}

	return border.Render(strings.TrimRight(sb.String(), "\n"))
}

func (a *App) overlayFilePopup(body string) string {
	popup := a.filePopupView()
	if popup == "" {
		return body
	}
	width := a.mainBodyWidth()
	if width <= 0 {
		width = a.width
	}
	if width <= 0 {
		return body
	}
	height := a.bodyHeight()
	if height <= 0 {
		return body
	}

	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	popupLines := strings.Split(strings.TrimRight(popup, "\n"), "\n")
	if len(popupLines) > height {
		popupLines = popupLines[:height]
	}
	top := height - len(popupLines) - 1
	if top < 0 {
		top = 0
	}
	left := 2
	if width < 16 {
		left = 0
	}
	for i, line := range popupLines {
		target := top + i
		if target >= len(lines) {
			break
		}
		lines[target] = overlayFilePopupLine(line, left, width)
	}
	return strings.Join(lines, "\n")
}

func overlayFilePopupLine(line string, left, width int) string {
	if width <= 0 {
		return ""
	}
	if left < 0 {
		left = 0
	}
	if left >= width {
		left = 0
	}
	rendered := strings.Repeat(" ", left) + line
	if lipgloss.Width(rendered) > width {
		return truncateStr(rendered, width)
	}
	return rendered + strings.Repeat(" ", width-lipgloss.Width(rendered))
}

// filePopupMoveDown moves the popup cursor down by one.
func (a *App) filePopupMoveDown() {
	limit := len(a.filePopup.matches)
	if limit > filePopupMaxVisible {
		limit = filePopupMaxVisible
	}
	if a.filePopup.cursor < limit-1 {
		a.filePopup.cursor++
	}
}

// filePopupMoveUp moves the popup cursor up by one.
func (a *App) filePopupMoveUp() {
	if a.filePopup.cursor > 0 {
		a.filePopup.cursor--
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
