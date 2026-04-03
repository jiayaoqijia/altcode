package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/auth"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type eventMsg event.Event
type streamDoneMsg struct{}

// App is the top-level Bubbletea model for altcode.
type App struct {
	engine        *engine.Engine
	theme         Theme
	version       string
	startupPrompt string
	commands      map[string]*command.Command
	setupInput    textinput.Model
	setupProvider string
	setupError    string
	setupSuccess  string
	input         textarea.Model
	viewport      viewport.Model
	width         int
	height        int

	palette         *Palette
	sessionSwitcher *SessionSwitcher

	messages     []string
	streaming    string
	busy         bool
	thinking     bool
	thinkingText string
	cancel       context.CancelFunc
	events       <-chan event.Event
	tokenInfo    string
}

// New creates a new App backed by the given engine and theme.
func New(eng *engine.Engine, theme Theme, version, startupPrompt string, cmds ...*command.Command) *App {
	ti := textarea.New()
	ti.Placeholder = normalInputPlaceholder(startupPrompt)
	ti.Focus()
	ti.SetHeight(3)
	ti.ShowLineNumbers = false

	setup := textinput.New()
	setup.Placeholder = "Paste API key and press Enter"
	setup.CharLimit = 4096
	setup.EchoMode = textinput.EchoPassword
	setup.EchoCharacter = '*'

	cmdMap := make(map[string]*command.Command, len(cmds))
	for _, c := range cmds {
		cmdMap[c.Name] = c
	}

	paletteCmds := buildPaletteCommands(cmdMap)
	pal := NewPalette(theme, paletteCmds)
	sw := NewSessionSwitcher(theme)

	return &App{
		engine:          eng,
		theme:           theme,
		version:         version,
		startupPrompt:   startupPrompt,
		commands:        cmdMap,
		setupInput:      setup,
		input:           ti,
		palette:         pal,
		sessionSwitcher: sw,
	}
}

func (a *App) Init() tea.Cmd {
	return textarea.Blink
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if model, cmd, handled := a.handleKey(msg); handled {
			return model, cmd
		}
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.viewport = viewport.New(msg.Width, max(1, msg.Height-6))
		a.input.SetWidth(msg.Width - 2)
		a.setupInput.Width = msg.Width - 2
		a.palette.SetWidth(msg.Width)
		a.sessionSwitcher.SetWidth(msg.Width)
		a.updateViewport()
		return a, nil
	case eventMsg:
		return a.handleEvent(event.Event(msg))
	case streamDoneMsg:
		return a, nil
	}

	if a.setupProvider != "" {
		var cmd tea.Cmd
		a.setupInput, cmd = a.setupInput.Update(msg)
		return a, cmd
	}

	if !a.busy {
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		return a, cmd
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	// Route to palette overlay when visible.
	if a.palette.IsVisible() {
		return a.handlePaletteKey(msg)
	}

	// Route to session switcher overlay when visible.
	if a.sessionSwitcher.IsVisible() {
		return a.handleSwitcherKey(msg)
	}

	if a.setupProvider != "" {
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

	switch msg.String() {
	case "ctrl+k":
		a.togglePalette()
		return a, nil, true
	case "ctrl+a":
		a.toggleSessionSwitcher()
		return a, nil, true
	case "esc":
		if a.busy {
			if a.cancel != nil {
				a.cancel()
			}
			a.busy = false
			return a, nil, true
		}
		return a, tea.Quit, true
	case "a", "1":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.beginSetup("anthropic")
			return a, nil, true
		}
	case "enter":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.startRecommendedSetup()
			return a, nil, true
		}
		if a.busy || strings.TrimSpace(a.input.Value()) == "" {
			return a, nil, true
		}
		return a, a.submit(), true
	case "ctrl+j":
		if a.busy {
			return a, nil, true
		}
		a.input.InsertString("\n")
		return a, nil, true
	case "ctrl+d":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.startRecommendedSetup()
			return a, nil, true
		}
		if a.busy || strings.TrimSpace(a.input.Value()) == "" {
			return a, nil, true
		}
		return a, a.submit(), true
	case "ctrl+c":
		return a, tea.Quit, true
	case "o", "2":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.beginSetup("openai")
			return a, nil, true
		}
	}
	return a, nil, false
}

func (a *App) startRecommendedSetup() {
	if provider := a.recommendedSetupProvider(); provider != "" {
		a.beginSetup(provider)
		return
	}
	a.setupError = "This model needs credentials before altcode can send prompts."
	a.setupSuccess = ""
	a.updateViewport()
}

