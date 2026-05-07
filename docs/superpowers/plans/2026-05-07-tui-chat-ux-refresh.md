# TUI Chat UX Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the default Bubble Tea chat TUI into a quieter, friendlier Claude Code / Gemini CLI style experience without changing engine or workspace backend semantics.

**Architecture:** Keep the work inside `internal/tui` and treat the first release as a chat-shell refresh. The shell owns top chrome, viewport sizing, composer, status line, and sidebar visibility; content renderers own message flow, tool traces, welcome, help, and palette grouping. Workspace/team views keep their current interaction model and receive only visual-language alignment/regression coverage in the final slice.

**Tech Stack:** Go, Bubble Tea, Bubbles `textarea`/`viewport`/`textinput`, Lip Gloss, existing `teatest` render tests, tmux manual E2E.

---

## Progress Tracker

| Stage | Status | Notes |
| --- | --- | --- |
| Grill decisions | Done | User approved low-distraction chat, bordered composer, compact tool trace, contextual welcome, grouped help/palette, default hidden metadata, and TUI test coverage. |
| Codex CLI design challenge | Done | `codex exec --dangerously-bypass-approvals-and-sandbox` reviewed core TUI files; incorporated warnings about resize cache invalidation and palette cursor risk. |
| Plan document | Done | This file is the live implementation tracker. Update checkboxes as each step lands. |
| Slice 1: Shell | Done | Passed Implementer, Spec Reviewer, Code Quality Reviewer, and Test Verifier. |
| Slice 2: Content | Done | Passed Implementer, Spec Reviewer, Code Quality Reviewer, and Test Verifier. |
| Slice 3: Polish/Test | Done | Passed workspace/team non-regression, responsive density coverage, tmux smoke, headless checks, and full pre-push gate. |

## Locked Decisions

- Prioritize the default single-agent chat UI; workspace/team gets visual consistency only.
- Default information density is low: conversation first, single-line status, details available through commands.
- Input becomes an elegant bordered composer, default compact and growing with multiline content.
- Tool traces are compact by default; running tools stay live, errors/diffs/test output auto-expand.
- Messages use a lightweight text flow; heavy borders are reserved for attention states.
- Top chrome shows identity context only: product, model, repo/branch, and small mode badge.
- Sidebar is hidden by default in chat, even on wide terminals.
- `Ctrl+K` command palette is the discovery surface; slash commands remain the fast path.
- Empty state is separately designed, with contextual hints. `/init` is recommended for new projects but never required.
- Metadata is hidden by default and can be enabled through a session-level slash command.
- Busy draft mode remains: users can type while a turn runs, but only control slash commands execute immediately.
- `Esc` behavior remains for this slice; vim mode messaging becomes clearer.
- TUI changes must include render tests, narrow viewport coverage, and at least one tmux E2E pass.

## File Map

- Modify `internal/tui/app_view.go`: top-level shell composition, compact header, composer renderer, status placement.
- Modify `internal/tui/app.go`: resize layout math, input height updates, sidebar default visibility, render-cache invalidation on width changes.
- Modify `internal/tui/helpers.go`: context-aware placeholder helpers and project-signal helpers if they can stay UI-only.
- Modify `internal/tui/app_setup.go`: contextual ready/setup welcome screens.
- Modify `internal/tui/messages.go`: lightweight user/assistant/info/trace rendering and metadata visibility.
- Modify `internal/tui/app_event_handlers.go`: final completion marker and persisted compact trace role.
- Modify `internal/tui/tooltree.go`: compact persisted tool trace renderer while preserving live renderer behavior.
- Modify `internal/tui/hud.go`: keep existing detailed HUD helpers available for tests/commands, add compact status rendering for default shell.
- Modify `internal/tui/palette.go`: grouped palette layout with non-selectable section labels.
- Modify `internal/tui/overlay.go`: command group metadata and common ordering.
- Modify `internal/tui/commands.go`: grouped `/help`, `/keymap`, and session-level metadata toggle command.
- Modify `internal/tui/workspace_view.go`, `internal/tui/agent_pane.go`, `internal/tui/team_view.go`: final visual alignment only if needed after shell/content changes.
- Modify tests in `internal/tui/app_test.go`, `internal/tui/tui_view_test.go`, `internal/tui/tooltree_test.go`, `internal/tui/palette_helpers_test.go`, `internal/tui/overlay_test.go`, `internal/tui/workspace_view_test.go`, and `internal/tui/event_helpers_test.go`.

