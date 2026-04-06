package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// fileChange tracks a file modified during the session.
type fileChange struct {
	path string
	adds int
	dels int
}

// sidebar renders the right sidebar showing session info and file changes.
type sidebar struct {
	theme   Theme
	width   int
	height  int
	model   string
	session string
	files   []fileChange
}

func newSidebar(theme Theme) *sidebar {
	return &sidebar{theme: theme}
}

func (s *sidebar) SetSize(w, h int) {
	s.width = w
	s.height = h
}

func (s *sidebar) AddFile(path string, adds, dels int) {
	// Update existing or append
	for i, f := range s.files {
		if f.path == path {
			s.files[i].adds += adds
			s.files[i].dels += dels
			return
		}
	}
	s.files = append(s.files, fileChange{path: path, adds: adds, dels: dels})
}

func (s *sidebar) View() string {
	if s.width < 20 {
		return ""
	}

	t := s.theme
	w := s.width - 2 // padding

	// Title
	title := lipgloss.NewStyle().
		Foreground(t.Primary).
		Bold(true).
		Render("Files")

	sep := lipgloss.NewStyle().
		Foreground(t.Border).
		Render(strings.Repeat("─", w))

	var sb strings.Builder
	sb.WriteString(title + "\n")
	sb.WriteString(sep + "\n")

	if len(s.files) == 0 {
		sb.WriteString(lipgloss.NewStyle().
			Foreground(t.Muted).
			Italic(true).
			Render("no changes yet"))
		sb.WriteString("\n")
	} else {
		maxFiles := s.height - 4
		if maxFiles < 3 {
			maxFiles = 3
		}
		for i, f := range s.files {
			if i >= maxFiles {
				sb.WriteString(lipgloss.NewStyle().
					Foreground(t.Muted).
					Render(fmt.Sprintf("  +%d more", len(s.files)-maxFiles)))
				sb.WriteString("\n")
				break
			}
			// Truncate path
			name := f.path
			maxPath := w - 12
			if maxPath < 10 {
				maxPath = 10
			}
			if len(name) > maxPath {
				name = "…" + name[len(name)-maxPath+1:]
			}

			nameStyle := lipgloss.NewStyle().Foreground(t.Foreground)
			addStyle := lipgloss.NewStyle().Foreground(t.DiffAdd)
			delStyle := lipgloss.NewStyle().Foreground(t.DiffDel)

			line := nameStyle.Render(name)
			if f.adds > 0 || f.dels > 0 {
				diff := ""
				if f.adds > 0 {
					diff += addStyle.Render(fmt.Sprintf("+%d", f.adds))
				}
				if f.dels > 0 {
					if diff != "" {
						diff += " "
					}
					diff += delStyle.Render(fmt.Sprintf("-%d", f.dels))
				}
				line += " " + diff
			}
			sb.WriteString(line + "\n")
		}
	}

	// Border on the left side
	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Border).
		PaddingLeft(1).
		Width(s.width).
		Height(s.height).
		Render(sb.String())
}
