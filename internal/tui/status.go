package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders the bottom status line.
type StatusBar struct {
	theme      Theme
	width      int
	busy       bool
	toolName   string
	agentDepth int
	mode       string
}

// NewStatusBar creates a StatusBar with the given theme.
func NewStatusBar(theme Theme) *StatusBar {
	return &StatusBar{theme: theme, mode: "default"}
}

func (s *StatusBar) SetWidth(width int)      { s.width = width }
func (s *StatusBar) SetBusy(busy bool)       { s.busy = busy }
func (s *StatusBar) SetTool(name string)     { s.toolName = name }
func (s *StatusBar) SetAgentDepth(depth int) { s.agentDepth = depth }
func (s *StatusBar) SetMode(mode string)     { s.mode = mode }

func (s *StatusBar) View() string {
	modeStyle := lipgloss.NewStyle().
		Background(s.theme.Primary).
		Foreground(lipgloss.Color("#000")).
		Padding(0, 1).
		Bold(true)

	var left string
	if s.busy {
		spinner := lipgloss.NewStyle().Foreground(s.theme.Warning).Render("● ")
		toolInfo := ""
		if s.toolName != "" {
			toolInfo = lipgloss.NewStyle().Foreground(s.theme.Muted).Render(s.toolName)
		}
		agentInfo := ""
		if s.agentDepth > 0 {
			agentInfo = lipgloss.NewStyle().Foreground(s.theme.Secondary).
				Render(fmt.Sprintf(" agent[%d]", s.agentDepth))
		}
		left = spinner + toolInfo + agentInfo
	} else {
		left = lipgloss.NewStyle().Foreground(s.theme.Success).Render("● ready")
	}

	right := modeStyle.Render(s.mode)

	hints := lipgloss.NewStyle().Foreground(s.theme.Muted).
		Render("  Ctrl+D send  Esc cancel")

	gap := s.width - lipgloss.Width(left) - lipgloss.Width(right) - lipgloss.Width(hints)
	if gap < 1 {
		gap = 1
	}

	return left + fmt.Sprintf("%*s", gap, "") + hints + "  " + right
}