## Slice 1: Shell

**Intent:** Make the default frame quiet and product-grade before touching content rendering.

**Files:**
- Modify: `internal/tui/app_view.go`
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/helpers.go`
- Modify: `internal/tui/hud.go`
- Test: `internal/tui/tui_view_test.go`
- Test: `internal/tui/app_test.go`
- Test: `internal/tui/overlay_test.go`

- [x] **Step 1: Add shell render tests**

Add tests covering the new default frame before implementation:

```go
func TestTUIView_DefaultChatShellUsesQuietChrome(t *testing.T) {
	a := testApp()
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	out := stripANSI(a.View())
	for _, want := range []string{"altcode", "chat", "Ask anything"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in default shell:\n%s", want, out)
		}
	}
	if strings.Count(out, "tokens") > 1 {
		t.Fatalf("default shell should not render detailed HUD tokens:\n%s", out)
	}
}

func TestTUIView_DefaultChatHidesWideSidebar(t *testing.T) {
	a := testApp()
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	a.sidebar.AddFile("internal/tui/app.go", 1, 0)

	out := stripANSI(a.View())
	if strings.Contains(out, "Files") {
		t.Fatalf("chat shell should hide sidebar by default:\n%s", out)
	}
	if got := a.mainBodyWidth(); got != 140 {
		t.Fatalf("mainBodyWidth = %d, want full chat width 140", got)
	}
}

func TestTUIView_ComposerBorderAndBusyStatus(t *testing.T) {
	a := testApp()
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	a.busy = true
	a.activeToolName = "Bash"
	a.activeToolDetail = "go test ./..."

	out := stripANSI(a.View())
	for _, want := range []string{"running", "Bash", "go test"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in busy composer:\n%s", want, out)
		}
	}
}
```

Run:

```bash
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1 -run 'TestTUIView_DefaultChatShellUsesQuietChrome|TestTUIView_DefaultChatHidesWideSidebar|TestTUIView_ComposerBorderAndBusyStatus|TestApp' -v
```

Expected: FAIL until the shell helpers are implemented.

- [x] **Step 2: Centralize chat layout sizing**

Add small helpers in `internal/tui/app_view.go` or `internal/tui/app.go` so viewport height is not tied to the old fixed HUD/input sizes:

```go
func (a *App) composerHeight() int {
	if a.setupProvider != "" {
		return 1
	}
	if a.vimMode {
		return 3
	}
	lines := a.input.LineCount()
	if lines < 1 {
		lines = 1
	}
	maxLines := 5
	if a.height < 18 {
		maxLines = 3
	}
	if lines > maxLines {
		lines = maxLines
	}
	return lines + 2 // composer border
}

func (a *App) chromeHeight() int {
	statusRows := 1
	headerRows := 1
	return headerRows + statusRows + a.composerHeight()
}
```

Update `tea.WindowSizeMsg` handling in `internal/tui/app.go`:

```go
mainWidth := msg.Width
sidebarWidth := 0
if a.chatSidebarVisible() {
	sidebarWidth = msg.Width / 4
	if sidebarWidth > 30 {
		sidebarWidth = 30
	}
	mainWidth = msg.Width - sidebarWidth
}

