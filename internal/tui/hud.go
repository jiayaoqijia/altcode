package tui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
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
	SessionName  string // e.g. "starry-waddling-tulip"

	// Git
	GitBranch  string
	GitProject string
	GitDirty   bool // uncommitted changes

	// Config counts (Claude Code parity)
	ClaudeMDCount int // number of CLAUDE.md files loaded
	MCPCount      int // number of MCP servers
	HooksCount    int // number of hooks

	// Tool counts
	ToolCounts map[string]int // tool name → call count

	// Tasks
	TasksTotal     int
	TasksDone      int
	ActiveTaskName string // currently in-progress task description
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
func renderHUD(h hudState, info statusBarInfo, theme Theme, width int, vimMode bool, spinnerView string) string {
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

	parts1 := []string{}

	// Model in [brackets] — CC style: [Opus 4.6]
	if info.Model != "" {
		short := info.Model
		if i := strings.LastIndex(short, "/"); i >= 0 {
			short = short[i+1:]
		}
		modelBadge := lipgloss.NewStyle().
			Foreground(theme.Secondary).Bold(true).
			Render("[" + short + "]")
		parts1 = append(parts1, modelBadge)
	} else {
		parts1 = append(parts1, mode)
	}

	// Git context (Claude Code style: project git:(branch*))
	if h.GitProject != "" {
		branchDisplay := h.GitBranch
		if h.GitDirty && branchDisplay != "" {
			branchDisplay += "*"
		}
		git := lipgloss.NewStyle().Foreground(theme.Warning).Render(h.GitProject)
		if branchDisplay != "" {
			git += " " + dim.Render("git:(") +
				lipgloss.NewStyle().Foreground(theme.Secondary).Render(branchDisplay) +
				dim.Render(")")
		}
		parts1 = append(parts1, git)
	}

	// Session name slug
	if h.SessionName != "" && width >= 100 {
		parts1 = append(parts1, dim.Render(h.SessionName))
	}

	// Tool activity: CC-style single line
	// ◐ Bash: go test | ✓ Read ×12 | ✓ Edit ×3
	{
		var toolParts []string
		if info.ToolActive != "" {
			spinnerText := spinnerView + " "
			toolLabel := info.ToolActive
			if width < 80 && len(toolLabel) > 15 {
				toolLabel = toolLabel[:12] + "..."
			}
			toolParts = append(toolParts,
				lipgloss.NewStyle().Foreground(theme.Primary).Bold(true).Render(spinnerText+toolLabel))
		}
		if len(h.ToolCounts) > 0 && width >= 60 {
			toolParts = append(toolParts, renderToolCounts(h.ToolCounts, theme))
		}
		if len(toolParts) > 0 {
			parts1 = append(parts1, strings.Join(toolParts, sep))
		}
	}

	// Session duration with emoji — CC style: ⏱️ 2m33s
	if !h.SessionStart.IsZero() && width >= 50 {
		dur := time.Since(h.SessionStart).Truncate(time.Second)
		parts1 = append(parts1, dim.Render("⏱️  "+formatDuration(dur)))
	}

	line1 := strings.Join(parts1, sep)

	// ── LINE 2 ──
	parts2 := []string{}

	// Context bar — adaptive width
	if h.ContextLimit > 0 {
		barWidth := 20
		if width < 60 {
			barWidth = 10 // shorter bar on narrow terminals
		}
		bar := renderContextBar(h.contextPercent(), theme, barWidth)
		pct := fmt.Sprintf("%d%%", h.contextPercent())
		if width >= 80 {
			tokens := fmt.Sprintf("%s/%s",
				formatTokens(h.ContextTokens), formatTokens(h.ContextLimit))
			parts2 = append(parts2, bar+" "+dim.Render(pct)+" "+dim.Render(tokens))
		} else {
			parts2 = append(parts2, bar+" "+dim.Render(pct))
		}
	} else if info.TokensIn+info.TokensOut > 0 {
		parts2 = append(parts2, dim.Render(fmt.Sprintf("%s in / %s out",
			formatTokens(info.TokensIn), formatTokens(info.TokensOut))))
	}

	// Config counts (Claude Code parity: "2 CLAUDE.md | 4 MCPs | 3 hooks")
	if h.ClaudeMDCount > 0 || h.MCPCount > 0 || h.HooksCount > 0 {
		var cfgParts []string
		if h.ClaudeMDCount > 0 {
			cfgParts = append(cfgParts, fmt.Sprintf("%d CLAUDE.md", h.ClaudeMDCount))
		}
		if h.MCPCount > 0 {
			cfgParts = append(cfgParts, fmt.Sprintf("%d MCPs", h.MCPCount))
		}
		if h.HooksCount > 0 {
			cfgParts = append(cfgParts, fmt.Sprintf("%d hooks", h.HooksCount))
		}
		parts2 = append(parts2, dim.Render(strings.Join(cfgParts, " | ")))
	}

	// Cost
	if info.CostUSD > 0 {
		parts2 = append(parts2, lipgloss.NewStyle().Foreground(theme.Success).Render(
			fmt.Sprintf("$%.4f", info.CostUSD)))
	}

	// Tasks (Claude Code style: ▸ active task (completed/total))
	if h.TasksTotal > 0 {
		if h.TasksDone == h.TasksTotal {
			parts2 = append(parts2, lipgloss.NewStyle().Foreground(theme.Success).Render(
				fmt.Sprintf("✓ All tasks complete (%d/%d)", h.TasksDone, h.TasksTotal)))
		} else if h.ActiveTaskName != "" {
			taskName := h.ActiveTaskName
			if len(taskName) > 50 {
				taskName = taskName[:47] + "..."
			}
			parts2 = append(parts2, lipgloss.NewStyle().Foreground(theme.Warning).Render("▸ ")+
				dim.Render(taskName)+" "+
				dim.Render(fmt.Sprintf("(%d/%d)", h.TasksDone, h.TasksTotal)))
		} else {
			parts2 = append(parts2, dim.Render(
				fmt.Sprintf("tasks %d/%d", h.TasksDone, h.TasksTotal)))
		}
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

// renderToolCounts formats tool usage as "✓ Bash ×3 ✓ Edit ×1" (CC style, capitalized).
func renderToolCounts(counts map[string]int, theme Theme) string {
	if len(counts) == 0 {
		return ""
	}
	check := lipgloss.NewStyle().Foreground(theme.Success).Render("✓")
	nameStyle := lipgloss.NewStyle().Foreground(theme.Muted)
	cnt := lipgloss.NewStyle().Foreground(theme.Foreground)

	// Sort by count descending, show top 4 like CC
	type toolCount struct {
		name  string
		count int
	}
	var sorted []toolCount
	for tool, n := range counts {
		sorted = append(sorted, toolCount{tool, n})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})
	if len(sorted) > 4 {
		sorted = sorted[:4]
	}

	var parts []string
	for _, tc := range sorted {
		// Capitalize first letter to match CC style
		displayName := capitalizeFirst(tc.name)
		parts = append(parts, fmt.Sprintf("%s %s %s",
			check, nameStyle.Render(displayName), cnt.Render(fmt.Sprintf("×%d", tc.count))))
	}
	return strings.Join(parts, " ")
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 32
	}
	return string(r)
}

