package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/event"
	tea "github.com/charmbracelet/bubbletea"
)

// handleEvent routes a streaming event to the appropriate handler.
func (a *App) handleEvent(ev event.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case event.TextDelta:
		return a.onTextDelta(ev)
	case event.TextDone:
		a.thinking = false
		return a, a.waitForEvent()
	case event.ThinkingDelta:
		return a.onThinkingDelta(ev)
	case event.ToolStart:
		return a.onToolStart(ev)
	case event.ToolResultEvent:
		return a.onToolResult(ev)
	case event.UsageEvent:
		return a.onUsage(ev)
	case event.InfoEvent:
		return a.onInfo(ev)
	case event.ErrorEvent:
		return a.onError(ev)
	case event.PermissionRequest:
		return a.onPermissionRequest(ev)
	case event.Done:
		return a.onDone()
	}
	return a, a.waitForEvent()
}

// onPermissionRequest auto-allows the tool call and surfaces a brief
// info note. Without this handler the engine's askPermission select
// blocks forever — every ActionAsk-classified tool call would hang
// the agent loop indefinitely (the original 1+ hour TUI hang).
//
// Auto-allow is the right TUI default because:
//   1. Interactive users are implicitly consenting to tool calls;
//      they can Esc-cancel any turn that goes wrong.
//   2. CC/codex TUIs default to allowing in interactive mode too.
//   3. Headless mode uses a different policy via permission rules.
//
// Future work: replace this with a proper modal driven by
// internal/tui/permission_dialog.go (struct already exists, just
// not wired up). For now, surface the auto-allow so users notice.
func (a *App) onPermissionRequest(ev event.Event) (tea.Model, tea.Cmd) {
	if ev.Permission == nil || ev.Permission.Response == nil {
		return a, a.waitForEvent()
	}
	// Permission policy resolution:
	//   ALTCODE_AUTO_APPROVE=1     → silent auto-allow (YOLO mode)
	//   ALTCODE_REQUIRE_APPROVAL=1 → modal (explicit on; same as default)
	//   neither set                → MODAL by default (CC parity)
	//
	// Round-4 CC review flagged "modal is opt-in" as the biggest
	// remaining UI gap vs DS-TUI/CC. Flipping the default to modal-on
	// brings altcode in line with CC's interactive behaviour. Users
	// who want the old auto-allow-fast experience set
	// ALTCODE_AUTO_APPROVE=1 to opt back out.
	autoApprove := os.Getenv("ALTCODE_AUTO_APPROVE") == "1"
	if !autoApprove {
		a.pendingPermission = ev.Permission
		if a.permDialog == nil {
			a.permDialog = NewPermissionDialog(a.theme)
		}
		a.permDialog.SetWidth(a.mainBodyWidth())
		a.permDialog.Show(ev.Permission.ToolName, ev.Permission.Pattern)
		a.updateViewport()
		return a, a.waitForEvent()
	}
	// Auto-allow flow: surface a one-time-per-tool note so users
	// see WHICH tools the agent is exercising.
	if a.autoAllowSeen == nil {
		a.autoAllowSeen = make(map[string]bool)
	}
	if !a.autoAllowSeen[ev.Permission.ToolName] {
		a.autoAllowSeen[ev.Permission.ToolName] = true
		a.appendInfo(fmt.Sprintf(
			"[auto-allow] %s — ALTCODE_AUTO_APPROVE=1 active. Unset to get the modal back.",
			ev.Permission.ToolName))
	}
	select {
	case ev.Permission.Response <- event.PermResponse{Action: event.Allow}:
	default:
	}
	return a, a.waitForEvent()
}

// handlePermDialogKey routes y/n/a/! while the permission modal is up.
// Sends the user's choice on the pending response channel and hides
// the dialog. Returns (handled, cmd) — caller should swallow handled
// keystrokes and skip the rest of the input router.
func (a *App) handlePermDialogKey(s string) (bool, tea.Cmd) {
	if a.permDialog == nil || !a.permDialog.IsVisible() || a.pendingPermission == nil {
		return false, nil
	}
	resp := event.PermResponse{}
	switch s {
	case "y":
		resp.Action = event.Allow
	case "a":
		resp.Action = event.Allow
		resp.Persistent = true // session-allow this exact pattern
	case "!":
		resp.Action = event.Allow
		resp.Persistent = true // tool-wide allow (engine treats Persistent as global)
	case "n", "esc":
		resp.Action = event.Deny
	default:
		return false, nil // unrecognized key — let the rest of the router handle it
	}
	select {
	case a.pendingPermission.Response <- resp:
	default:
	}
	a.pendingPermission = nil
	a.permDialog.Hide()
	a.updateViewport()
	return true, nil
}