a.input.SetWidth(max(1, mainWidth-4))
a.input.SetHeight(max(1, a.composerHeight()-2))
a.setupInput.Width = max(1, mainWidth-4)
a.viewport = viewport.New(mainWidth, max(1, msg.Height-a.chromeHeight()))
a.mdRenderer = NewMarkdownRenderer(max(10, mainWidth-4))
a.sidebar.SetSize(sidebarWidth, max(1, msg.Height-2))
a.palette.SetWidth(mainWidth)
a.sessionSwitcher.SetWidth(mainWidth)
a.renderCache = ""
a.renderCacheLen = 0
a.updateViewport()
```

Add:

```go
func (a *App) chatSidebarVisible() bool {
	return false
}
```

This preserves sidebar data collection but hides it from default chat.

- [x] **Step 3: Render compact header and status**

Replace the old header/separator/two-line HUD composition with a quiet shell:

```go
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}
	if a.height < 4 {
		return "altcode (terminal too small)"
	}

	header := a.renderHeader()
	body := a.renderMainBody()
	status := a.renderCompactStatus()
	input := a.renderInputArea()
	popup := a.filePopupView()

	result := strings.Join([]string{header, body, status, input}, "\n")
	if popup != "" {
		result += "\n" + popup
	}
	return result
}
```

Add compact status text:

```go
func (a *App) renderCompactStatus() string {
	state := "ready"
	if active := a.buildToolActive(); active != "" {
		state = "running " + active
	} else if a.busy {
		state = "thinking"
	}
	if a.wsView != nil && a.wsView.IsActive() {
		state = "workspace"
	}
	if a.wfRunning {
		state = "workflow"
	}
	return lipgloss.NewStyle().
		Foreground(a.theme.Muted).
		Width(a.width).
		Render(state)
}
```

Keep `renderHUD` in `hud.go` for focused tests and later detailed-display commands; do not delete it in this slice.

- [x] **Step 4: Render the bordered composer**

Update `renderInputArea()`:

```go
func (a *App) renderInputArea() string {
	if a.setupProvider != "" {
		return a.setupInput.View()
	}
	if a.vimMode {
		return a.renderComposerText("-- NORMAL --", "i insert · Ctrl+D quit")
	}
	return a.renderComposer(a.input.View())
}