func (a *App) beginSetup(provider string) {
	a.setupProvider = provider
	a.setupError = ""
	a.setupSuccess = ""
	a.setupInput.Reset()
	a.setupInput.Prompt = providerLabel(provider) + " API key: "
	a.setupInput.Placeholder = providerSetupPlaceholder(provider)
	a.setupInput.Focus()
	a.updateViewport()
}

func (a *App) cancelSetup() {
	a.setupProvider = ""
	a.setupInput.Blur()
	a.setupInput.Reset()
	a.input.Focus()
	a.updateViewport()
}

func (a *App) saveSetupKey() {
	key := strings.TrimSpace(a.setupInput.Value())
	if key == "" {
		a.setupError = "Paste an API key before saving."
		a.setupSuccess = ""
		a.updateViewport()
		return
	}

	providerName := a.setupProvider
	requiredProvider := a.recommendedSetupProvider()
	currentModel := a.activeModel()
	cfg := a.engine.Config()
	pcfg := cfg.Provider[providerName]
	pcfg.APIKey = key
	cfg.Provider[providerName] = pcfg

	path, err := auth.SaveProviderAPIKey(providerName, key)
	if err != nil {
		a.setupError = fmt.Sprintf("Could not save the API key: %v", err)
		a.setupSuccess = ""
		a.updateViewport()
		return
	}

	if err := a.refreshEngine(); err != nil {
		a.setupError = fmt.Sprintf("Saved the API key, but could not refresh altcode: %v", err)
		a.setupSuccess = ""
		a.updateViewport()
		return
	}

	a.setupProvider = ""
	a.setupInput.Blur()
	a.setupInput.Reset()
	a.input.Focus()
	a.startupPrompt = auth.MissingCredentialPrompt(a.engine.Config())
	a.input.Placeholder = normalInputPlaceholder(a.startupPrompt)
	a.setupError = ""
	a.setupSuccess = fmt.Sprintf("Saved %s API key to %s.", providerLabel(providerName), path)
	if strings.TrimSpace(a.startupPrompt) == "" && providerName == requiredProvider {
		a.setupSuccess += " You can start chatting now."
	} else if requiredProvider != "" && providerName != requiredProvider {
		a.setupSuccess += fmt.Sprintf(
			" Current model %s still needs %s credentials, or you can relaunch with --model %s/...",
			currentModel,
			providerLabel(requiredProvider),
			providerName,
		)
	}
	a.updateViewport()
}

func (a *App) refreshEngine() error {
	if a.engine == nil {
		return nil
	}

	refreshed, err := engine.New(engine.EngineParams{
		Config:       a.engine.Config(),
		Perm:         a.engine.PermissionEvaluator(),
		Store:        a.engine.StoreInstance(),
		SessionID:    a.engine.SessionID(),
		Messages:     a.engine.Messages(),
		Hooks:        a.engine.HooksRunner(),
		Instructions: a.engine.Instructions(),
		Memory:       a.engine.MemoryStore(),
	})
	if err != nil {
		return err
	}

	a.engine = refreshed
	return nil
}

func (a *App) handleEvent(ev event.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case event.TextDelta:
		a.thinking = false
		a.streaming += ev.Text
		a.updateViewport()
		return a, a.waitForEvent()
	case event.TextDone:
		a.thinking = false
		return a, a.waitForEvent()
	case event.ThinkingDelta:
		a.thinking = true
		a.thinkingText = ev.Thinking
		a.updateViewport()
		return a, a.waitForEvent()
	case event.ToolStart:
		a.thinking = true
		name := ""
		if ev.ToolCall != nil {
			name = ev.ToolCall.Name
		}
		a.thinkingText = "using " + name + "..."
		a.updateViewport()
		return a, a.waitForEvent()
	case event.ToolResultEvent:
		a.thinking = false
		a.updateViewport()
		return a, a.waitForEvent()
	case event.UsageEvent:
		if ev.Usage != nil {
			a.tokenInfo = fmt.Sprintf("tokens: %d in / %d out",
				ev.Usage.InputTokens, ev.Usage.OutputTokens)
		}
		return a, a.waitForEvent()
	case event.ErrorEvent:
		if provider := a.authErrorProvider(ev.Error); provider != "" {
			a.busy = false
			a.streaming = ""
			a.repromptForAPIKey(provider)
			return a, nil
		}
		a.messages = append(a.messages,
			fmt.Sprintf("[error] %s", ev.Error))
		a.streaming = ""
		a.busy = false
		a.updateViewport()
		return a, nil
	case event.Done:
		if a.streaming != "" {
			a.messages = append(a.messages, a.streaming)
			a.streaming = ""
		}
		a.busy = false
		a.updateViewport()
		return a, nil
	}
	return a, a.waitForEvent()
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	header := lipgloss.NewStyle().
		Foreground(a.theme.Primary).
		Bold(true).
		Render("altcode") +
		lipgloss.NewStyle().
			Foreground(a.theme.Muted).
			Render("  "+a.headerMeta())

	sep := lipgloss.NewStyle().
		Foreground(a.theme.Border).
		Render(strings.Repeat("─", a.width))

	status := ""
	if a.busy {
		if a.thinking && a.thinkingText != "" {
			status = lipgloss.NewStyle().
				Foreground(a.theme.Secondary).
				Italic(true).
				Render("  ◆ " + a.thinkingText)
		} else if a.thinking {
			status = lipgloss.NewStyle().
				Foreground(a.theme.Secondary).
				Italic(true).
				Render("  ◆ thinking...")
		} else {
			status = lipgloss.NewStyle().
				Foreground(a.theme.Warning).
				Render("  ● streaming...")
		}
	}

	inputView := a.input.View()
	if a.setupProvider != "" {
		inputView = a.setupInput.View()
	}

	body := a.viewport.View()
	if a.palette.IsVisible() {
		body = a.palette.View()
	} else if a.sessionSwitcher.IsVisible() {
		body = a.sessionSwitcher.View()
	}

	return fmt.Sprintf("%s\n%s\n%s%s\n%s\n%s",
		header, sep, body, status, sep, inputView)
}

