package tui

import (
	"fmt"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/workspace"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const agentPaneOutputLines = 200

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
	Lines          []string // rolling output buffer
	Focused        bool     // true when this pane has keyboard focus
	scrollOffset   int      // 0 = bottom (latest), >0 = scrolled up
	lastBodyHeight int      // visible body lines from the last Render call
}

// AppendOutput adds a line to the pane's rolling buffer.
//
// When the buffer is full and truncates, reset scrollOffset to 0 so
// the user (who may have scrolled up to read earlier output) doesn't
// end up looking at a negative position past EOF. The old content
// they were reading has been evicted anyway — pinning them to the
// latest is the least-wrong behavior.
func (p *wsAgentPane) AppendOutput(line string) {
	p.Lines = append(p.Lines, line)
	if len(p.Lines) > agentPaneOutputLines {
		p.Lines = p.Lines[len(p.Lines)-agentPaneOutputLines:]
		p.scrollOffset = 0
	}
}

// Render draws the agent pane as a bordered box.
func (p *wsAgentPane) Render(
	theme Theme,
	width int,
	height int,
) string {
	borderColor := attentionColor(p.Priority)
	if p.Focused {
		// Focused pane gets a bright white border for clear visual distinction
		borderColor = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}
	}

	// Header: focus indicator + role badge + backend + activity
	focusMarker := ""
	if p.Focused {
		focusMarker = "▸ "
	}
	badge := lipgloss.NewStyle().
		Background(borderColor).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1).
		Render(focusMarker + strings.ToUpper(p.Role))

	backend := lipgloss.NewStyle().
		Foreground(theme.Muted).
		Render(fmt.Sprintf("(%s)", p.Backend))

	actIcon := activityIcon(p.Activity)
	header := fmt.Sprintf("%s %s %s", badge, backend, actIcon)

	// Branch line (if available). Truncation walks runes (not bytes)
	// so a branch like `feature/測試` doesn't get sliced mid-rune
	// and produce invalid UTF-8 in the rendered pane.
	branchLine := ""
	if p.Branch != "" {
		runes := []rune(p.Branch)
		shortBranch := p.Branch
		if len(runes) > width-6 {
			tail := width - 9
			if tail < 1 {
				tail = 1
			}
			if tail > len(runes) {
				tail = len(runes)
			}
			shortBranch = "..." + string(runes[len(runes)-tail:])
		}
		branchLine = lipgloss.NewStyle().
			Foreground(theme.Secondary).
			Render("⎇ " + shortBranch)
	}

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
	overhead := 3 // header + status + border
	if branchLine != "" {
		overhead = 4 // + branch line
	}
	bodyHeight := height - overhead
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	// Stash the body height so ScrollPane can clamp to the real
	// visible window instead of a hardcoded magic number.
	p.lastBodyHeight = bodyHeight
	// Apply scroll offset: 0 = show latest lines, >0 = scrolled up
	visible := p.Lines
	if p.scrollOffset > 0 && len(visible) > bodyHeight {
		end := len(visible) - p.scrollOffset
		if end < bodyHeight {
			end = bodyHeight
		}
		start := end - bodyHeight
		if start < 0 {
			start = 0
		}
		visible = visible[start:end]
	} else if len(visible) > bodyHeight {
		visible = visible[len(visible)-bodyHeight:]
	}
	var body []string
	maxLineWidth := width - 4
	if maxLineWidth < 3 {
		maxLineWidth = 3
	}
	for _, l := range visible {
		// Truncate by display columns. The previous version compared
		// rune count to width and silently broke ANSI escapes mid-
		// sequence when a tool piped colored output (`ls --color`,
		// `cargo build`, `grep --color=always`). lipgloss.Width is
		// ANSI-aware and counts visible columns, and ansi.Truncate
		// preserves the escape state.
		if lipgloss.Width(l) > maxLineWidth {
			cut := maxLineWidth - 3
			if cut < 0 {
				cut = 0
			}
			l = ansi.Truncate(l, cut, "") + "..."
		}
		body = append(body, l)
	}
	for len(body) < bodyHeight {
		body = append(body, "")
	}

	contentParts := []string{header, statusLine}
	if branchLine != "" {
		contentParts = append(contentParts, branchLine)
	}
	contentParts = append(contentParts, strings.Join(body, "\n"))
	content := strings.Join(contentParts, "\n")

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

// activityIcon returns a user-friendly label for the agent's state.
// Avoids implementation jargon — uses verbs users understand.
func activityIcon(s workspace.ActivityState) string {
	switch s {
	case workspace.ActivityActive:
		return "working"
	case workspace.ActivityReady:
		return "ready"
	case workspace.ActivityIdle:
		return "idle"
	case workspace.ActivitySpawning:
		return "starting…"
	case workspace.ActivityWaitInput:
		return "needs input"
	case workspace.ActivityBlocked:
		return "STUCK"
	case workspace.ActivityExited:
		return "done"
	default:
		return "—"
	}
}
