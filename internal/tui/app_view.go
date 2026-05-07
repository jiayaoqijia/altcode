package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full TUI. Delegates to sub-renderers.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}
	// Degenerate terminal: not enough room for header+body+status+input.
	// Render a single-line fallback so we don't garble the display or panic.
	if a.height < a.minimumViewHeight() {
		return "altcode (terminal too small)"
	}
	header := a.renderHeader()
	body := a.renderMainBody()
	input := a.renderInputArea()
	statusBar := a.renderStatusSection()

	sections := []string{header, body}
	if statusBar != "" {
		sections = append(sections, statusBar)
	}
	sections = append(sections, input)
	result := strings.Join(sections, "\n")
	return result
}

// renderHeader renders the top logo + metadata line.
func (a *App) renderHeader() string {
	parts := []string{
		a.renderHeaderSegment("◆", "", "altcode", a.theme.Primary),
		a.renderHeaderSegment("◐", "mode", "chat", a.theme.Secondary),
	}
	if model := shortModelName(a.activeModel()); model != "" {
		parts = append(parts, a.renderHeaderSegment("✦", "model", model, a.theme.Primary))
	}
	if a.gitProject != "" {
		git := a.gitProject
		if a.gitBranch != "" {
			branch := a.gitBranch
			if a.gitDirty {
				branch += "*"
			}
			git += "@" + branch
		}
		accent := a.theme.Secondary
		if a.gitDirty {
			accent = a.theme.Warning
		}
		parts = append(parts, a.renderHeaderSegment("⎇", "git", git, accent))
	}
	sep := lipgloss.NewStyle().
		Foreground(a.theme.Border).
		Render(" · ")
	line := strings.Join(parts, sep)
	header := lipgloss.NewStyle().Width(a.width).Render(truncateStr(line, a.width))
	return header + "\n" + a.renderDivider()
}

func (a *App) renderHeaderSegment(icon, label, value string, accent lipgloss.Color) string {
	iconStyle := lipgloss.NewStyle().
		Foreground(accent).
		Bold(true)
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4A505C"))
	valueStyle := lipgloss.NewStyle().
		Foreground(a.theme.Foreground).
		Bold(label == "")

	parts := []string{iconStyle.Render(icon)}
	if label != "" {
		parts = append(parts, labelStyle.Render(label))
	}
	parts = append(parts, valueStyle.Render(value))
	return strings.Join(parts, " ")
}

// renderInputArea renders the bottom input field.
func (a *App) renderInputArea() string {
	if a.setupProvider != "" {
		return a.setupInput.View()
	}
	return a.renderComposer()
}

