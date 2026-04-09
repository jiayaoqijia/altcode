package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/orchestra"
	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type eventMsg event.Event
type streamDoneMsg struct{}
type wfEventMsg orchestra.PhaseEvent  // workflow phase event from orchestra
type wfDoneMsg struct{}               // workflow completed
type workspacePollMsg struct{}        // periodic workspace poll tick
type workspaceTransitionMsg struct {  // workspace state machine transition
	Status workspace.WorkspaceStatus
}
type stuckToolMsg struct {            // tool running longer than threshold
	name      string
	startedAt time.Time
}

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
	projectRoot     string
	mdRenderer      *MarkdownRenderer

	filePopup   filePopup
	vimMode     bool
	vimPendingG bool
	tools       *toolTree
	toolStart   time.Time
	sidebar     *sidebar
	spinner     spinner.Model

	messages         []chatMessage
	streaming        string
	busy             bool
	thinking         bool
	thinkingText     string
	activeToolName   string // tool currently executing
	activeToolDetail string // e.g. file path
	turnStart        time.Time // when the current turn began (for response timing)
	cancel           context.CancelFunc
	events           <-chan event.Event
	tokenInfo        string
	tokensIn         int
	tokensOut        int
	costUSD          float64
	sessionStart     time.Time
	toolCounts       map[string]int
	gitProject       string
	gitBranch        string
	gitDirty         bool

	teamView       *teamView        // split-pane view for team mode
	wsView         *WorkspaceView   // workspace mode dashboard
	lastBell       time.Time        // bell cooldown (30s between rings)
	tasksTotal     int              // total tasks created
	tasksDone      int              // tasks completed
	activeTaskName string           // currently in-progress task name
	wfHeader       *workflowHeader  // phase breadcrumb for workflow mode
	wfEvents       <-chan orchestra.PhaseEvent // workflow event stream
	wfOverride     chan orchestra.OverrideCmd  // TUI → orchestra control
	wfRunning      bool
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
		projectRoot:     detectProjectRoot(),
		tools:           newToolTree(),
		sidebar:         newSidebar(theme),
		sessionStart:    time.Now(),
		toolCounts:      make(map[string]int),
		spinner:         newSpinner(theme),
		teamView:        newTeamView(),
		wfHeader:        &workflowHeader{},
		wfOverride:      make(chan orchestra.OverrideCmd, 4),
	}
}

func newSpinner(theme Theme) spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	s.Style = lipgloss.NewStyle().Foreground(theme.Primary)
	return s
}