func (a *App) submit() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	a.input.Reset()
	a.messages = append(a.messages, fmt.Sprintf("> %s", text))
	a.streaming = ""

	if a.handleBuiltinCommand(text) {
		a.busy = false
		return nil
	}

	a.busy = true
	a.thinking = true
	a.thinkingText = ""

	text = a.expandSlashCommand(text)
	a.updateViewport()

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.events = a.engine.Run(ctx, text)

	return a.waitForEvent()
}

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

func (a *App) expandSlashCommand(text string) string {
	if !strings.HasPrefix(text, "/") {
		return text
	}
	parts := strings.SplitN(text, " ", 2)
	name := strings.TrimPrefix(parts[0], "/")
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	cmd, ok := a.commands[name]
	if !ok {
		return text
	}
	expanded, err := cmd.Expand(args)
	if err != nil {
		return text
	}
	return expanded
}

func (a *App) waitForEvent() tea.Cmd {
	ch := a.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return eventMsg(ev)
	}
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
	if msg.String() == "enter" {
		selected, ok := a.palette.Selected()
		if ok {
			a.palette.Hide()
			a.input.Focus()
			cmdText := selected.Name
			a.messages = append(a.messages, fmt.Sprintf("> %s", cmdText))
			if !a.handleBuiltinCommand(cmdText) {
				expanded := a.expandSlashCommand(cmdText)
				if expanded != cmdText {
					return a, a.submitText(expanded), true
				}
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
		{Name: "/team", Description: "multi-AI team config"},
		{Name: "/backends", Description: "detect coding backends"},
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

func (a *App) updateViewport() {
	if a.setupProvider != "" {
		a.viewport.SetContent(a.welcomeView())
		a.viewport.GotoTop()
		return
	}
	if len(a.messages) == 0 && a.streaming == "" {
		a.viewport.SetContent(a.welcomeView())
		a.viewport.GotoTop()
		return
	}

	var sb strings.Builder
	for _, m := range a.messages {
		sb.WriteString(m)
		sb.WriteString("\n\n")
	}
	if a.streaming != "" {
		sb.WriteString(a.streaming)
	}
	a.viewport.SetContent(sb.String())
	a.viewport.GotoBottom()
}

func (a *App) welcomeView() string {
	logoStyle := lipgloss.NewStyle().
		Foreground(a.theme.Primary).
		Bold(true)

	titleStyle := lipgloss.NewStyle().
		Foreground(a.theme.Secondary).
		Bold(true)

	mutedStyle := lipgloss.NewStyle().
		Foreground(a.theme.Muted)

	codeStyle := lipgloss.NewStyle().
		Foreground(a.theme.Warning).
		Bold(true)

	version := a.version
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}

	lines := a.welcomeHeader(logoStyle, mutedStyle, version)

	if a.setupSuccess != "" {
		successStyle := lipgloss.NewStyle().
			Foreground(a.theme.Success).
			Bold(true)
		lines = append(lines, "", successStyle.Render(a.setupSuccess))
	}

	if a.setupError != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(a.theme.Error).
			Bold(true)
		lines = append(lines, "", errorStyle.Render(a.setupError))
	}

	if a.setupProvider != "" {
		provider := a.setupProvider
		lines = append(lines,
			"",
			titleStyle.Render("Set "+providerLabel(provider)+" API key"),
			mutedStyle.Render("Current model: ")+codeStyle.Render(a.activeModel()),
			mutedStyle.Render("Paste the key below. It will be masked and saved to ")+codeStyle.Render(auth.UserConfigPath()),
			mutedStyle.Render("• Press ")+codeStyle.Render("Enter")+mutedStyle.Render(" to save the key"),
			mutedStyle.Render("• Press ")+codeStyle.Render("Esc")+mutedStyle.Render(" to go back"),
			mutedStyle.Render("• altcode can also auto-detect ")+codeStyle.Render(providerLoginLabel(provider))+mutedStyle.Render(" on restart"),
		)
		return strings.Join(lines, "\n")
	}

	if strings.TrimSpace(a.startupPrompt) != "" {
		warningStyle := lipgloss.NewStyle().
			Foreground(a.theme.Warning).
			Bold(true)
		provider := a.recommendedSetupProvider()
		lines = append(lines,
			"",
			titleStyle.Render("Let's get altcode connected"),
			warningStyle.Render("Current model: "+a.activeModel()),
			"",
			titleStyle.Render("Recommended next step"),
			mutedStyle.Render("• Press ")+codeStyle.Render("Enter")+mutedStyle.Render(" to add your ")+codeStyle.Render(providerLabel(provider)+" API key"),
			mutedStyle.Render("• Your key is masked while typing and saved to ")+codeStyle.Render(auth.UserConfigPath()),
			mutedStyle.Render("• Already signed into ")+codeStyle.Render(providerLoginLabel(provider))+mutedStyle.Render("? Restart altcode and it will auto-detect it"),
			"",
			titleStyle.Render("Other paths"),
			mutedStyle.Render("• Press ")+codeStyle.Render("A")+mutedStyle.Render(" to save an Anthropic key manually"),
			mutedStyle.Render("• Press ")+codeStyle.Render("O")+mutedStyle.Render(" to save an OpenAI key manually"),
			mutedStyle.Render("• Want local models instead? Relaunch with ")+codeStyle.Render("--model ollama/<model>")+mutedStyle.Render(" or ")+codeStyle.Render("--model lmstudio/<model>"),
		)
	}

	lines = append(lines,
		"",
		titleStyle.Render("How to use"),
		mutedStyle.Render("• Type a prompt below and press ")+codeStyle.Render("Enter")+mutedStyle.Render(" to send"),
		mutedStyle.Render("• Press ")+codeStyle.Render("Ctrl+J")+mutedStyle.Render(" for a newline"),
		mutedStyle.Render("• Press ")+codeStyle.Render("Ctrl+K")+mutedStyle.Render(" for the command palette"),
		mutedStyle.Render("• Press ")+codeStyle.Render("Ctrl+A")+mutedStyle.Render(" to switch sessions"),
		mutedStyle.Render("• Use ")+codeStyle.Render("/command")+mutedStyle.Render(" for discovered slash commands"),
		mutedStyle.Render("• Press ")+codeStyle.Render("Esc")+mutedStyle.Render(" to quit"),
	)

	if strings.TrimSpace(a.startupPrompt) == "" {
		lines = append(lines,
			"",
			titleStyle.Render("Try asking"),
			mutedStyle.Render("• summarize this repository"),
			mutedStyle.Render("• find where auth is loaded"),
			mutedStyle.Render("• add a test for the billing route"),
		)
	}

	return strings.Join(lines, "\n")
}

func (a *App) repromptForAPIKey(provider string) {
	a.beginSetup(provider)
	a.setupError = fmt.Sprintf(
		"%s rejected the current API key for model %s. Enter a new key to continue.",
		providerLabel(provider),
		a.activeModel(),
	)
	a.setupSuccess = ""
	a.updateViewport()
}

func (a *App) welcomeHeader(logoStyle, mutedStyle lipgloss.Style, version string) []string {
	if a.width < 70 || a.viewport.Height < 10 {
		return []string{
			logoStyle.Render("altcode") + lipgloss.NewStyle().Foreground(a.theme.Muted).Render("  v"+displayVersion(version)),
			"",
			mutedStyle.Render("Fast AI coding CLI for Claude, Codex, and local models."),
		}
	}

	return []string{
		logoStyle.Render(welcomeWordmark()),
		"",
		mutedStyle.Render("A blazing-fast AI coding CLI with TUI and headless modes."),
		mutedStyle.Render("Works with Claude, Codex, local models, and existing repo instructions."),
	}
}

func normalInputPlaceholder(startupPrompt string) string {
	if strings.TrimSpace(startupPrompt) != "" {
		switch currentProviderFromPrompt(startupPrompt) {
		case "anthropic":
			return "Press Enter to set up your Anthropic API key"
		case "openai":
			return "Press Enter to set up your OpenAI API key"
		default:
			return "Press Enter to start setup"
		}
	}
	return "Ask anything... (Enter to submit, Ctrl+J newline, Esc to quit)"
}

func (a *App) headerMeta() string {
	parts := []string{"v" + displayVersion(a.version)}
	if strings.TrimSpace(a.tokenInfo) != "" {
		parts = append(parts, a.tokenInfo)
	}
	if costInfo := a.costSummaryShort(); costInfo != "" {
		parts = append(parts, costInfo)
	}
	return strings.Join(parts, "  ")
}

// costSummaryShort returns a compact "Xk tokens / $Y.YYYY" string
// from the engine's CostTracker, or empty if unavailable.
func (a *App) costSummaryShort() string {
	if a.engine == nil || a.engine.CostTracker() == nil {
		return ""
	}
	ct := a.engine.CostTracker()
	in, out := ct.TotalTokens()
	if in+out == 0 {
		return ""
	}
	return fmt.Sprintf("%dk tokens · $%.4f", (in+out)/1000, ct.TotalCost())
}

func displayVersion(version string) string {
	if strings.TrimSpace(version) == "" {
		return "dev"
	}
	return version
}

func welcomeWordmark() string {
	return strings.Join([]string{
		"    _     _   _____   ____    ___    ____    _____ ",
		"   / \\   | | |_   _| / ___|  / _ \\  |  _ \\  | ____|",
		"  / _ \\  | |   | |  | |     | | | | | | | | |  _|  ",
		" / ___ \\ | |___| |  | |___  | |_| | | |_| | | |___ ",
		"/_/   \\_\\|_____|_|   \\____|  \\___/  |____/  |_____|",
	}, "\n")
}

func currentProviderFromPrompt(prompt string) string {
	switch {
	case strings.Contains(prompt, "Anthropic"):
		return "anthropic"
	case strings.Contains(prompt, "OpenAI"):
		return "openai"
	default:
		return ""
	}
}

func (a *App) activeModel() string {
	if a.engine == nil || a.engine.Config() == nil || strings.TrimSpace(a.engine.Config().Model) == "" {
		return "anthropic/claude-sonnet-4-20250514"
	}
	return a.engine.Config().Model
}

func (a *App) recommendedSetupProvider() string {
	if a.engine != nil && a.engine.Config() != nil && strings.TrimSpace(a.engine.Config().Model) != "" {
		return parseProvider(a.engine.Config().Model)
	}
	return currentProviderFromPrompt(a.startupPrompt)
}

func parseProvider(model string) string {
	for i := 0; i < len(model); i++ {
		if model[i] == '/' {
			return model[:i]
		}
	}
	if model == "" {
		return "anthropic"
	}
	return model
}

func providerSetupPlaceholder(provider string) string {
	switch provider {
	case "anthropic":
		return "Paste your Anthropic API key and press Enter"
	case "openai":
		return "Paste your OpenAI API key and press Enter"
	default:
		return "Paste API key and press Enter"
	}
}

func providerLoginLabel(provider string) string {
	switch provider {
	case "anthropic":
		return "Claude Code"
	case "openai":
		return "Codex"
	default:
		return "your CLI"
	}
}

func (a *App) authErrorProvider(err string) string {
	lower := strings.ToLower(err)
	current := a.recommendedSetupProvider()

	if current == "anthropic" && looksLikeAuthError(lower, "anthropic") {
		return "anthropic"
	}
	if current == "openai" && looksLikeAuthError(lower, "openai") {
		return "openai"
	}

	if looksLikeAuthError(lower, "anthropic") {
		return "anthropic"
	}
	if looksLikeAuthError(lower, "openai") {
		return "openai"
	}
	return ""
}

func looksLikeAuthError(msg, provider string) bool {
	authSignals := []string{
		" status 401",
		" status 403",
		"unauthorized",
		"authentication",
		"auth_error",
		"authentication_error",
		"invalid api key",
		"incorrect api key",
		"invalid x-api-key",
	}

	hasProvider := strings.Contains(msg, provider)
	for _, signal := range authSignals {
		if strings.Contains(msg, signal) {
			if hasProvider {
				return true
			}
			if signal == "incorrect api key" || signal == "invalid api key" || signal == "invalid x-api-key" {
				return true
			}
		}
	}
	return false
}

func providerLabel(name string) string {
	switch name {
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	default:
		if name == "" {
			return "Provider"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