func (a *App) renderComposer() string {
	borderColor := a.theme.Border
	if a.input.Focused() && !a.vimMode {
		borderColor = a.theme.Primary
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(max(1, a.width-4)).
		Render(a.renderComposerText())
}

func (a *App) renderComposerText() string {
	if a.vimMode {
		return lipgloss.NewStyle().Foreground(a.theme.Warning).Bold(true).Render("NORMAL") +
			lipgloss.NewStyle().Foreground(a.theme.Muted).Render("  i insert  Ctrl+D quit")
	}
	if a.input.Focused() && strings.TrimSpace(a.input.Value()) == "" {
		return a.renderEmptyComposerText()
	}
	return a.renderFileMentionChips(trimComposerLineFill(a.input.View()))
}

func (a *App) renderEmptyComposerText() string {
	cursor := " "
	if !a.input.Cursor.Blink {
		cursor = lipgloss.NewStyle().
			Foreground(a.theme.Primary).
			Bold(true).
			Render("▌")
	}
	placeholder := lipgloss.NewStyle().
		Foreground(a.theme.Muted).
		Render(truncateStr(a.input.Placeholder, max(1, a.width-10)))
	return cursor + " " + placeholder
}

func trimComposerLineFill(view string) string {
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func (a *App) renderDivider() string {
	width := a.width
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().
		Foreground(a.theme.Border).
		Width(width).
		Render(strings.Repeat("─", width))
}

// renderMainBody determines which view mode to show.
func (a *App) renderMainBody() string {
	mainBody := a.viewport.View()
	if a.permDialog != nil && a.permDialog.IsVisible() {
		// Permission modal sits on top of every other view — it's a
		// blocking interaction. SetWidth tracks the current viewport
		// since the dialog is built once and resized on demand.
		a.permDialog.SetWidth(a.mainBodyWidth())
		mainBody = a.permDialog.View()
	} else if a.palette.IsVisible() {
		mainBody = a.palette.View()
	} else if a.sessionSwitcher.IsVisible() {
		mainBody = a.sessionSwitcher.View()
	}
	if a.wsView != nil && a.wsView.IsActive() {
		a.wsView.inputHas = a.input.Value() // sync input for Tab hint
		mainBody = a.wsView.Render(a.theme)
	}
	if a.teamView.IsActive() || a.wfRunning {
		panes := a.teamView.Render(a.theme)
		if a.wfRunning && len(a.wfHeader.phases) > 0 {
			mainBody = a.wfHeader.Render(a.theme) + "\n" + panes
		} else {
			mainBody = panes
		}
	}
	body := mainBody
	if a.filePopup.visible {
		body = a.overlayFilePopup(body)
	}
	return body
}

// buildToolActive returns the HUD tool activity string.
func (a *App) buildToolActive() string {
	if a.activeToolName != "" {
		s := a.activeToolName
		if a.activeToolDetail != "" && !strings.HasPrefix(a.activeToolDetail, "running for") {
			s += ": " + a.activeToolDetail
		}
		if !a.toolStart.IsZero() {
			if elapsed := time.Since(a.toolStart); elapsed >= time.Second {
				s += " (" + formatDuration(elapsed) + ")"
			}
		}
		return s
	}
	if a.busy {
		// Show "thinking" with elapsed time in HUD (like CC's ◐ thinking)
		if !a.turnStart.IsZero() {
			if el := time.Since(a.turnStart); el >= time.Second {
				return "thinking (" + formatDuration(el) + ")"
			}
		}
		return "thinking"
	}
	return ""
}

// buildHUDState assembles the hudState from current app state.
func (a *App) buildHUDState() hudState {
	ctxLimit := int64(128000)
	claudeMDCount, mcpCount, hooksCount := 0, 0, 0
	if a.engine != nil {
		ctxLimit = int64(a.engine.ContextWindowSize())
		claudeMDCount = len(a.engine.Instructions())
		cfg := a.engine.Config()
		mcpCount = len(cfg.MCP)
		for _, matchers := range cfg.Hooks {
			hooksCount += len(matchers)
		}
	}
	// Context-tokens fallback: prefer currentContextTokens (the last
	// turn's prompt token count, which is what the model actually saw),
	// but fall back to cumulative tokensIn when usage events haven't
	// arrived yet — some providers (notably OpenRouter forwarding
	// DeepSeek) emit prompt_tokens only at end-of-stream, leaving the
	// HUD stuck at 0/N for the entire first turn. tokensIn is at least
	// directionally correct even before the final usage chunk.
	contextTokens := a.currentContextTokens
	if contextTokens == 0 && a.tokensIn > 0 {
		contextTokens = a.tokensIn
	}
	return hudState{
		ContextTokens:  contextTokens,
		CachedTokens:   a.cachedTokens,
		QueueDepth:     len(a.queue),
		ContextLimit:   ctxLimit,
		SessionStart:   a.sessionStart,
		SessionName:    a.sessionDisplayName(),
		GitProject:     a.gitProject,
		GitBranch:      a.gitBranch,
		GitDirty:       a.gitDirty,
		ClaudeMDCount:  claudeMDCount,
		MCPCount:       mcpCount,
		HooksCount:     hooksCount,
		ToolCounts:     a.toolCounts,
		TasksTotal:     a.tasksTotal,
		TasksDone:      a.tasksDone,
		ActiveTaskName: a.activeTaskName,
	}
}

func (a *App) sessionDisplayName() string {
	if strings.TrimSpace(a.sessionTitle) != "" {
		return a.sessionTitle
	}
	return a.sessionSlug
}

// renderStatusSection renders the HUD status bar.
func (a *App) renderStatusSection() string {
	model := ""
	if a.engine != nil {
		model = a.engine.Config().Model
	}
	info := statusBarInfo{
		Model:      model,
		TokensIn:   a.tokensIn,
		TokensOut:  a.tokensOut,
		CostUSD:    a.costUSD,
		ToolActive: a.buildToolActive(),
	}
	spinView := ""
	if a.busy {
		spinView = a.spinner.View()
	}
	if !a.defaultChatShell() {
		return renderHUD(a.buildHUDState(), info, a.theme, a.width, a.vimMode, spinView)
	}
	return a.renderDivider() + "\n" + a.renderCompactStatus(info, spinView)
}

func (a *App) renderCompactStatus(info statusBarInfo, spinView string) string {
	left := "● ready"
	if info.ToolActive != "" {
		prefix := "running"
		if spinView != "" {
			prefix = strings.TrimSpace(spinView) + " " + prefix
		}
		left = "● " + prefix + " · " + info.ToolActive
	} else if a.busy {
		left = "● thinking"
	}
	right := a.statusHint()
	line := joinStatusParts(left, right, a.width)
	return lipgloss.NewStyle().
		Foreground(a.theme.Muted).
		Width(a.width).
		Render(line)
}

func (a *App) statusHint() string {
	if a.busy {
		return "Esc cancel · /stop cancel · Ctrl+K commands"
	}
	if a.width > 0 && a.width < 56 {
		return "Enter send · Ctrl+J newline"
	}
	return "Enter send · Ctrl+J newline · Ctrl+K commands"
}

func joinStatusParts(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if right == "" {
		return truncateStr(left, width)
	}
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if leftWidth+rightWidth+2 <= width {
		return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
	}
	if leftWidth >= width {
		return truncateStr(left, width)
	}
	space := width - leftWidth - 1
	if space < 1 {
		space = 1
	}
	return left + " " + truncateStr(right, space)
}

func (a *App) defaultChatShell() bool {
	return (a.wsView == nil || !a.wsView.IsActive()) && !a.teamView.IsActive() && !a.wfRunning
}

func (a *App) chromeHeight() int {
	return 2
}

func (a *App) minimumViewHeight() int {
	return max(4, a.chromeHeight()+1+a.statusHeight()+a.composerHeight())
}

func (a *App) statusHeight() int {
	if !a.defaultChatShell() {
		return 2
	}
	return 2
}

func (a *App) composerTextHeight() int {
	if a.setupProvider != "" {
		return 1
	}
	lines := a.input.LineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > 5 {
		lines = 5
	}
	return lines
}

func (a *App) composerHeight() int {
	if a.setupProvider != "" {
		return 1
	}
	return a.composerTextHeight() + 2
}
