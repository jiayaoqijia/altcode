package tui

import (
	crand "crypto/rand"
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
	ContextTokens int64
	ContextLimit  int64 // e.g. 128000 for GPT-4

	// CachedTokens is the most-recent turn's prompt tokens served from
	// the provider's prefix cache (cached_tokens for OpenAI/OpenRouter/
	// DeepSeek, cache_read_input_tokens for Anthropic). When > 0 the
	// HUD renders a "cache N%" chip — useful telemetry for users
	// tuning long-running deepseek sessions.
	CachedTokens int64

	// QueueDepth is the number of prompts the user has typed ahead
	// while the current turn is running (FIFO drained at onDone).
	// Surfaced as a "[N queued]" chip in the HUD so users can SEE
	// at a glance that their typed-ahead input was buffered, not
	// silently lost. DeepSeek-TUI parity for visible queue depth.
	QueueDepth int

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
	return int(p)
}

// renderHUD builds the full 2-line HUD matching codex-hud/claude-hud style.
// Line 1: [mode] model │ project@branch │ tools │ session time
// Line 2: context bar [████████░░] 45% │ 12.3K/128K tokens │ $0.0123
func renderHUD(h hudState, info statusBarInfo, theme Theme, width int, vimMode bool, spinnerView string) string {
	// Narrow terminal: single compact line
	if width < 80 {
		return renderCompactHUD(h, info, theme, width, spinnerView)
	}
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
		parts1 = append(parts1, dim.Render(formatDuration(dur)))
	}

	line1 := strings.Join(parts1, sep)

	// ── LINE 2: left=progress, right=resources ──
	// Left side: task progress
	var leftParts []string

	// Right side: context + config + cost (resource meters)
	var rightParts []string

	// Context bar (right)
	if h.ContextLimit > 0 {
		barWidth := 12
		if width >= 80 {
			barWidth = 16
		}
		bar := renderContextBar(h.contextPercent(), theme, barWidth)
		pct := fmt.Sprintf("%d%%", h.contextPercent())
		if width >= 100 {
			tokens := fmt.Sprintf("%s/%s",
				formatTokens(h.ContextTokens), formatTokens(h.ContextLimit))
			rightParts = append(rightParts, bar+" "+dim.Render(pct)+" "+dim.Render(tokens))
		} else {
			rightParts = append(rightParts, bar+" "+dim.Render(pct))
		}
	}
	if total := info.TokensIn + info.TokensOut; total > 0 {
		rightParts = append(rightParts, dim.Render("tok "+formatTokens(total)))
	}

	// Cache-hit chip (right): show prefix-cache effectiveness when the
	// last turn benefited from the provider's prompt cache. Hidden
	// when cached==0 to keep the HUD quiet on cache-cold turns.
	// DeepSeek-TUI #396 parity.
	if h.CachedTokens > 0 && h.ContextTokens > 0 {
		cachePct := h.CachedTokens * 100 / h.ContextTokens
		if cachePct > 100 {
			cachePct = 100
		}
		// Color the chip green when cache hit is high (80%+), yellow
		// for moderate (40-79%), muted for low (<40%) — quick visual
		// gauge of whether the prompt is reusing prior context.
		chipColor := theme.Muted
		if cachePct >= 80 {
			chipColor = theme.Success
		} else if cachePct >= 40 {
			chipColor = theme.Warning
		}
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(chipColor).
				Render(fmt.Sprintf("cache %d%%", cachePct)))
	}

	// Queue depth chip (right): only render when prompts are
	// type-ahead buffered. Warning color makes the chip pop so users
	// SEE that their input was queued, not silently lost — fixes the
	// "where did my Enter go" mental-model gap. DS-TUI parity.
	//
	// Glyph note: ▶ (U+25B6 BLACK RIGHT-POINTING TRIANGLE) is a
	// universally-rendered Unicode geometric shape — no font fallback
	// risk. Earlier ⏵ (U+23F5) had degraded glyph rendering in some
	// terminal fonts (round-4 review).
	if h.QueueDepth > 0 {
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(theme.Warning).Bold(true).
				Render(fmt.Sprintf("▶ %d queued", h.QueueDepth)))
	}

	// Config counts (right)
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
		rightParts = append(rightParts, dim.Render(strings.Join(cfgParts, " | ")))
	}

	// Cost (right)
	if info.CostUSD > 0 {
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(theme.Success).Render(
			fmt.Sprintf("$%.4f", info.CostUSD)))
	}

	// Merge into parts2 — left progress first, then right resources
	// Tasks go on left side (progress)
	if h.TasksTotal > 0 {
		if h.TasksDone == h.TasksTotal {
			leftParts = append(leftParts, lipgloss.NewStyle().Foreground(theme.Success).Render(
				fmt.Sprintf("✓ All tasks (%d/%d)", h.TasksDone, h.TasksTotal)))
		} else if h.ActiveTaskName != "" {
			taskName := h.ActiveTaskName
			if len(taskName) > 40 {
				taskName = taskName[:37] + "..."
			}
			leftParts = append(leftParts, lipgloss.NewStyle().Foreground(theme.Warning).Render("▸ ")+
				dim.Render(taskName)+" "+
				dim.Render(fmt.Sprintf("(%d/%d)", h.TasksDone, h.TasksTotal)))
		} else {
			leftParts = append(leftParts, dim.Render(
				fmt.Sprintf("tasks %d/%d", h.TasksDone, h.TasksTotal)))
		}
	}

	// Build line 2: left progress │ right resources. When both sides
	// are populated, render an emphasised separator between them so
	// the visual grouping reads at a glance even on narrow terminals.
	// CC round-5 nit: at narrow widths the left/right halves looked
	// "run-together" without a clear divider.
	line2 := ""
	switch {
	case len(leftParts) > 0 && len(rightParts) > 0:
		spacer := lipgloss.NewStyle().Foreground(theme.Border).Render(" │ ")
		left := strings.Join(leftParts, sep)
		right := strings.Join(rightParts, sep)
		line2 = "  " + left + spacer + right
	case len(leftParts) > 0:
		line2 = "  " + strings.Join(leftParts, sep)
	case len(rightParts) > 0:
		line2 = "  " + strings.Join(rightParts, sep)
	}

	// Pad both lines to width. macOS Terminal.app renders a slate-on-slate
	// HUD as black-on-black (limited color profile / theme inheritance);
	// detect it and drop the background so the user sees text on their
	// terminal's natural background. ALTCODE_PLAIN_HUD=1 forces the same
	// fallback for any environment where the colored bg looks wrong.
	pad := lipgloss.NewStyle().Width(width)
	if !plainHUD() {
		pad = pad.Background(theme.HeaderBg)
	}
	result := pad.Render(line1)
	if line2 != "" {
		result += "\n" + pad.Render(line2)
	}
	return result
}