func (a *App) onTextDelta(ev event.Event) (tea.Model, tea.Cmd) {
	if a.thinking && a.thinkingText != "" {
		a.messages = append(a.messages, chatMessage{
			role: roleThinking, content: a.thinkingText,
		})
		a.thinkingText = ""
	}
	a.thinking = false
	a.streaming += ev.Text
	a.updateViewport()
	return a, a.waitForEvent()
}

func (a *App) onThinkingDelta(ev event.Event) (tea.Model, tea.Cmd) {
	a.thinking = true
	// Providers stream thinking as incremental deltas (each Delta holds
	// the new fragment, not the running total). Overwriting would drop
	// everything except the last fragment and make the preview jump.
	a.thinkingText += ev.Thinking
	a.updateViewport()
	return a, a.waitForEvent()
}

func (a *App) onToolStart(ev event.Event) (tea.Model, tea.Cmd) {
	if a.streaming != "" {
		a.messages = append(a.messages, chatMessage{
			role: roleAssistant, content: a.streaming,
		})
		a.streaming = ""
	}
	a.thinking = true
	a.activeToolName = ""
	a.activeToolDetail = ""
	if ev.ToolCall != nil {
		a.activeToolName = ev.ToolCall.Name
		target := extractToolTarget(ev.ToolCall)
		a.activeToolDetail = target
		a.tools.Start(ev.ToolCall.ID, ev.ToolCall.Name, target)
		a.toolStart = time.Now()
	}
	a.updateViewport()
	stuckName := a.activeToolName
	stuckStart := a.toolStart
	stuckCmd := tea.Tick(30*time.Second, func(_ time.Time) tea.Msg {
		return stuckToolMsg{name: stuckName, startedAt: stuckStart}
	})
	return a, tea.Batch(a.waitForEvent(), stuckCmd)
}

func (a *App) onToolResult(ev event.Event) (tea.Model, tea.Cmd) {
	a.thinking = false
	title, output := extractToolOutput(ev)
	hasError := ev.ToolResult != nil && ev.ToolResult.Error != ""
	// Guard against orphaned result events (ToolResult without a
	// matching ToolStart): time.Since(zero) would otherwise render as
	// a two-thousand-year duration next to the tool line.
	var elapsed time.Duration
	if !a.toolStart.IsZero() {
		elapsed = time.Since(a.toolStart)
	}

	// Extract tool call ID for matching the correct running entry
	// when multiple tools run concurrently.
	toolID := ""
	if ev.ToolCall != nil {
		toolID = ev.ToolCall.ID
	}

	if hasError {
		a.tools.DoneWithErrorOutput(toolID, title, elapsed, output)
	} else {
		a.recordToolSuccess(ev, toolID, title, elapsed, output)
	}
	a.recordToolMeta(ev, title, hasError)
	// Lightweight git dirty refresh after file-changing tools
	tn := a.activeToolName
	if ev.ToolCall != nil && ev.ToolCall.Name != "" {
		tn = ev.ToolCall.Name
	}
	switch strings.ToLower(tn) {
	case "write", "edit", "bash", "apply_patch":
		a.gitDirty = detectGitDirty()
	}
	a.activeToolName = ""
	a.activeToolDetail = ""
	a.updateViewport()
	return a, a.waitForEvent()
}

func (a *App) onUsage(ev event.Event) (tea.Model, tea.Cmd) {
	if ev.Usage != nil {
		a.tokensIn += ev.Usage.InputTokens
		a.tokensOut += ev.Usage.OutputTokens
		// Track the MOST RECENT turn's input tokens separately so
		// the HUD can show current context window occupancy instead
		// of cumulative session totals. Without this, the HUD bar
		// grew to 100% after ~10 turns even though each turn only
		// sent the conversation history which stays a fixed size.
		// Phase 13 bug hunt catch. OnUsage fires once per API call
		// within a turn; the last call's InputTokens is the best
		// proxy for "what the model saw last".
		a.currentContextTokens = ev.Usage.InputTokens
		// Capture cache-hit count from the same usage chunk so the
		// HUD chip reflects the latest turn's prefix-cache savings.
		// Provider-agnostic: anthropic emits cache_read_input_tokens,
		// openai/openrouter/deepseek emit prompt_tokens_details.cached_tokens
		// — both land on event.UsageInfo.CacheHits via the collector.
		if ev.Usage.CacheHits > 0 {
			a.cachedTokens = ev.Usage.CacheHits
		}
		a.tokenInfo = fmt.Sprintf("tokens: %d in / %d out",
			a.tokensIn, a.tokensOut)
	}
	if a.engine != nil {
		a.costUSD = a.engine.CostTracker().TotalCost()
	}
	return a, a.waitForEvent()
}