func (a *App) Init() tea.Cmd {
	a.gitProject, a.gitBranch, a.gitDirty = detectGitInfo()
	return tea.Batch(textarea.Blink, a.spinner.Tick)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		var cmd tea.Cmd
		a.viewport, cmd = a.viewport.Update(msg)
		return a, cmd
	case spinner.TickMsg:
		if a.busy {
			var cmd tea.Cmd
			a.spinner, cmd = a.spinner.Update(msg)
			return a, cmd
		}
	case tea.KeyMsg:
		// Global scroll keys that work even while busy/typing
		switch msg.String() {
		case "ctrl+up":
			a.viewport.LineUp(3)
			return a, nil
		case "ctrl+down":
			a.viewport.LineDown(3)
			return a, nil
		case "pgup":
			a.viewport.HalfViewUp()
			return a, nil
		case "pgdown":
			a.viewport.HalfViewDown()
			return a, nil
		}
		if model, cmd, handled := a.handleKey(msg); handled {
			return model, cmd
		}
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// Split: sidebar gets 25% on wide terminals, hidden on narrow
		sidebarWidth := 0
		mainWidth := msg.Width
		if msg.Width >= 100 {
			sidebarWidth = msg.Width / 4
			if sidebarWidth > 30 {
				sidebarWidth = 30
			}
			mainWidth = msg.Width - sidebarWidth
		}
		// Height: header(1) + sep(1) + body + HUD(2) + input(3) = 7 lines overhead
		a.viewport = viewport.New(mainWidth, max(1, msg.Height-7))
		a.mdRenderer = NewMarkdownRenderer(mainWidth - 4)
		a.sidebar.SetSize(sidebarWidth, max(1, msg.Height-6))
		a.teamView.SetSize(msg.Width, max(1, msg.Height-6))
		if a.wsView != nil {
			a.wsView.SetSize(msg.Width, max(1, msg.Height-6))
		}
		if a.wfHeader != nil {
			a.wfHeader.width = msg.Width
		}
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
	case wfEventMsg:
		return a.handleWorkflowEvent(orchestra.PhaseEvent(msg))
	case wfDoneMsg:
		a.wfRunning = false
		a.busy = false
		a.teamView.Stop()
		a.appendInfo("[workflow] Complete.")
		return a, nil
	case stuckToolMsg:
		// Only show notification if the same tool is still running
		if a.activeToolName == msg.name && a.toolStart == msg.startedAt && a.busy {
			elapsed := time.Since(msg.startedAt).Truncate(time.Second)
			a.activeToolDetail = fmt.Sprintf("running for %s", elapsed)
		}
		return a, nil
	case workspacePollMsg:
		return a, a.handleWorkspacePoll()
	case workspaceTransitionMsg:
		a.appendInfo(fmt.Sprintf("[workspace] Status → %s", msg.Status))
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
		a.updateFilePopup()
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

	// Route to file popup when visible.
	if a.filePopup.visible {
		return a.handleFilePopupKey(msg)
	}

	// Vim mode: viewport navigation when input is blurred.
	if a.vimMode && !a.busy {
		return a.handleVimModeKey(msg)
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

	// Workspace override keys (when workspace view is active)
	if a.wsView != nil && a.wsView.IsActive() {
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
			// Smart Tab: slash completion when typing /, focus cycling otherwise
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
			// Pre-fill input with /send <focused-role> for quick message
			focused := a.wsView.FocusedRole()
			if focused != "" {
				a.input.SetValue(fmt.Sprintf("/send %s ", focused))
				a.input.Focus()
			} else {
				a.appendInfo("No agent focused. Press Tab to cycle, then Ctrl+S.")
			}
			return a, nil, true
		}
	}

	// Workflow override keys
	if a.wfRunning {
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
	}

	switch msg.String() {
	case "ctrl+k":
		a.togglePalette()
		return a, nil, true
	case "ctrl+a":
		a.toggleSessionSwitcher()
		return a, nil, true
	case "esc":
		if a.filePopup.visible {
			a.dismissFilePopup()
			return a, nil, true
		}
		if a.busy {
			if a.cancel != nil {
				a.cancel()
			}
			a.busy = false
			return a, nil, true
		}
		// Enter vim mode instead of quitting on first Esc.
		if !a.vimMode && strings.TrimSpace(a.startupPrompt) == "" {
			a.vimMode = true
			a.input.Blur()
			return a, nil, true
		}
		return a, tea.Quit, true
	case "a", "1":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.beginSetup("anthropic")
			return a, nil, true
		}
	case "tab":
		if !a.busy {
			if a.trySlashComplete() {
				return a, nil, true
			}
		}
		return a, nil, true
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
		if a.busy {
			// Cancel current generation (like Claude Code)
			if a.cancel != nil {
				a.cancel()
			}
			a.busy = false
			a.streaming = ""
			a.appendInfo("[cancelled]")
			return a, nil, true
		}
		// When idle, copy last assistant response to clipboard via OSC 52
		if last := a.lastAssistantMessage(); last != "" {
			copyToClipboard(last)
			a.appendInfo("[copied last response to clipboard]")
			return a, nil, true
		}
		return a, tea.Quit, true
	case "o", "2":
		if strings.TrimSpace(a.startupPrompt) != "" {
			a.beginSetup("openai")
			return a, nil, true
		}
	}
	return a, nil, false
}

// handleFilePopupKey routes keys when the file completion popup is visible.
func (a *App) handleFilePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "tab", "enter":
		a.acceptFileCompletion()
		return a, nil, true
	case "esc":
		a.dismissFilePopup()
		return a, nil, true
	case "down", "ctrl+n":
		a.filePopupMoveDown()
		return a, nil, true
	case "up", "ctrl+p":
		a.filePopupMoveUp()
		return a, nil, true
	default:
		// Pass through to input, then re-evaluate popup.
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		a.updateFilePopup()
		return a, cmd, true
	}
}

// handleVimModeKey routes keys in vim navigation mode.
func (a *App) handleVimModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "i", "a", "enter":
		// Exit vim mode, re-focus input.
		a.vimMode = false
		a.vimPendingG = false
		a.input.Focus()
		return a, nil, true
	case "esc":
		// Second Esc in vim mode quits.
		return a, tea.Quit, true
	case "ctrl+c":
		return a, tea.Quit, true
	case "ctrl+k":
		a.togglePalette()
		return a, nil, true
	case "ctrl+a":
		a.toggleSessionSwitcher()
		return a, nil, true
	default:
		if a.handleVimKey(msg) {
			return a, nil, true
		}
		return a, nil, false
	}
}


// Setup, welcome, auth functions are in app_setup.go
// Event handling is in app_events.go