func (a *App) renderComposer(content string) string {
	status := "Enter send · Ctrl+J newline · Ctrl+K commands"
	if a.busy {
		status = "running · /stop cancels · keep drafting"
	}
	border := a.theme.Border
	if a.input.Focused() {
		border = a.theme.Primary
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(1, a.mainBodyWidth()-2)).
		Render(content + "\n" + lipgloss.NewStyle().Foreground(a.theme.Muted).Render(status))
}
```

Use a small helper for vim mode text:

```go
func (a *App) renderComposerText(primary, secondary string) string {
	text := lipgloss.NewStyle().Foreground(a.theme.Warning).Bold(true).Render(primary)
	if secondary != "" {
		text += "  " + lipgloss.NewStyle().Foreground(a.theme.Muted).Render(secondary)
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(a.theme.Warning).
		Padding(0, 1).
		Width(max(1, a.mainBodyWidth()-2)).
		Render(text)
}
```

- [x] **Step 5: Make placeholder context-aware**

Keep credential placeholders unchanged, and add UI-only context helpers:

```go
func (a *App) updateInputPlaceholder() {
	if strings.TrimSpace(a.startupPrompt) != "" {
		a.input.Placeholder = normalInputPlaceholder(a.startupPrompt)
		return
	}
	if a.busy {
		a.input.Placeholder = "Draft the next prompt, or type /stop"
		return
	}
	if a.wsView != nil && a.wsView.IsActive() {
		a.input.Placeholder = "Type /send <agent> message or a control command"
		return
	}
	if !projectHasAgentContext(a.projectRoot) {
		a.input.Placeholder = "Ask anything, type /init, or attach @file"
		return
	}
	a.input.Placeholder = "Ask anything, attach @file, or press Ctrl+K"
}

func projectHasAgentContext(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}
```

Call `a.updateInputPlaceholder()` after size changes and after busy state transitions in UI handlers.

- [x] **Step 6: Run shell tests**

Run:

```bash
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1 -run 'TestTUIView_DefaultChatShellUsesQuietChrome|TestTUIView_DefaultChatHidesWideSidebar|TestTUIView_ComposerBorderAndBusyStatus|TestTUIView_NarrowWidth_NoNegative|TestTUIView_Height1_Fallback|TestApp' -v
```

Expected: PASS.

## Slice 2: Content

**Intent:** Make the chat transcript, tool trace, welcome screen, help, and palette match the new quiet shell.

**Files:**
- Modify: `internal/tui/messages.go`
- Modify: `internal/tui/app_event_handlers.go`
- Modify: `internal/tui/tooltree.go`
- Modify: `internal/tui/app_setup.go`
- Modify: `internal/tui/palette.go`
- Modify: `internal/tui/overlay.go`
- Modify: `internal/tui/commands.go`
- Test: `internal/tui/tui_view_test.go`
- Test: `internal/tui/tooltree_test.go`
- Test: `internal/tui/palette_helpers_test.go`
- Test: `internal/tui/overlay_test.go`
- Test: `internal/tui/app_test.go`
- Test: `internal/tui/event_helpers_test.go`

- [x] **Step 1: Add content render tests**

Add tests first:

```go
func TestRenderMessage_UsesLightweightFlow(t *testing.T) {
	a := testApp()
	a.width = 100

	user := stripANSI(a.renderMessage(chatMessage{role: roleUser, content: "hello"}))
	assistant := stripANSI(a.renderMessage(chatMessage{role: roleAssistant, content: "world"}))

	if strings.Contains(user, "┃") || strings.Contains(assistant, "┃") {
		t.Fatalf("messages should not use heavy left borders:\nuser=%q\nassistant=%q", user, assistant)
	}
	if !strings.Contains(user, "> hello") {
		t.Fatalf("user prompt prefix missing: %q", user)
	}
	if !strings.Contains(assistant, "world") {
		t.Fatalf("assistant content missing: %q", assistant)
	}
}

func TestToolTree_RenderCompact_ShowsImportantOutputOnly(t *testing.T) {
	tree := newToolTree()
	tree.Start("r1", "Read", "internal/tui/app.go")
	tree.Done("r1", "internal/tui/app.go", 5*time.Millisecond)
	tree.Start("b1", "Bash", "go test ./internal/tui")
	tree.DoneWithOutput("b1", "go test ./internal/tui", time.Second, "ok internal/tui 1.0s")

	out := stripANSI(tree.RenderCompact(DefaultTheme, 100))
	if !strings.Contains(out, "Read") || !strings.Contains(out, "Bash") {
		t.Fatalf("compact trace missing tool rows:\n%s", out)
	}
	if strings.Count(out, "⎿") > 1 {
		t.Fatalf("compact trace expanded too much output:\n%s", out)
	}
}

func TestBuiltinHelpText_Grouped(t *testing.T) {
	out := builtinHelpText()
	for _, want := range []string{"Chat", "Project", "Workspace", "Recovery", "/help", "/workspace"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in grouped help:\n%s", want, out)
		}
	}
	if strings.HasPrefix(out, "```") {
		t.Fatalf("help should not be a fenced log block:\n%s", out)
	}
}
```

Run:

```bash
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1 -run 'TestRenderMessage_UsesLightweightFlow|TestToolTree_RenderCompact_ShowsImportantOutputOnly|TestBuiltinHelpText_Grouped|TestPalette' -v
```

Expected: FAIL until content changes land.

- [x] **Step 2: Add trace role and metadata display flag**

Extend message roles:

```go
const (
	roleUser messageRole = iota
	roleAssistant
	roleTool
	roleInfo
	roleThinking
	roleTrace
)
```

Add UI state to `App`:

```go
showMessageMeta bool
lastCompletion  string
```

In `renderMessage`, only render `msg.meta` if `a.showMessageMeta` is true. Completion markers should use `lastCompletion` or turn summary, not per-message metadata by default.

- [x] **Step 3: Restyle messages**

Update `renderMessage` so user and assistant are prose-first:

```go
switch msg.role {
case roleUser:
	return lipgloss.NewStyle().
		Foreground(a.theme.Secondary).
		Width(width).
		Render("> " + rendered)
case roleAssistant:
	body := lipgloss.NewStyle().Foreground(a.theme.Foreground).Width(width).Render(rendered)
	if a.showMessageMeta && msg.meta != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(a.theme.Muted).Render(msg.meta)
	}
	return body
