package tui

import (
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const switcherMaxVisible = 10

// SessionEntry is a display row in the session switcher.
type SessionEntry struct {
	ID    string
	Title string
	Model string
	Date  string
}

// SessionSwitcher renders a filterable list of recent sessions.
type SessionSwitcher struct {
	theme    Theme
	width    int
	visible  bool
	cursor   int
	input    textinput.Model
	entries  []SessionEntry
	filtered []SessionEntry
}

// NewSessionSwitcher creates an empty session switcher.
func NewSessionSwitcher(theme Theme) *SessionSwitcher {
	ti := textinput.New()
	ti.Placeholder = "Filter sessions..."
	ti.CharLimit = 50

	return &SessionSwitcher{
		theme: theme,
		input: ti,
	}
}

// Load populates the switcher from the store.
func (s *SessionSwitcher) Load(db *store.DB) error {
	if db == nil {
		s.entries = nil
		s.filtered = nil
		return nil
	}
	sessions, err := db.ListSessions()
	if err != nil {
		return err
	}
	limit := len(sessions)
	if limit > 20 {
		limit = 20
	}
	entries := make([]SessionEntry, 0, limit)
	for _, sess := range sessions[:limit] {
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		entries = append(entries, SessionEntry{
			ID:    sess.ID,
			Title: title,
			Model: sess.Model,
			Date:  sess.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	s.entries = entries
	s.filtered = entries
	return nil
}

// Show opens the switcher and resets state.
func (s *SessionSwitcher) Show() {
	s.visible = true
	s.cursor = 0
	s.input.Focus()
	s.input.Reset()
	s.filtered = s.entries
}

// Hide closes the switcher.
func (s *SessionSwitcher) Hide() {
	s.visible = false
	s.input.Blur()
}

// IsVisible reports whether the switcher is open.
func (s *SessionSwitcher) IsVisible() bool { return s.visible }

// SetWidth sets the render width.
func (s *SessionSwitcher) SetWidth(w int) { s.width = w }

// SelectedID returns the session ID under the cursor.
func (s *SessionSwitcher) SelectedID() string {
	if s.cursor < 0 || s.cursor >= len(s.filtered) {
		return ""
	}
	return s.filtered[s.cursor].ID
}

// UpdateKey handles a key press. Returns true if consumed.
func (s *SessionSwitcher) UpdateKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up":
		if s.cursor > 0 {
			s.cursor--
		}
		return true
	case "down":
		limit := len(s.filtered)
		if limit > switcherMaxVisible {
			limit = switcherMaxVisible
		}
		if s.cursor < limit-1 {
			s.cursor++
		}
		return true
	case "esc":
		s.Hide()
		return true
	case "enter":
		return true // caller reads SelectedID()
	default:
		s.input, _ = s.input.Update(msg)
		s.filter(s.input.Value())
		return true
	}
}

func (s *SessionSwitcher) filter(query string) {
	if query == "" {
		s.filtered = s.entries
		s.cursor = 0
		return
	}
	q := strings.ToLower(query)
	var out []SessionEntry
	for _, e := range s.entries {
		if strings.Contains(strings.ToLower(e.Title), q) ||
			strings.Contains(strings.ToLower(e.ID), q) ||
			strings.Contains(strings.ToLower(e.Model), q) {
			out = append(out, e)
		}
	}
	s.filtered = out
	if s.cursor >= len(s.filtered) {
		s.cursor = max(0, len(s.filtered)-1)
	}
}

// View renders the switcher overlay.
func (s *SessionSwitcher) View() string {
	if !s.visible {
		return ""
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.theme.Secondary).
		Padding(0, 1).
		Width(s.width - 4)

	var sb strings.Builder
	sb.WriteString(s.input.View())
	sb.WriteByte('\n')

	if len(s.filtered) == 0 {
		muted := lipgloss.NewStyle().Foreground(s.theme.Muted)
		sb.WriteString(muted.Render("No sessions found.") + "\n")
		return border.Render(sb.String())
	}

	for i, e := range s.filtered {
		if i >= switcherMaxVisible {
			break
		}
		idStr := e.ID
		if len(idStr) > 8 {
			idStr = idStr[:8]
		}
		row := fmt.Sprintf("%-8s  %-16s  %-20s  %s",
			idStr, e.Date, truncateStr(e.Title, 20), e.Model)

		style := lipgloss.NewStyle().Foreground(s.theme.Foreground)
		if i == s.cursor {
			style = style.
				Background(s.theme.Border).
				Foreground(s.theme.Primary).
				Bold(true)
		}
		sb.WriteString(style.Render(row) + "\n")
	}

	return border.Render(sb.String())
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "~"
}
