package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jiayaoqijia/altcode/internal/command"
	"github.com/jiayaoqijia/altcode/internal/engine"
	"github.com/jiayaoqijia/altcode/internal/store"
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
		a.showPaletteWithQuery("")
	}
}

func (a *App) showPaletteWithQuery(query string) {
	a.palette.SetWidth(a.mainBodyWidth())
	if query == "" {
		a.palette.Show()
	} else {
		a.palette.ShowWithQuery(query)
	}
	a.input.Blur()
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
	a.sessionSwitcher.SetWidth(a.mainBodyWidth())
	a.sessionSwitcher.Show()
	a.input.Blur()
}

// mainBodyWidth returns the width of the main area. With the sidebar
// removed, this is simply the full terminal width.
func (a *App) mainBodyWidth() int {
	return a.width
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

	// Rehydrate the TUI message display from the loaded provider
	// messages — previously we only loaded them into the engine and
	// left the viewport empty, so the user saw '[info] Switched to
	// session X' and nothing else. Now the history actually shows up.
	for _, pm := range providerMsgs {
		content := strings.TrimSpace(pm.Content)
		if content == "" {
			continue
		}
		var role messageRole
		switch pm.Role {
		case "user":
			role = roleUser
		case "assistant":
			role = roleAssistant
		case "tool":
			role = roleTool
		default:
			continue
		}
		a.messages = append(a.messages, chatMessage{role: role, content: content})
	}

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
	// Keep this list in sync with handleBuiltinCommand. Missing
	// entries here mean Ctrl+K palette can't find commands that
	// otherwise work via direct typing — discovered hands-on when
	// 'doctor' filter returned no matches despite /doctor working.
	pc := func(group, name, desc string) PaletteCommand {
		return PaletteCommand{Name: name, Description: desc, Group: group}
	}
	builtins := []PaletteCommand{
		pc("Chat", "/help", "show help"),
		pc("Chat", "/clear", "clear conversation"),
		pc("Chat", "/metadata", "show or toggle message metadata"),
		pc("Chat", "/search", "search messages in conversation"),
		pc("Inspect", "/status", "model, session, tokens"),
		pc("Inspect", "/context", "context size breakdown"),
		pc("Inspect", "/model", "show current model"),
		pc("Inspect", "/tools", "list available tools"),
		pc("Inspect", "/skills", "list discovered skills"),
		pc("Inspect", "/mcp", "list MCP servers + tools"),
		pc("Inspect", "/plugins", "show plugin warnings + search paths"),
		pc("Inspect", "/version", "show altcode version"),
		pc("Inspect", "/cost", "cost breakdown per turn"),
		pc("Inspect", "/stats", "combined status + cost"),
		pc("Project", "/history", "file change history"),
		pc("Project", "/diff", "files changed this session"),
		pc("Project", "/plan", "enter plan mode"),
		pc("Project", "/agents", "agent + context overview"),
		pc("Project", "/doctor", "check environment health"),
		pc("Project", "/init", "generate CLAUDE.md from codebase"),
		pc("Workspace", "/tasks", "list background tasks"),
		pc("Workspace", "/wf-status", "show active workflow state"),
		pc("Workspace", "/wf-cancel", "clear workflow state"),
		pc("Workspace", "/wf-pause", "pause running workflows"),
		pc("Workspace", "/wf-resume", "resume paused workflows"),
		pc("Workspace", "/team", "multi-AI team config"),
		pc("Workspace", "/backends", "detect coding backends"),
		pc("Workspace", "/compare", "A/B test prompt across models"),
		pc("Workspace", "/workspace", "start multi-agent workspace"),
		pc("Workspace", "/spawn", "add agent to active workspace"),
		pc("Workspace", "/send", "annotate agent pane (operator note)"),
		pc("Workspace", "/workflow", "run phased workflow"),
		pc("Recovery", "/compact", "trigger context compaction"),
		pc("Recovery", "/undo", "git-backed undo (stash)"),
		pc("Recovery", "/redo", "restore last undo"),
		pc("Recovery", "/rollback", "rollback to turn N"),
		// Iter-1 parity (claude-code + codex + opencode):
		pc("Config/Session", "/resume", "resume a previous session"),
		pc("Chat", "/new", "start a fresh session"),
		pc("Config/Session", "/fork", "fork session under a new id"),
		pc("Chat", "/copy", "copy last response to clipboard"),
		pc("Config/Session", "/keymap", "show keyboard shortcut reference"),
		pc("Project", "/review", "structured review of current diff"),
		pc("Config/Session", "/rename", "rename current session display title"),
		pc("Config/Session", "/share", "export conversation as markdown"),
		pc("Recovery", "/stop", "cancel in-flight engine turn"),
		pc("Config/Session", "/theme", "show / pick UI theme"),
		pc("Config/Session", "/title", "set terminal window title"),
		pc("Config/Session", "/vim", "toggle vim-modal editing"),
		pc("Config/Session", "/sessions", "list recent sessions"),
		pc("Config/Session", "/memory", "show loaded memories"),
		pc("Config/Session", "/quit", "quit altcode"),
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
			Group:       "Config/Session",
			Action: func() string {
				return "> " + name
			},
		})
	}
	return builtins
}
