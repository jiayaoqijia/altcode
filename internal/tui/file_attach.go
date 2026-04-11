package tui

import (
	"strings"

	"github.com/altcode-ai/altcode/internal/completions"
	"github.com/charmbracelet/lipgloss"
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
	a.filePopup.query = query
	a.filePopup.matches = completions.Complete(root, query, 10)
	a.filePopup.visible = len(a.filePopup.matches) > 0
	a.filePopup.cursor = 0
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
	a.filePopup.visible = false
	a.filePopup.matches = nil
	a.filePopup.cursor = 0
	return true
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

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(a.theme.Secondary).
		Padding(0, 1).
		Width(min(a.width-4, 60))

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

	return border.Render(sb.String())
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