// handleEvent is in app_events.go

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	// Rich header
	logo := lipgloss.NewStyle().
		Foreground(a.theme.Primary).
		Bold(true).
		Render("⌬ altcode")
	meta := lipgloss.NewStyle().
		Foreground(a.theme.Muted).
		Render("  " + a.headerMeta())
	header := logo + meta

	sep := lipgloss.NewStyle().
		Foreground(a.theme.Border).
		Render(strings.Repeat("─", a.width))

	// All status is in the bottom bar now — no duplicate activity text here

	inputView := a.input.View()
	if a.setupProvider != "" {
		inputView = a.setupInput.View()
	}
	if a.vimMode {
		inputView = lipgloss.NewStyle().
			Foreground(a.theme.Warning).
			Bold(true).
			Render("-- NORMAL --") +
			lipgloss.NewStyle().
				Foreground(a.theme.Muted).
				Render("  (i to insert, Esc to quit)")
	}

	popupView := a.filePopupView()

	mainBody := a.viewport.View()
	if a.palette.IsVisible() {
		mainBody = a.palette.View()
	} else if a.sessionSwitcher.IsVisible() {
		mainBody = a.sessionSwitcher.View()
	}

	// Workspace view: dashboard display when workspace mode is active
	if a.wsView != nil && a.wsView.IsActive() {
		mainBody = a.wsView.Render(a.theme)
	}

	// Team view: split-pane display when team mode is active
	// NOTE: SetSize is called in Update's WindowSizeMsg handler, not here.
	// View() must be a pure render function (no state mutation).
	if a.teamView.IsActive() || a.wfRunning {
		panes := a.teamView.Render(a.theme)
		if a.wfRunning && len(a.wfHeader.phases) > 0 {
			mainBody = a.wfHeader.Render(a.theme) + "\n" + panes
		} else {
			mainBody = panes
		}
	}

	// Side-by-side: main content | sidebar (if wide enough)
	body := mainBody
	if a.sidebar.width > 0 && !a.teamView.IsActive() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, mainBody, a.sidebar.View())
	}

	// Rich status bar at the bottom
	model := ""
	if a.engine != nil {
		model = a.engine.Config().Model
	}
	toolActive := ""
	if a.activeToolName != "" {
		toolActive = a.activeToolName
		if a.activeToolDetail != "" {
			// CC style: "Bash: go test ./..." for targets,
			// "Bash (running for 35s)" for stuck warnings
			if strings.HasPrefix(a.activeToolDetail, "running for") {
				toolActive += " (" + a.activeToolDetail + ")"
			} else {
				toolActive += ": " + a.activeToolDetail
			}
		}
	}
	if toolActive == "" && a.busy {
		if a.thinking {
			toolActive = "thinking"
		} else {
			toolActive = "streaming"
		}
	}
	info := statusBarInfo{
		Model:      model,
		TokensIn:   a.tokensIn,
		TokensOut:  a.tokensOut,
		CostUSD:    a.costUSD,
		ToolActive: toolActive,
	}
	ctxLimit := 128000
	if a.engine != nil {
		ctxLimit = a.engine.ContextWindowSize()
	}
	// Count config items for HUD display (Claude Code parity)
	claudeMDCount, mcpCount, hooksCount := 0, 0, 0
	if a.engine != nil {
		claudeMDCount = len(a.engine.Instructions())
		cfg := a.engine.Config()
		mcpCount = len(cfg.MCP)
		for _, matchers := range cfg.Hooks {
			hooksCount += len(matchers)
		}
	}
	hs := hudState{
		// API-reported input tokens = total context seen by model on last request.
		// Add output tokens as they become part of context on next turn.
		ContextTokens: a.tokensIn + a.tokensOut,
		ContextLimit:  ctxLimit,
		SessionStart:  a.sessionStart,
		GitProject:    a.gitProject,
		GitBranch:     a.gitBranch,
		GitDirty:      a.gitDirty,
		ClaudeMDCount: claudeMDCount,
		MCPCount:      mcpCount,
		HooksCount:    hooksCount,
		ToolCounts:     a.toolCounts,
		TasksTotal:     a.tasksTotal,
		TasksDone:      a.tasksDone,
		ActiveTaskName: a.activeTaskName,
	}
	spinView := ""
	if a.busy {
		spinView = a.spinner.View()
	}
	statusBar := renderHUD(hs, info, a.theme, a.width, a.vimMode, spinView)

	result := fmt.Sprintf("%s\n%s\n%s\n%s\n%s",
		header, sep, body, statusBar, inputView)
	if popupView != "" {
		result += "\n" + popupView
	}
	return result
}

func (a *App) submit() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	a.input.Reset()
	a.messages = append(a.messages, chatMessage{role: roleUser, content: text})
	a.streaming = ""

	if handled, cmd := a.handleBuiltinCommand(text); handled {
		if cmd == nil {
			a.busy = false
		}
		return cmd
	}

	a.busy = true
	a.thinking = true
	a.thinkingText = ""
	a.turnStart = time.Now()

	text = a.expandSlashCommand(text)
	a.updateViewport()

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.events = a.engine.Run(ctx, text)

	return tea.Batch(a.waitForEvent(), a.spinner.Tick)
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
		sb.WriteString(a.renderMessage(m))
		sb.WriteString("\n")
	}
	// Show live tool tree during tool execution
	if len(a.tools.entries) > 0 {
		sb.WriteString(a.tools.Render(a.theme, a.width-6))
	}
	if a.streaming != "" {
		sb.WriteString(a.renderMessage(chatMessage{role: roleAssistant, content: a.streaming}))
	}
	a.viewport.SetContent(sb.String())
	a.viewport.GotoBottom()
}