case roleInfo:
	return lipgloss.NewStyle().Foreground(a.theme.Muted).Width(width).Render("· " + rendered)
case roleTrace:
	return lipgloss.NewStyle().Foreground(a.theme.Muted).Width(width).Render(rendered)
case roleThinking:
	return lipgloss.NewStyle().Foreground(a.theme.Muted).Italic(true).Width(width).Render("thinking " + rendered)
}
```

Keep `roleTool` diff rendering for compatibility if existing tests rely on it.

- [x] **Step 4: Add compact persisted tool trace**

Add `RenderCompact` in `tooltree.go` and keep `RenderLive`/`Render` behavior available:

```go
func (t *toolTree) RenderCompact(theme Theme, width int) string {
	if len(t.entries) == 0 {
		return ""
	}
	items := collapseEntries(t.entries)
	return t.renderItemsCompact(items, theme, width)
}
```

Rules for `renderItemsCompact`:
- Completed `Read`, `Glob`, `Grep` rows stay one line unless they are errors.
- `Edit`, `Write`, `apply_patch` may show diff preview through `formatDiffOutput` with 4 lines.
- `Bash` shows output when output is non-empty and less than or equal to 4 non-empty lines.
- Errors always show output preview and use error color.

- [x] **Step 5: Persist final trace as trace role and completion marker**

Change `onDone()`:

```go
if len(a.tools.entries) > 0 {
	tree := a.tools.RenderCompact(a.theme, max(10, a.width-6))
	a.messages = append(a.messages, chatMessage{role: roleTrace, content: tree})
	a.tools.Clear()
}
```

Keep the turn summary but make it lightweight:

```go
if summary := a.buildTurnSummary(); summary != "" {
	a.lastCompletion = summary
	a.messages = append(a.messages, chatMessage{role: roleInfo, content: summary})
}
```

- [x] **Step 6: Redesign contextual welcome**

Replace the current ready welcome with a concise contextual panel:

```go
func (a *App) welcomeView() string {
	// setup success/error stays first
	// setupProvider keeps dedicated credential flow
	// startupPrompt keeps credential onboarding
	// ready state shows project/model and 2-3 actions
}
```

Ready-state content should include:
- `altcode ready`
- `project <name>` when `gitProject` or `projectRoot` is available
- `model <shortModel>`
- hints:
  - no `AGENTS.md`/`CLAUDE.md`: `/init  create project context`
  - always: `@file  attach context`
  - always: `Ctrl+K  commands`

Credential setup remains clear but shorter:
- `Connect provider`
- `Enter save · Esc back`
- `key is masked and saved to <config path>`

- [x] **Step 7: Group palette commands**

Extend `PaletteCommand`:

```go
type PaletteCommand struct {
	Name        string
	Description string
	Group       string
	Action      func() string
}
```

Populate groups in `buildPaletteCommands`:

```go
{Name: "/help", Group: "Chat", Description: "show help"},
{Name: "/status", Group: "Inspect", Description: "model and session status"},
{Name: "/init", Group: "Project", Description: "create project context"},
{Name: "/workspace", Group: "Workspace", Description: "start multi-agent workspace"},
{Name: "/undo", Group: "Recovery", Description: "git-backed undo"},
```

Update `Palette.View()` to render a muted group label before the first command in each group. Do not insert group labels into `p.filtered`; cursor indexes must remain command indexes.

- [x] **Step 8: Group `/help` and add metadata toggle**

Add a shared command catalog helper if it keeps `overlay.go` and `commands.go` in sync:

```go
type commandHelpRow struct {
	Group string
	Cmd   string
	Desc  string
}
```

`builtinHelpText()` should render plain grouped text rather than a fenced block:

```text
Chat
  /help      Show help
  /clear     Clear conversation