func (a *App) onInfo(ev event.Event) (tea.Model, tea.Cmd) {
	if ev.Info != "" {
		a.messages = append(a.messages,
			chatMessage{role: roleInfo, content: ev.Info})
		a.updateViewport()
	}
	return a, a.waitForEvent()
}

func (a *App) onError(ev event.Event) (tea.Model, tea.Cmd) {
	if provider := a.authErrorProvider(ev.Error); provider != "" {
		a.busy = false
		a.streaming = ""
		a.resetTurnTransientState()
		a.repromptForAPIKey(provider)
		// Keep draining events until the engine emits Done — without
		// this, the engine goroutine blocks forever on its next send
		// because nobody is reading a.events. Cancel ctx so the
		// engine exits promptly instead of finishing the turn.
		if a.cancel != nil {
			a.cancel()
		}
		return a, a.waitForEvent()
	}
	a.messages = append(a.messages,
		chatMessage{role: roleInfo, content: ev.Error, meta: "error"})
	a.streaming = ""
	a.busy = false
	a.resetTurnTransientState()
	a.updateViewport()
	// Same drain-and-cancel pattern: stop the engine but keep reading
	// events so the deferred Done send doesn't block on a full buffer
	// and leak the engine goroutine.
	if a.cancel != nil {
		a.cancel()
	}
	return a, a.waitForEvent()
}

// resetTurnTransientState clears the UI bits that should not persist
// after a turn ends abnormally (provider error, auth failure). Without
// this the HUD keeps the last-seen tool name, the tool tree keeps a
// stale ⟳ next to a tool that never completed, and the thinking
// indicator keeps flashing even though nothing is happening.
func (a *App) resetTurnTransientState() {
	a.thinking = false
	a.thinkingText = ""
	a.activeToolName = ""
	a.activeToolDetail = ""
	a.toolStart = time.Time{}
	a.tools.SweepRunning()
}

func (a *App) onDone() (tea.Model, tea.Cmd) {
	// Drop any zombie "running" entries that never got a ToolResult —
	// otherwise the final snapshot shows a stale ⟳ next to the real results.
	a.tools.SweepRunning()
	if len(a.tools.entries) > 0 {
		tree := a.tools.Render(a.theme, max(10, a.width-6))
		a.messages = append(a.messages, chatMessage{role: roleInfo, content: tree})
		// Clear immediately to avoid tools appearing TWICE (in messages + live tree)
		// which causes them to physically jump positions on screen.
		a.tools.Clear()
	}
	if a.streaming != "" {
		meta := a.buildTurnMeta()
		a.messages = append(a.messages, chatMessage{
			role: roleAssistant, content: a.streaming, meta: meta,
		})
		a.streaming = ""
	}
	// Turn completion summary — compact line showing what happened
	turnSummary := a.buildTurnSummary()
	if turnSummary != "" {
		a.messages = append(a.messages, chatMessage{
			role: roleInfo, content: turnSummary,
		})
	}
	a.busy = false
	// OSC 9 desktop notification when the turn ran past the visibility
	// threshold (DeepSeek-TUI parity). Suppressed in headless mode and
	// when ALTCODE_NOTIFY=0. 30-second user-visible cooldown stops a
	// chain of medium-length turns from hammering the desktop.
	if !a.turnStart.IsZero() {
		elapsed := time.Since(a.turnStart)
		if time.Since(a.lastBell) > 30*time.Second {
			emitTurnNotification(elapsed, turnSummary)
			a.lastBell = time.Now()
		}
	}

	// Drain the type-ahead queue: if the user typed prompts while
	// this turn was running, fire the next one automatically. One
	// drain per onDone keeps each prompt distinct (each gets its
	// own turn / cost / context). DeepSeek-TUI parity.
	if cmd := a.drainQueue(); cmd != nil {
		a.updateViewport()
		return a, cmd
	}

	a.updateViewport()
	return a, nil
}

// drainQueue pops the head of the type-ahead queue (if any) and
// returns the submit command for it. Called from onDone() after a
// turn completes. Empty queue → nil.
func (a *App) drainQueue() tea.Cmd {
	if len(a.queue) == 0 {
		return nil
	}
	next := a.queue[0]
	a.queue = a.queue[1:]
	a.input.SetValue(next)
	if len(a.queue) > 0 {
		a.appendInfo(fmt.Sprintf("[queue] dequeued — %d remaining", len(a.queue)))
	}
	return a.submit()
}

