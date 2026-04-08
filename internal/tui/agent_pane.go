package tui

import (
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/charmbracelet/lipgloss"
)

const agentPaneOutputLines = 50

// wsAgentPane renders a single agent within the workspace view.
type wsAgentPane struct {
	Role     string
	Backend  string
	Activity workspace.ActivityState
	Branch   string
	PRID     int
	CIStatus workspace.CICheckStatus
	Priority workspace.AttentionPriority
	CostUSD  float64
	Turns    int
	Lines    []string // rolling output buffer
}

// AppendOutput adds a line to the pane's rolling buffer.
func (p *wsAgentPane) AppendOutput(line string) {
	p.Lines = append(p.Lines, line)
	if len(p.Lines) > agentPaneOutputLines {
		p.Lines = p.Lines[len(p.Lines)-agentPaneOutputLines:]
	}
}

// Render draws the agent pane as a bordered box.
func (p *wsAgentPane) Render(
	theme Theme,
	width int,
	height int,
) string {
	borderColor := attentionColor(p.Priority)

	// Header: role badge + backend + activity
	badge := lipgloss.NewStyle().
		Background(borderColor).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1).
		Render(strings.ToUpper(p.Role))

	backend := lipgloss.NewStyle().
		Foreground(theme.Muted).
		Render(fmt.Sprintf("(%s)", p.Backend))

	actIcon := activityIcon(p.Activity)
	header := fmt.Sprintf("%s %s %s", badge, backend, actIcon)

	// Status line: PR + CI + cost
	statusParts := []string{
		fmt.Sprintf("turns:%d", p.Turns),
	}
	if p.CostUSD > 0 {
		statusParts = append(statusParts,
			fmt.Sprintf("$%.2f", p.CostUSD))
	}
	if p.PRID > 0 {
		statusParts = append(statusParts,
			fmt.Sprintf("PR#%d", p.PRID))
	}
	ci := string(p.CIStatus)
	if ci != "" && ci != "unknown" {
		statusParts = append(statusParts,
			fmt.Sprintf("CI:%s", ci))
	}
	statusLine := lipgloss.NewStyle().
		Foreground(theme.Muted).
		Render(strings.Join(statusParts, "  "))

	// Body: last N visible lines
	bodyHeight := height - 3 // header + status + border
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	visible := p.Lines
	if len(visible) > bodyHeight {
		visible = visible[len(visible)-bodyHeight:]
	}
	var body []string
	maxLineWidth := width - 4
	if maxLineWidth < 3 {
		maxLineWidth = 3
	}
	for _, l := range visible {
		runes := []rune(l)
		if len(runes) > maxLineWidth {
			cut := maxLineWidth - 3
			if cut < 0 {
				cut = 0
			}
			l = string(runes[:cut]) + "..."
		}
		body = append(body, l)
	}
	for len(body) < bodyHeight {
		body = append(body, "")
	}

	content := header + "\n" + statusLine + "\n" +
		strings.Join(body, "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Render(content)
}

// attentionColor maps priority to a theme-aware lipgloss color.
// Uses AdaptiveColor for graceful degradation on 16-color terminals.
func attentionColor(p workspace.AttentionPriority) lipgloss.TerminalColor {
	switch p {
	case workspace.AttentionYellow:
		return lipgloss.AdaptiveColor{Light: "#B8860B", Dark: "#FFD700"}
	case workspace.AttentionOrange:
		return lipgloss.AdaptiveColor{Light: "#CC5500", Dark: "#FF8C00"}
	case workspace.AttentionRed:
		return lipgloss.AdaptiveColor{Light: "#CC0000", Dark: "#FF4444"}
	default:
		return lipgloss.AdaptiveColor{Light: "#228B22", Dark: "#00CC00"}
	}
}

// activityIcon returns a compact icon for the agent's activity state.
func activityIcon(s workspace.ActivityState) string {
	switch s {
	case workspace.ActivityActive:
		return "[active]"
	case workspace.ActivityReady:
		return "[ready]"
	case workspace.ActivityIdle:
		return "[idle]"
	case workspace.ActivitySpawning:
		return "[spawning]"
	case workspace.ActivityWaitInput:
		return "[waiting]"
	case workspace.ActivityBlocked:
		return "[BLOCKED]"
	case workspace.ActivityExited:
		return "[exited]"
	default:
		return "[--]"
	}
}
