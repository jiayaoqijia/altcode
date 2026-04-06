package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// hudState holds all metrics for the HUD display.
type hudState struct {
	// Context
	ContextTokens int
	ContextLimit  int // e.g. 128000 for GPT-4

	// Session
	SessionStart time.Time

	// Git
	GitBranch  string
	GitProject string

	// Tool counts
	ToolCounts map[string]int // tool name → call count

	// Tasks
	TasksTotal int
	TasksDone  int
}

// contextPercent returns 0-100 context usage.
func (h *hudState) contextPercent() int {
	if h.ContextLimit <= 0 {
		return 0
	}
	p := h.ContextTokens * 100 / h.ContextLimit
	if p > 100 {
		return 100
	}
	return p
}

// renderHUD builds the full 2-line HUD matching codex-hud/claude-hud style.
// Line 1: [mode] model │ project@branch │ tools │ session time
// Line 2: context bar [████████░░] 45% │ 12.3K/128K tokens │ $0.0123
func renderHUD(h hudState, info statusBarInfo, theme Theme, width int, vimMode bool) string {
	// ── LINE 1 ──
	modeStyle := lipgloss.NewStyle().
		Background(theme.Primary).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1)
	mode := modeStyle.Render("altcode")
	if vimMode {
		mode = modeStyle.Background(theme.Warning).Render("NORMAL")
	}

	sep := lipgloss.NewStyle().Foreground(theme.Border).Render(" │ ")
	dim := lipgloss.NewStyle().Foreground(theme.Muted)
	bright := lipgloss.NewStyle().Foreground(theme.Foreground)

	parts1 := []string{mode}

	// Model
	if info.Model != "" {
		short := info.Model
		if i := strings.LastIndex(short, "/"); i >= 0 {
			short = short[i+1:]
		}
		parts1 = append(parts1, bright.Bold(true).Render(short))
	}

	// Git context
	if h.GitProject != "" {
		git := dim.Render(h.GitProject)
		if h.GitBranch != "" {
			git += dim.Render("@") + lipgloss.NewStyle().Foreground(theme.Secondary).Render(h.GitBranch)
		}
		parts1 = append(parts1, git)
	}

	// Tool activity with counts
	if info.ToolActive != "" {
		parts1 = append(parts1, lipgloss.NewStyle().Foreground(theme.Warning).Bold(true).Render("⟳ "+info.ToolActive))
	} else if len(h.ToolCounts) > 0 {
		toolStr := renderToolCounts(h.ToolCounts, theme)
		parts1 = append(parts1, toolStr)
	}

	// Session duration
	if !h.SessionStart.IsZero() {
		dur := time.Since(h.SessionStart).Truncate(time.Second)
		parts1 = append(parts1, dim.Render(formatDuration(dur)))
	}

	line1 := strings.Join(parts1, sep)

	// ── LINE 2 ──
	parts2 := []string{}

	// Context bar
	if h.ContextLimit > 0 {
		bar := renderContextBar(h.contextPercent(), theme, 20)
		pct := fmt.Sprintf("%d%%", h.contextPercent())
		tokens := fmt.Sprintf("%s/%s",
			formatTokens(h.ContextTokens), formatTokens(h.ContextLimit))
		parts2 = append(parts2, bar+" "+dim.Render(pct)+" "+dim.Render(tokens))
	} else if info.TokensIn+info.TokensOut > 0 {
		parts2 = append(parts2, dim.Render(fmt.Sprintf("%s in / %s out",
			formatTokens(info.TokensIn), formatTokens(info.TokensOut))))
	}

	// Cost
	if info.CostUSD > 0 {
		parts2 = append(parts2, lipgloss.NewStyle().Foreground(theme.Success).Render(
			fmt.Sprintf("$%.4f", info.CostUSD)))
	}

	// Tasks
	if h.TasksTotal > 0 {
		taskColor := theme.Muted
		if h.TasksDone == h.TasksTotal {
			taskColor = theme.Success
		}
		parts2 = append(parts2, lipgloss.NewStyle().Foreground(taskColor).Render(
			fmt.Sprintf("tasks %d/%d", h.TasksDone, h.TasksTotal)))
	}

	line2 := ""
	if len(parts2) > 0 {
		line2 = "  " + strings.Join(parts2, sep)
	}

	// Pad both lines to width
	pad := lipgloss.NewStyle().Width(width).Background(theme.HeaderBg)
	result := pad.Render(line1)
	if line2 != "" {
		result += "\n" + pad.Render(line2)
	}
	return result
}

// renderContextBar returns a visual bar like [████████░░░░] with color gradient.
func renderContextBar(pct int, theme Theme, barWidth int) string {
	filled := pct * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	// Color gradient: green → yellow → red
	var fillColor lipgloss.Color
	switch {
	case pct >= 90:
		fillColor = theme.Error
	case pct >= 70:
		fillColor = theme.Warning
	default:
		fillColor = theme.Success
	}

	fill := lipgloss.NewStyle().Foreground(fillColor).Render(strings.Repeat("█", filled))
	emp := lipgloss.NewStyle().Foreground(theme.Border).Render(strings.Repeat("░", empty))

	return lipgloss.NewStyle().Foreground(theme.Muted).Render("[") +
		fill + emp +
		lipgloss.NewStyle().Foreground(theme.Muted).Render("]")
}

// renderToolCounts formats tool usage as "✓ read ×3 ✓ edit ×1"
func renderToolCounts(counts map[string]int, theme Theme) string {
	if len(counts) == 0 {
		return ""
	}
	check := lipgloss.NewStyle().Foreground(theme.Success).Render("✓")
	name := lipgloss.NewStyle().Foreground(theme.Muted)
	cnt := lipgloss.NewStyle().Foreground(theme.Foreground)

	var parts []string
	for tool, n := range counts {
		parts = append(parts, fmt.Sprintf("%s %s %s", check, name.Render(tool), cnt.Render(fmt.Sprintf("×%d", n))))
	}
	return strings.Join(parts, " ")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// detectGitInfo reads current branch and project name.
func detectGitInfo() (project, branch string) {
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		full := strings.TrimSpace(string(out))
		if i := strings.LastIndex(full, "/"); i >= 0 {
			project = full[i+1:]
		} else {
			project = full
		}
	}
	if out, err := exec.Command("git", "branch", "--show-current").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	return
}
