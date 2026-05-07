package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/command"
	"github.com/jiayaoqijia/altcode/internal/engine"
	"github.com/jiayaoqijia/altcode/internal/event"
	"github.com/jiayaoqijia/altcode/internal/orchestra"
	"github.com/jiayaoqijia/altcode/internal/workspace"
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

	filePopup    filePopup
	vimMode      bool
	vimPendingG  bool
	sessionTitle string // Display label set by /rename
	tools        *toolTree
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
	tokensIn             int64
	tokensOut            int64
	currentContextTokens int64 // last-turn input (not cumulative)
	costUSD              float64
	sessionStart     time.Time
	sessionSlug      string
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
	turnToolCount  int              // tools used this turn (for summary)
	turnWrites     int              // files written this turn
	turnReads      int              // files read this turn
	turnBashes     int              // commands run this turn
	turnCostStart  float64          // cost at turn start (for delta)
	turnTokenStart int64            // tokens at turn start (for delta)
	cachedTokens   int64            // last turn's cached prompt tokens (HUD chip)
	lastFrame      time.Time        // last viewport-render timestamp (frame rate limiter — DS-TUI parity)
	// queue is the type-ahead FIFO for prompts entered while the
	// current turn is running. Drained by drainQueue() in onDone().
	// DeepSeek-TUI parity for `/queue` workflow.
	queue          []string
	// lastSubmittedText + lastSubmittedAt drive the 500ms double-submit
	// dedup. tmux send-keys can replay the same prompt under bracketed-
	// paste / TIOCSTI races, burning the user's cost on a duplicate turn.
	lastSubmittedText string
	lastSubmittedAt   time.Time
	// permDialog shows a modal asking the user to approve a tool call.
	// Wired only when ALTCODE_REQUIRE_APPROVAL=1 is set; otherwise the
	// existing auto-allow path stays. DS-TUI parity. The pending PermReq
	// holds the response channel until the user picks y/n/a/!.
	permDialog        *PermissionDialog
	pendingPermission *event.PermReq
	prevContentLen int              // viewport content length (kept for backward-compat with tests)
	// renderCache holds the rendered string prefix for the first
	// renderCacheLen messages. Append-only message lists (the common
	// case — see grep of `a.messages =` assignments) extend this
	// cache instead of re-rendering every message on every event.
	// Karpathy autoresearch UI-perf metric drop on 1000-msg sessions:
	// 66.7ms → 0.5ms per updateViewport once the prefix is cached.
	renderCache    string
	renderCacheLen int
	// autoAllowSeen tracks which tool names have already had their
	// "[auto-allow]" info note shown this session. Prevents the same
	// 1-line note from repeating once per tool call (a 6-bash turn
	// used to print 6 identical lines drowning the real tool tree).
	autoAllowSeen map[string]bool
	// userScrolledAway tracks whether the user has actively scrolled the
	// viewport away from the bottom. Set on pgup / ctrl+up, cleared on
	// pgdown-to-bottom or on prompt submit. Iteration-2 of autoresearch
	// loop: replaces the fragile content-length heuristic that could
	// GotoTop mid-conversation when the tool tree collapsed.
	userScrolledAway bool
	inputHistory   *inputHistory    // prompt history for up/down recall
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
	projectRoot := detectProjectRoot()
	tt := newToolTree()
	tt.projectRoot = projectRoot

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
		projectRoot:     projectRoot,
		tools:           tt,
		sidebar:         newSidebar(theme),
		sessionStart:    time.Now(),
		sessionSlug:     generateSessionSlug(),
		inputHistory:    newPersistentInputHistory(DefaultHistoryPath()),
		toolCounts:      make(map[string]int),
		spinner:         newSpinner(theme),
		teamView:        newTeamView(),
		wfHeader:        &workflowHeader{},
		wfOverride:      make(chan orchestra.OverrideCmd, 4),
		permDialog:      NewPermissionDialog(theme),
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
		// Route mouse to workspace panes when active
		if a.wsView != nil && a.wsView.IsActive() {
			switch msg.Action {
			case tea.MouseActionPress:
				a.wsView.FocusByClick(msg.X, msg.Y)
			case tea.MouseActionMotion:
				// Scroll wheel: Button 4=up, 5=down in some terminals
				if msg.Button == tea.MouseButtonWheelUp {
					a.wsView.ScrollPane(-3)
				} else if msg.Button == tea.MouseButtonWheelDown {
					a.wsView.ScrollPane(3)
				}
			}
			return a, nil
		}
		var cmd tea.Cmd
		a.viewport, cmd = a.viewport.Update(msg)
		return a, cmd
	case spinner.TickMsg:
		if a.busy {
			var cmd tea.Cmd
			a.spinner, cmd = a.spinner.Update(msg)
			// Re-render viewport while a tool is running so its
			// elapsed time label actually ticks. Without this the
			// tree freezes at "⟳ bash" until the next engine event.
			//
			// Frame rate limiter (DeepSeek-TUI parity): cap the
			// re-render rate so the spinner ticking doesn't burn
			// CPU at the spinner's natural ~10Hz cadence. ~30 FPS
			// (33ms) is fast enough that the elapsed-time label
			// looks live, slow enough to drop the per-tick render
			// cost from a 1000-msg viewport rebuild to roughly
			// 1/3 the cost.
			if a.tools.HasRunning() && time.Since(a.lastFrame) > 33*time.Millisecond {
				a.updateViewport()
				a.lastFrame = time.Now()
			}
			return a, cmd
		}
	case tea.KeyMsg:
		// Global scroll keys that work even while busy/typing
		switch msg.String() {
		case "ctrl+up":
			a.viewport.LineUp(3)
			a.userScrolledAway = !a.viewport.AtBottom()
			return a, nil
		case "ctrl+down":
			a.viewport.LineDown(3)
			a.userScrolledAway = !a.viewport.AtBottom()
			return a, nil
		case "pgup":
			a.viewport.HalfViewUp()
			a.userScrolledAway = !a.viewport.AtBottom()
			return a, nil
		case "pgdown":
			a.viewport.HalfViewDown()
			// If pgdown landed us at the bottom, the user has
			// re-engaged auto-follow — clear the flag.
			a.userScrolledAway = !a.viewport.AtBottom()
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
		a.mdRenderer = NewMarkdownRenderer(max(10, mainWidth-4))
		a.sidebar.SetSize(sidebarWidth, max(1, msg.Height-6))
		a.teamView.SetSize(msg.Width, max(1, msg.Height-6))
		if a.wsView != nil {
			a.wsView.SetSize(msg.Width, max(1, msg.Height-6))
		}
		if a.wfHeader != nil {
			a.wfHeader.SetWidth(msg.Width)
		}
		a.input.SetWidth(max(1, msg.Width-2))
		a.setupInput.Width = max(1, msg.Width-2)
		// Overlays (palette + session switcher) render into mainBody,
		// which lives inside mainWidth — NOT the full terminal width.
		// Passing msg.Width makes the rounded border spill into the
		// sidebar column and stack '╮│' / '╯│' at the right edge.
		a.palette.SetWidth(mainWidth)
		a.sessionSwitcher.SetWidth(mainWidth)
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
	case workspaceDetectedMsg:
		return a, a.handleWorkspaceDetected(msg)
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

	// Always allow textarea input — user needs to type /spawn, /send, /quit
	// even during workspace mode (a.busy=true). Only the engine-submit is
	// blocked by busy, not the typing itself.
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	if !a.busy {
		a.updateFilePopup()
	}
	return a, cmd
}


