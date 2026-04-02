package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/auth"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
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

	messages  []string
	streaming string
	busy      bool
	cancel    context.CancelFunc
	events    <-chan event.Event
	tokenInfo string
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

	return &App{
		engine:        eng,
		theme:         theme,
		version:       version,
		startupPrompt: startupPrompt,
		commands:      cmdMap,
		setupInput:    setup,
		input:         ti,
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
		a.viewport = viewport.New(msg.Width, msg.Height-6)
		a.input.SetWidth(msg.Width - 2)
		a.setupInput.Width = msg.Width - 2
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
		a.streaming += ev.Text
		a.updateViewport()
		return a, a.waitForEvent()
	case event.TextDone:
		return a, a.waitForEvent()
	case event.UsageEvent:
		if ev.Usage != nil {
			a.tokenInfo = fmt.Sprintf("tokens: %d in / %d out",
				ev.Usage.InputTokens, ev.Usage.OutputTokens)
		}
		return a, a.waitForEvent()
	case event.ErrorEvent:
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
		status = lipgloss.NewStyle().
			Foreground(a.theme.Warning).
			Render("  streaming...")
	}

	inputView := a.input.View()
	if a.setupProvider != "" {
		inputView = a.setupInput.View()
	}

	return fmt.Sprintf("%s\n%s\n%s%s\n%s\n%s",
		header, sep, a.viewport.View(), status, sep, inputView)
}

func (a *App) submit() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	a.input.Reset()
	a.messages = append(a.messages, fmt.Sprintf("> %s", text))
	a.streaming = ""
	a.busy = true

	text = a.expandSlashCommand(text)
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

func (a *App) updateViewport() {
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

	lines := []string{
		logoStyle.Render(welcomeWordmark()),
		"",
		mutedStyle.Render("A blazing-fast AI coding CLI with TUI and headless modes."),
		mutedStyle.Render("Works with Claude, Codex, local models, and existing repo instructions."),
	}

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
	return strings.Join(parts, "  ")
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