// generateSessionSlug creates a CC-style session name: adjective-verb-noun.
func generateSessionSlug() string {
	adjectives := []string{
		"bright", "calm", "daring", "eager", "fair",
		"gentle", "happy", "keen", "lively", "merry",
		"nimble", "plain", "quick", "sharp", "swift",
		"warm", "witty", "bold", "crisp", "fresh",
	}
	verbs := []string{
		"dancing", "flying", "gliding", "jumping", "racing",
		"running", "sailing", "singing", "soaring", "wading",
		"walking", "waving", "coding", "building", "crafting",
		"forging", "making", "solving", "testing", "writing",
	}
	nouns := []string{
		"brook", "cloud", "crane", "dawn", "eagle",
		"flame", "grove", "hawk", "iris", "jade",
		"lark", "maple", "oak", "pearl", "pine",
		"reed", "sage", "tulip", "vine", "wren",
	}
	now := time.Now().UnixNano()
	a := adjectives[int(now/7)%len(adjectives)]
	v := verbs[int(now/13)%len(verbs)]
	n := nouns[int(now/17)%len(nouns)]
	return a + "-" + v + "-" + n
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

// detectGitInfo reads current branch, project name, and dirty status.
func detectGitInfo() (project, branch string, dirty bool) {
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
	if out, err := exec.Command("git", "status", "--porcelain").Output(); err == nil {
		dirty = strings.TrimSpace(string(out)) != ""
	}
	return
}

// detectTerminal returns info about the terminal environment.
func detectTerminal() string {
	if os.Getenv("TMUX") != "" {
		return "tmux"
	}
	if os.Getenv("ZELLIJ") != "" {
		return "zellij"
	}
	if os.Getenv("STY") != "" {
		return "screen"
	}
	term := os.Getenv("TERM_PROGRAM")
	if term != "" {
		return term
	}
	return os.Getenv("TERM")
}