// buildTurnSummary creates a compact "✓ 2 files · 1 command · $0.03 · 45s" line.
func (a *App) buildTurnSummary() string {
	if a.turnToolCount == 0 {
		return "" // pure text response, no tools used
	}
	var parts []string
	writes := a.turnWrites
	reads := a.turnReads
	bashes := a.turnBashes

	if writes > 0 {
		parts = append(parts, fmt.Sprintf("%d file%s changed", writes, plural(writes)))
	}
	if reads > 0 {
		parts = append(parts, fmt.Sprintf("%d file%s read", reads, plural(reads)))
	}
	if bashes > 0 {
		parts = append(parts, fmt.Sprintf("%d command%s", bashes, plural(bashes)))
	}
	// Per-turn cost delta (not cumulative session cost)
	turnCost := a.costUSD - a.turnCostStart
	if turnCost > 0.001 {
		parts = append(parts, fmt.Sprintf("$%.2f", turnCost))
	}
	// Per-turn token delta
	turnTokens := (a.tokensIn + a.tokensOut) - a.turnTokenStart
	if turnTokens > 0 {
		parts = append(parts, formatTokens(turnTokens)+" tokens")
	}
	if !a.turnStart.IsZero() {
		parts = append(parts, formatDuration(time.Since(a.turnStart)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "✓ " + strings.Join(parts, " · ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// buildTurnMeta returns "model (duration)" for the response attribution line.
func (a *App) buildTurnMeta() string {
	if a.engine == nil || a.turnStart.IsZero() {
		return ""
	}
	elapsed := time.Since(a.turnStart)
	model := a.engine.Config().Model
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return fmt.Sprintf("%s (%s)", model, formatDuration(elapsed))
}

// extractToolOutput pulls title and output from a tool result event.
func extractToolOutput(ev event.Event) (string, string) {
	title, output := "", ""
	if ev.ToolResult != nil {
		title = ev.ToolResult.Title
		output = ev.ToolResult.Output
		if ev.ToolResult.Error != "" {
			output = ev.ToolResult.Error
		}
	}
	return title, output
}

// recordToolSuccess dispatches the tool result to the tree with output.
// Tool names are normalized to lower case so this matches the actual
// registry names (read, edit, bash, …) as well as the CC-style capitals.
func (a *App) recordToolSuccess(ev event.Event, toolID, title string, elapsed time.Duration, output string) {
	toolName := ""
	if ev.ToolCall != nil {
		toolName = ev.ToolCall.Name
	}
	switch strings.ToLower(toolName) {
	case "edit", "bash", "write", "apply_patch":
		a.tools.DoneWithOutput(toolID, title, elapsed, output)
	case "read":
		a.tools.DoneWithOutput(toolID, title, elapsed, truncateLines(output, 3))
	case "grep":
		a.tools.DoneWithOutput(toolID, title, elapsed, truncateLines(output, 4))
	default:
		a.tools.Done(toolID, title, elapsed)
	}
}

// recordToolMeta updates tool counts, task tracking, and sidebar.
// hasError tools still get a turn tick (they happened) but don't
// inflate the per-type success counters shown in the HUD / turn summary.
func (a *App) recordToolMeta(ev event.Event, title string, hasError bool) {
	toolName := ""
	if ev.ToolCall != nil {
		toolName = ev.ToolCall.Name
	}
	if toolName == "" {
		toolName = a.activeToolName // fallback from ToolStart
	}
	if toolName != "" {
		a.turnToolCount++
		if !hasError {
			a.toolCounts[toolName]++
			switch strings.ToLower(toolName) {
			case "write", "edit", "apply_patch":
				a.turnWrites++
			case "read":
				a.turnReads++
			case "bash":
				a.turnBashes++
			}
		}
	}
	if ev.ToolCall != nil {
		a.trackTaskFromTool(ev.ToolCall)
	}
	if !hasError {
		switch strings.ToLower(toolName) {
		case "edit", "write", "apply_patch":
			// Use the raw file_path from the tool input, not the
			// tool's Title. Write returns 'write /path' and Edit
			// returns 'edit /path' — different strings, so the
			// sidebar would list the same file twice after a
			// Write→Edit cycle. Pulling from tool input gives a
			// stable key for dedup.
			if path := toolFilePath(ev.ToolCall); path != "" {
				a.sidebar.AddFile(path, 1, 0)
			} else if title != "" {
				a.sidebar.AddFile(title, 1, 0)
			}
		}
	}
}

// toolFilePath extracts the file_path argument from a file-touching
// tool call so the sidebar can key changes by absolute path instead of
// each tool's bespoke Title format.
func toolFilePath(tc *event.ToolCall) string {
	if tc == nil || len(tc.Input) == 0 {
		return ""
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(tc.Input, &input) != nil {
		return ""
	}
	v, ok := input["file_path"]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}