Project
  /init      Create project context
  /diff      Show changed files
```

Add `/metadata on|off` or `/display metadata on|off` as a session-level command:

```go
case "/metadata":
	a.appendInfo(a.builtinMetadataText(parts))
```

Implementation:

```go
func (a *App) builtinMetadataText(parts []string) string {
	if len(parts) < 2 {
		state := "off"
		if a.showMessageMeta {
			state = "on"
		}
		return "[metadata] " + state + " — use /metadata on or /metadata off"
	}
	switch strings.ToLower(parts[1]) {
	case "on":
		a.showMessageMeta = true
		return "[metadata] on"
	case "off":
		a.showMessageMeta = false
		return "[metadata] off"
	default:
		return "Usage: /metadata on|off"
	}
}
```

Update `slashCommandNames()` and `buildPaletteCommands()` with `/metadata`.

- [x] **Step 9: Run content tests**

Run:

```bash
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1 -run 'TestRenderMessage|TestToolTree|TestCCStyle|TestBuiltinHelpText|TestPalette|TestWelcome|TestBuildTurnSummary|TestPaletteBuiltins_MatchSlashCommandNames' -v
```

Expected: PASS.

## Slice 3: Polish And Verification

**Intent:** Make sure the refreshed chat shell does not regress workspace/team mode, narrow terminals, or required repo gates.

**Files:**
- Modify: `internal/tui/workspace_view.go` only if shell changes make spacing inconsistent.
- Modify: `internal/tui/agent_pane.go` only if focus/attention colors are too loud after shell refresh.
- Modify: `internal/tui/team_view.go` only if pane borders need the same quiet default.
- Test: `internal/tui/workspace_view_test.go`
- Test: `internal/tui/team_view_test.go`
- Test: `internal/tui/tui_view_test.go`

- [x] **Step 1: Add workspace/team non-regression tests**

Add tests to confirm existing dashboard behavior still works:

```go
func TestWorkspaceView_VisualLanguageStillShowsFocusAndBlocked(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01VISUAL",
		Task:   "review tui",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"reviewer": {
				Role:          "reviewer",
				Backend:       "codex",
				ActivityState: workspace.ActivityBlocked,
			},
		},
	}
	wv := NewWorkspaceView(sess)
	wv.SetSize(100, 20)
	wv.FocusAgent(0)

	out := stripANSI(wv.Render(DefaultTheme))
	for _, want := range []string{"workspace", "REVIEWER", "STUCK"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in workspace render:\n%s", want, out)
		}
	}
}
```

Run:

```bash
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1 -run 'TestWorkspaceView|TestTeamView|TestTUIView_NarrowWidth_NoNegative|TestTUIView_Height1_Fallback' -v
```

Expected: PASS.

- [x] **Step 2: Responsive density pass**

Check these terminal sizes through direct render tests:
- `40x12`: no panic, composer still visible, welcome collapses to essential hints.
- `80x24`: default target experience.
- `120x30`: no sidebar in default chat; palette centered in full chat width.
- `160x40`: conversation remains readable, header/status do not sprawl.

Add assertions to `internal/tui/tui_view_test.go`:

```go
func TestTUIView_ResponsiveDensity(t *testing.T) {
	for _, size := range []struct{ w, h int }{{40, 12}, {80, 24}, {120, 30}, {160, 40}} {
		a := testApp()
		a.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := stripANSI(a.View())
		if !strings.Contains(out, "altcode") {
			t.Fatalf("%dx%d missing header:\n%s", size.w, size.h, out)
		}
		if len(strings.Split(out, "\n")) > size.h+2 {
			t.Fatalf("%dx%d rendered too many rows", size.w, size.h)
		}
	}
}
```

- [x] **Step 3: Manual tmux E2E**

Build and smoke test the TUI in tmux:

```bash
GOFLAGS=-mod=mod go build -o /tmp/altcode-test ./cmd/altcode
tmux new-session -d -s altcode-tui-ux -x 120 -y 30 "/tmp/altcode-test"
sleep 3
tmux capture-pane -t altcode-tui-ux -p
tmux send-keys -t altcode-tui-ux C-k
sleep 1
tmux capture-pane -t altcode-tui-ux -p
tmux send-keys -t altcode-tui-ux Escape
tmux kill-session -t altcode-tui-ux
```

Verify captured output manually:
- Header is compact.
- Welcome is contextual and short.
- Composer border is visible.
- Palette opens, groups are readable, and no text overflows.
- No always-on right sidebar appears in chat.

- [x] **Step 4: Required TUI and headless checks**

Run:

```bash
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1 -run TestTUIView -v
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1
GOFLAGS=-mod=mod go build -o /tmp/altcode-test ./cmd/altcode
timeout 5 /tmp/altcode-test workspace "test" --dry-run
timeout 3 /tmp/altcode-test workspace list --json
timeout 3 /tmp/altcode-test workspace status
timeout 3 /tmp/altcode-test workspace spawn --help
```

Expected:
- TUI tests pass.
- Dry-run/status/help commands exit without hanging.
- `workspace list --json` returns valid JSON, usually `[]` in a clean environment.

- [x] **Step 5: Full pre-push gate**

Run the repo-required cleanup and gate before commit/push:

```bash
rm -f internal/main.go internal/stringxor.go internal/reverse_test.go
rm -rf internal/lru internal/middleware internal/stack internal/ratelimit stack/
GOFLAGS=-mod=mod go build ./...
GOFLAGS=-mod=mod go vet ./...
GOFLAGS=-mod=mod go test ./... -race -count=1 -timeout=180s
```

Expected: PASS.

## Review Pipeline

Each implementation slice must pass the repo's four-stage pipeline before the next slice starts:

1. Implementer: write tests, implement, self-review, commit if requested.
2. Spec Reviewer: compare against this plan and locked decisions.
3. Code Quality Reviewer: check rendering resilience, naming, width math, race risk, and test quality.
4. Test Verifier: run focused tests, add missing edge-case tests, and run the required gate.

If a reviewer finds issues, the implementer fixes them and that reviewer re-checks before proceeding.

## Verification Matrix

| Area | Required Evidence |
| --- | --- |
| Shell | Default chat shows compact header, bordered composer, one-line status, no sidebar. |
| Input | Single-line default, multiline grows within cap, busy draft mode is clear. |
| Messages | User/assistant flow has no heavy per-message borders; metadata hidden by default. |
| Tool Trace | Live trace remains stable; final trace compact; errors/diffs/test output visible. |
| Welcome | Ready/setup/new-project states are short, contextual, and actionable. |
| Palette | Groups render; filtering and cursor selection still target commands only. |
| Help | Grouped text, no fenced log block, includes workspace and recovery commands. |
| Workspace | Existing panes, focus, blocked/attention states, and footer hints still render. |
| Narrow Screens | 40-column and tiny-height views do not panic or overflow wildly. |

## Notes From Codex CLI Design Challenge

- Do not replace `toolTree.Render` immediately; add `RenderCompact` first so existing tests and live rendering remain stable.
- Invalidate `renderCache` on resize because cached rendered messages are width-dependent.
- Palette grouping must not insert selectable fake rows into `filtered`; render labels around command rows instead.
- Sidebar hiding affects `mainBodyWidth()` and overlay tests; update those expectations intentionally.
- Use `lipgloss.Width`, `ansi.Truncate`, and existing rune-safe helpers for display math; avoid byte-count truncation.
