package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full TUI. Delegates to sub-renderers.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}
	// Degenerate terminal: not enough room for header+sep+body+status+input.
	// Render a single-line fallback so we don't garble the display or panic.
	if a.height < 4 {
		return "altcode (terminal too small)"
	}
	header := a.renderHeader()
	sep := lipgloss.NewStyle().Foreground(a.theme.Border).
		Render(strings.Repeat("─", a.width))
	body := a.renderMainBody()
	input := a.renderInputArea()
	statusBar := a.renderStatusSection()
	popup := a.filePopupView()

	result := fmt.Sprintf("%s\n%s\n%s\n%s\n%s",
		header, sep, body, statusBar, input)
	if popup != "" {
		result += "\n" + popup
	}
	return result
}

// renderHeader renders the top logo + metadata line.
func (a *App) renderHeader() string {
	logo := lipgloss.NewStyle().
		Foreground(a.theme.Primary).Bold(true).
		Render("⌬ altcode")
	meta := lipgloss.NewStyle().
		Foreground(a.theme.Muted).
		Render("  " + a.headerMeta())
	return logo + meta
}

// renderInputArea renders the bottom input field.
func (a *App) renderInputArea() string {
	if a.setupProvider != "" {
		return a.setupInput.View()
	}
	if a.vimMode {
		return lipgloss.NewStyle().
			Foreground(a.theme.Warning).Bold(true).
			Render("-- NORMAL --") +
			lipgloss.NewStyle().Foreground(a.theme.Muted).
				Render("  (i to insert, Ctrl+D to quit)")
	}
	return a.input.View()
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
	return mainBody
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
	return renderHUD(a.buildHUDState(), info, a.theme, a.width, a.vimMode, spinView)
}