// plainHUD reports whether we should render the HUD without an
// explicit Background() call — used so the macOS Terminal "all black"
// rendering bug doesn't make the HUD invisible.
func plainHUD() bool {
	if os.Getenv("ALTCODE_PLAIN_HUD") == "1" {
		return true
	}
	// Apple_Terminal (the default macOS Terminal.app) reports a 256-color
	// profile but inherits the user's bg theme aggressively, producing
	// a black-on-black HUD on common dark profiles. Skip the bg there.
	if os.Getenv("TERM_PROGRAM") == "Apple_Terminal" {
		return true
	}
	return false
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

// renderCompactHUD renders a single-line HUD for narrow terminals (<80 cols).
// Shows only: [model] tool_activity timer
func renderCompactHUD(h hudState, info statusBarInfo, theme Theme, width int, spinnerView string) string {
	dim := lipgloss.NewStyle().Foreground(theme.Muted)
	sep := lipgloss.NewStyle().Foreground(theme.Border).Render(" │ ")
	var parts []string

	// Model
	if info.Model != "" {
		short := info.Model
		if i := strings.LastIndex(short, "/"); i >= 0 {
			short = short[i+1:]
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(theme.Secondary).Bold(true).Render("["+short+"]"))
	}

	// Active tool or thinking
	if info.ToolActive != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(theme.Primary).Render(spinnerView+" "+info.ToolActive))
	}

	// Timer
	if !h.SessionStart.IsZero() {
		parts = append(parts, dim.Render(formatDuration(time.Since(h.SessionStart).Truncate(time.Second))))
	}

	line := strings.Join(parts, sep)
	style := lipgloss.NewStyle().Width(width)
	if !plainHUD() {
		style = style.Background(theme.HeaderBg)
	}
	return style.Render(line)
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
	// Use crypto/rand for proper randomness (time-based was biased)
	a := adjectives[cryptoRandInt(len(adjectives))]
	v := verbs[cryptoRandInt(len(verbs))]
	n := nouns[cryptoRandInt(len(nouns))]
	return a + "-" + v + "-" + n
}

func cryptoRandInt(max int) int {
	if max <= 0 {
		return 0
	}
	b := make([]byte, 1)
	crand.Read(b)
	return int(b[0]) % max
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

// detectGitDirty is a lightweight check — only runs git status (1 process,
// not 3 like detectGitInfo). Called after file-changing tool results.
func detectGitDirty() bool {
	if out, err := exec.Command("git", "status", "--porcelain").Output(); err == nil {
		return strings.TrimSpace(string(out)) != ""
	}
	return false
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
