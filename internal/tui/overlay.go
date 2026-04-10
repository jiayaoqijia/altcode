package tui

import (
	"context"
	"fmt"

	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// submitText sends the given text to the engine as a new turn.
func (a *App) submitText(text string) tea.Cmd {
	a.busy = true
	a.thinking = true
	a.thinkingText = ""
	a.streaming = ""
	a.updateViewport()

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.events = a.engine.Run(ctx, text)
	return a.waitForEvent()
}

// togglePalette opens or closes the command palette.
func (a *App) togglePalette() {
	if a.palette.IsVisible() {
		a.palette.Hide()
		a.input.Focus()
	} else {
		a.palette.SetWidth(a.width)
		a.palette.Show()
		a.input.Blur()
	}
}

// toggleSessionSwitcher opens or closes the session switcher.
func (a *App) toggleSessionSwitcher() {
	if a.sessionSwitcher.IsVisible() {
		a.sessionSwitcher.Hide()
		a.input.Focus()
		return
	}
	if a.engine != nil && a.engine.StoreInstance() != nil {
		_ = a.sessionSwitcher.Load(a.engine.StoreInstance())
	}
	a.sessionSwitcher.SetWidth(a.width)
	a.sessionSwitcher.Show()
	a.input.Blur()
}

// handlePaletteKey routes keys while the palette is open.
func (a *App) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if msg.String() == "ctrl+c" {
		return a, tea.Quit, true
	}
	if msg.String() == "esc" {
		// Palette has its own Esc-handler that hides itself, but the
		// overlay wrapper must re-focus the textarea — otherwise the
		// main input stays blurred and subsequent keystrokes are eaten.
		a.palette.Hide()
		a.input.Focus()
		return a, nil, true
	}
	if msg.String() == "enter" {
		selected, ok := a.palette.Selected()
		if ok {
			a.palette.Hide()
			a.input.Focus()
			cmdText := selected.Name
			a.messages = append(a.messages, chatMessage{role: roleUser, content: cmdText})
			if handled, cmd := a.handleBuiltinCommand(cmdText); !handled {
				expanded := a.expandSlashCommand(cmdText)
				if expanded != cmdText {
					return a, a.submitText(expanded), true
				}
			} else if cmd != nil {
				return a, cmd, true
			}
			a.updateViewport()
		}
		return a, nil, true
	}
	a.palette.UpdateKey(msg)
	return a, nil, true
}

// handleSwitcherKey routes keys while the session switcher is open.
func (a *App) handleSwitcherKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if msg.String() == "ctrl+c" {
		return a, tea.Quit, true
	}
	if msg.String() == "esc" {
		// Same fix as the palette: hide the overlay AND give the
		// main input its focus back. The switcher's own UpdateKey
		// only hides itself; it can't touch the app's textarea.
		a.sessionSwitcher.Hide()
		a.input.Focus()
		return a, nil, true
	}
	if msg.String() == "enter" {
		id := a.sessionSwitcher.SelectedID()
		if id != "" {
			a.switchToSession(id)
		}
		a.sessionSwitcher.Hide()
		a.input.Focus()
		return a, nil, true
	}
	a.sessionSwitcher.UpdateKey(msg)
	return a, nil, true
}

// switchToSession loads messages from the given session into the engine.
func (a *App) switchToSession(sessionID string) {
	if a.engine == nil || a.engine.StoreInstance() == nil {
		return
	}
	db := a.engine.StoreInstance()
	msgs, err := db.ListMessages(sessionID)
	if err != nil {
		a.appendInfo(fmt.Sprintf("[error] loading session: %v", err))
		return
	}
	providerMsgs := store.ToProviderMessages(msgs)

	refreshed, err := engine.New(engine.EngineParams{
		Config:       a.engine.Config(),
		Perm:         a.engine.PermissionEvaluator(),
		Store:        db,
		SessionID:    sessionID,
		Messages:     providerMsgs,
		Hooks:        a.engine.HooksRunner(),
		Instructions: a.engine.Instructions(),
		Memory:       a.engine.MemoryStore(),
	})
	if err != nil {
		a.appendInfo(fmt.Sprintf("[error] switching session: %v", err))
		return
	}
	a.engine = refreshed
	a.messages = nil
	a.streaming = ""
	a.busy = false

	sess, err := db.GetSession(sessionID)
	title := sessionID[:8]
	if err == nil && sess.Title != "" {
		title = sess.Title
	}
	a.appendInfo(fmt.Sprintf("Switched to session %s (%s)",
		sessionID[:8], title))
}

// buildPaletteCommands creates PaletteCommand entries from builtins
// and loaded slash commands.
func buildPaletteCommands(
	cmds map[string]*command.Command,
) []PaletteCommand {
	builtins := []PaletteCommand{
		{Name: "/help", Description: "show help"},
		{Name: "/status", Description: "model, session, tokens"},
		{Name: "/context", Description: "context size breakdown"},
		{Name: "/model", Description: "show current model"},
		{Name: "/clear", Description: "clear conversation"},
		{Name: "/tools", Description: "list available tools"},
		{Name: "/sessions", Description: "list recent sessions"},
		{Name: "/memory", Description: "show loaded memories"},
		{Name: "/version", Description: "show altcode version"},
		{Name: "/cost", Description: "cost breakdown per turn"},
		{Name: "/history", Description: "file change history"},
		{Name: "/compact", Description: "trigger context compaction"},
		{Name: "/diff", Description: "files changed this session"},
		{Name: "/plan", Description: "enter plan mode"},
		{Name: "/stats", Description: "combined status + cost"},
		{Name: "/tasks", Description: "list background tasks"},
		{Name: "/wf-status", Description: "show active workflow state"},
		{Name: "/wf-cancel", Description: "clear workflow state"},
		{Name: "/agents", Description: "agent + context overview"},
		{Name: "/wf-pause", Description: "pause running workflows"},
		{Name: "/wf-resume", Description: "resume paused workflows"},
		{Name: "/team", Description: "multi-AI team config"},
		{Name: "/backends", Description: "detect coding backends"},
		{Name: "/undo", Description: "git-backed undo (stash)"},
		{Name: "/redo", Description: "restore last undo"},
	}
	// Assign actions that return the command name (submitted by the app).
	for i := range builtins {
		name := builtins[i].Name
		builtins[i].Action = func() string { return "> " + name }
	}

	// Add discovered slash commands from markdown files.
	for _, c := range cmds {
		name := "/" + c.Name
		desc := c.Description
		if desc == "" {
			desc = "slash command"
		}
		builtins = append(builtins, PaletteCommand{
			Name:        name,
			Description: desc,
			Action: func() string {
				return "> " + name
			},
		})
	}
	return builtins
}
