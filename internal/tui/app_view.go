package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full TUI. Delegates to sub-renderers.
func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
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
	if a.palette.IsVisible() {
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
	if a.sidebar.width > 0 && !a.teamView.IsActive() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, mainBody, a.sidebar.View())
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
	ctxLimit := 128000
	claudeMDCount, mcpCount, hooksCount := 0, 0, 0
	if a.engine != nil {
		ctxLimit = a.engine.ContextWindowSize()
		claudeMDCount = len(a.engine.Instructions())
		cfg := a.engine.Config()
		mcpCount = len(cfg.MCP)
		for _, matchers := range cfg.Hooks {
			hooksCount += len(matchers)
		}
	}
	return hudState{
		ContextTokens:  a.tokensIn + a.tokensOut,
		ContextLimit:   ctxLimit,
		SessionStart:   a.sessionStart,
		SessionName:    a.sessionSlug,
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