// handleKey, handleSetupKey, handleWorkspaceKey, handleWorkflowKey,
// handleGlobalKey, handleEscKey, handleEnterKey, handleCtrlCKey
// are all in app_keys.go


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
		// Stay in vim mode — don't quit. Use Ctrl+C to quit or
		// `i`/`a` to return to insert mode. Ctrl+D is the standard
		// vim half-page-down binding (handled below) so it does NOT
		// quit, despite what the previous comment claimed.
		return a, nil, true
	case "ctrl+c":
		if a.cancel != nil {
			a.cancel()
		}
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


// View, renderHeader, renderInputArea, renderMainBody, buildToolActive,
// buildHUDState, renderStatusSection are all in app_view.go


func (a *App) submit() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	a.input.Reset()
	a.inputHistory.Add(text)
	a.inputHistory.Reset()

	// Double-submit guard: dedup the same text fired within 500ms of
	// the previous submit. Surfaced by a 3-way live comparison
	// (CC vs altcode vs Codex) — bracketed-paste replay or tmux
	// TIOCSTI race could fire submit() twice with identical text and
	// burn the user's cost on a duplicate turn. Slash commands and
	// empty-text submits skip the guard so /clear /clear behaves
	// normally and idle-Enter doesn't get suppressed.
	if text != "" && !strings.HasPrefix(text, "/") {
		if a.lastSubmittedText == text && time.Since(a.lastSubmittedAt) < 500*time.Millisecond {
			a.appendInfo("[debounce] dropped duplicate submit within 500ms")
			a.updateViewport()
			return nil
		}
		a.lastSubmittedText = text
		a.lastSubmittedAt = time.Now()
	}

	// Type-ahead queue (DeepSeek-TUI parity): if a turn is already
	// running, push this prompt onto a FIFO queue and surface a
	// non-blocking info note. The queue is drained automatically
	// when onDone() fires for the in-flight turn — see drainQueue().
	// Slash commands still execute immediately because they're
	// metadata, not LLM turns.
	if a.busy && !strings.HasPrefix(text, "/") {
		a.queue = append(a.queue, text)
		a.appendInfo(fmt.Sprintf("[queue] +1 — %d prompt(s) waiting; will run after the current turn",
			len(a.queue)))
		a.updateViewport()
		return nil
	}

	a.messages = append(a.messages, chatMessage{role: roleUser, content: text})
	a.streaming = ""
	// Submitting a new prompt re-engages auto-follow. Autoresearch
	// iteration 2: the user asking a new question means they want
	// to see the response, regardless of where they were scrolled.
	a.userScrolledAway = false
	// Reset per-turn counters for the new turn's summary
	a.turnToolCount = 0
	a.turnWrites = 0
	a.turnReads = 0
	a.turnBashes = 0
	a.turnCostStart = a.costUSD
	a.turnTokenStart = a.tokensIn + a.tokensOut

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
		// Reset the render cache too — otherwise a stale prefix
		// from the previous conversation would leak in on the next
		// updateViewport that has at least one new message.
		a.renderCache = ""
		a.renderCacheLen = 0
		a.viewport.SetContent(a.welcomeView())
		a.viewport.GotoTop()
		return
	}

	// Incremental render: keep the prefix string for the first
	// renderCacheLen messages and only render the appended tail.
	// Cache invalidates when len(messages) shrinks (which only
	// happens on /clear or session reset). Karpathy autoresearch
	// UI iter: 1000-msg viewport drops from 66ms to <1ms.
	var sb strings.Builder
	cacheValid := a.renderCacheLen <= len(a.messages)
	if !cacheValid {
		// Conversation shrank (clear / reset) — full rebuild and
		// reset the cache.
		a.renderCache = ""
		a.renderCacheLen = 0
	}
	if a.renderCacheLen > 0 {
		sb.WriteString(a.renderCache)
	}
	if a.renderCacheLen < len(a.messages) {
		var tail strings.Builder
		for _, m := range a.messages[a.renderCacheLen:] {
			tail.WriteString(a.renderMessage(m))
			tail.WriteByte('\n')
		}
		tailStr := tail.String()
		sb.WriteString(tailStr)
		// Promote tail into the cache so the next call sees it as
		// part of the stable prefix.
		a.renderCache += tailStr
		a.renderCacheLen = len(a.messages)
	}
	// Show live tool tree during tool execution — NO collapsing to prevent height jumps
	if len(a.tools.entries) > 0 {
		sb.WriteString(a.tools.RenderLive(a.theme, max(10, a.width-6)))
	}
	// CC-style thinking indicator — show whenever busy and not streaming.
	// Using a.busy (not a.thinking) prevents the indicator from flickering
	// off between consecutive tool calls when a.thinking briefly toggles.
	if a.busy && a.streaming == "" {
		sb.WriteString(a.renderThinkingIndicator())
	}
	if a.streaming != "" {
		sb.WriteString(a.renderMessage(chatMessage{role: roleAssistant, content: a.streaming}))
	}

	newContent := sb.String()
	a.viewport.SetContent(newContent)
	// Scroll behaviour:
	//  - content grew  → GotoBottom so new output is visible
	//  - content shrank drastically (e.g. /clear) → GotoTop so the
	//    viewport doesn't linger on a YOffset that's now past EOF
	//  - otherwise hold position to prevent visual jumping when the
	//    live tool tree toggles on/off at similar height.
	// Auto-scroll to bottom UNLESS the user has explicitly scrolled
	// away from the bottom. This replaces the old content-length
	// heuristic (which would unexpectedly GotoTop when the tool tree
	// collapsed after a fast tool call, losing the user's place).
	// Iteration-2 autoresearch fix based on CC baseline finding.
	if !a.userScrolledAway {
		a.viewport.GotoBottom()
	}
	a.prevContentLen = len(newContent)
}
