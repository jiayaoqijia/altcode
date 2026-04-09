package tui

import (
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/orchestra"
	tea "github.com/charmbracelet/bubbletea"
)

// handleKey is the top-level key router. Delegates to focused sub-handlers.
func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if a.palette.IsVisible() {
		return a.handlePaletteKey(msg)
	}
	if a.sessionSwitcher.IsVisible() {
		return a.handleSwitcherKey(msg)
	}
	if a.filePopup.visible {
		return a.handleFilePopupKey(msg)
	}
	if a.vimMode && !a.busy {
		return a.handleVimModeKey(msg)
	}
	if a.setupProvider != "" {
		return a.handleSetupKey(msg)
	}
	if a.wsView != nil && a.wsView.IsActive() {
		if m, cmd, ok := a.handleWorkspaceKey(msg); ok {
			return m, cmd, ok
		}
	}
	if a.wfRunning {
		if m, cmd, ok := a.handleWorkflowKey(msg); ok {
			return m, cmd, ok
		}
	}
	return a.handleGlobalKey(msg)
}

// handleSetupKey handles keys during API key setup flow.
func (a *App) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		a.cancelSetup()
		return a, nil, true
	case "enter", "ctrl+d":
		a.saveSetupKey()
		return a, nil, true
	case "ctrl+c":
		return a, tea.Quit, true
	}
	return a, nil, false
}

// handleWorkspaceKey handles keys when workspace view is active.
func (a *App) handleWorkspaceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+z":
		a.wsView.SetPaused(true)
		a.appendInfo("[workspace] Paused.")
		return a, nil, true
	case "ctrl+q":
		a.wsView.Stop()
		a.busy = false
		a.appendInfo("[workspace] Aborted.")
		return a, nil, true
	case "ctrl+r":
		a.wsView.SetPaused(false)
		a.appendInfo("[workspace] Resumed.")
		return a, nil, true
	case "tab":
		if strings.HasPrefix(a.input.Value(), "/") {
			if a.trySlashComplete() {
				return a, nil, true
			}
		}
		a.wsView.CycleFocus()
		return a, nil, true
	case "ctrl+1":
		a.wsView.FocusAgent(0)
		return a, nil, true
	case "ctrl+2":
		a.wsView.FocusAgent(1)
		return a, nil, true
	case "ctrl+3":
		a.wsView.FocusAgent(2)
		return a, nil, true
	case "ctrl+s":
		focused := a.wsView.FocusedRole()
		if focused != "" {
			a.input.SetValue(fmt.Sprintf("/send %s ", focused))
			a.input.Focus()
		} else {
			a.appendInfo("No agent focused. Press Tab to cycle, then Ctrl+S.")
		}
		return a, nil, true
	}
	return a, nil, false
}

// handleWorkflowKey handles keys during workflow execution.
func (a *App) handleWorkflowKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+z":
		select {
		case a.wfOverride <- orchestra.OverrideCmd{Op: orchestra.OpPause}:
			a.appendInfo("[workflow] Paused.")
		default:
		}
		return a, nil, true
	case "ctrl+q":
		select {
		case a.wfOverride <- orchestra.OverrideCmd{Op: orchestra.OpAbort}:
			a.appendInfo("[workflow] Aborting...")
		default:
		}
		return a, nil, true
	case "ctrl+r":
		select {
		case a.wfOverride <- orchestra.OverrideCmd{Op: orchestra.OpResume}:
			a.appendInfo("[workflow] Resumed.")
		default:
		}
		return a, nil, true
	}
	return a, nil, false
}

// handleGlobalKey handles keys in the main input mode.
func (a *App) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+k":
		a.togglePalette()
		return a, nil, true
	case "ctrl+a":
		a.toggleSessionSwitcher()
		return a, nil, true
	case "esc":
		return a.handleEscKey()
	case "tab":
		if !a.busy && a.trySlashComplete() {
			return a, nil, true
		}
		return a, nil, true
	case "enter":
		return a.handleEnterKey()
	case "ctrl+j":
		if !a.busy {
			a.input.InsertString("\n")
		}
		return a, nil, true
	case "up":
		// Up arrow: recall previous prompt from history
		if !a.busy {
			if text, ok := a.inputHistory.Up(a.input.Value()); ok {
				a.input.Reset()
				a.input.SetValue(text)
			}
			return a, nil, true
		}
	case "down":
		// Down arrow: recall next prompt from history
		if !a.busy {
			if text, ok := a.inputHistory.Down(); ok {
				a.input.Reset()
				a.input.SetValue(text)
			}
			return a, nil, true
		}
	case "ctrl+l":
		// Ctrl+L = clear screen (like CC and bash)
		a.messages = a.messages[:0]
		a.streaming = ""
		a.updateViewport()
		return a, nil, true
	case "ctrl+r":
		// Ctrl+R = retry last prompt (re-submit previous user message)
		if !a.busy {
			if last := a.lastUserMessage(); last != "" {
				a.input.SetValue(last)
				return a, a.submit(), true
			}
		}
		return a, nil, true
	case "ctrl+d":
		// Ctrl+D = quit
		return a, tea.Quit, true
	case "ctrl+c":
		return a.handleCtrlCKey()
	case "a", "1":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.beginSetup("anthropic")
			return a, nil, true
		}
	case "o", "2":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.beginSetup("openai")
			return a, nil, true
		}
	}
	return a, nil, false
}

// handleEscKey implements the multi-step Esc behavior.
func (a *App) handleEscKey() (tea.Model, tea.Cmd, bool) {
	if a.filePopup.visible {
		a.dismissFilePopup()
		return a, nil, true
	}
	if a.busy {
		if a.cancel != nil {
			a.cancel()
		}
		a.busy = false
		a.streaming = ""
		a.appendInfo("[cancelled]")
		return a, nil, true
	}
	if strings.TrimSpace(a.input.Value()) != "" {
		a.input.Reset()
		return a, nil, true
	}
	if !a.vimMode {
		a.vimMode = true
		a.input.Blur()
		return a, nil, true
	}
	return a, nil, true
}

// handleEnterKey submits the prompt or starts setup.
func (a *App) handleEnterKey() (tea.Model, tea.Cmd, bool) {
	// If user typed text, always submit it — don't redirect to setup
	if !a.busy && strings.TrimSpace(a.input.Value()) != "" {
		return a, a.submit(), true
	}
	// Empty input + startup prompt → start setup
	if strings.TrimSpace(a.startupPrompt) != "" {
		a.startRecommendedSetup()
		return a, nil, true
	}
	return a, nil, true
}

// handleCtrlCKey cancels generation or copies last response.
func (a *App) handleCtrlCKey() (tea.Model, tea.Cmd, bool) {
	if a.busy {
		if a.cancel != nil {
			a.cancel()
		}
		a.busy = false
		a.streaming = ""
		a.appendInfo("[cancelled]")
		return a, nil, true
	}
	if last := a.lastAssistantMessage(); last != "" {
		copyToClipboard(last)
		a.appendInfo("[copied last response to clipboard]")
		return a, nil, true
	}
	return a, tea.Quit, true
}
