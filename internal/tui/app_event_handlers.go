package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
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
	case event.Done:
		return a.onDone()
	}
	return a, a.waitForEvent()
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
		a.tools.Start(ev.ToolCall.Name, target)
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

	if hasError {
		a.tools.DoneWithErrorOutput(title, elapsed, output)
	} else {
		a.recordToolSuccess(ev, title, elapsed, output)
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
		return a, nil
	}
	a.messages = append(a.messages,
		chatMessage{role: roleInfo, content: ev.Error, meta: "error"})
	a.streaming = ""
	a.busy = false
	a.resetTurnTransientState()
	a.updateViewport()
	return a, nil
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
		tree := a.tools.Render(a.theme, a.width-6)
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
	if summary := a.buildTurnSummary(); summary != "" {
		a.messages = append(a.messages, chatMessage{
			role: roleInfo, content: summary,
		})
	}
	a.busy = false
	a.updateViewport()
	return a, nil
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
func (a *App) recordToolSuccess(ev event.Event, title string, elapsed time.Duration, output string) {
	toolName := ""
	if ev.ToolCall != nil {
		toolName = ev.ToolCall.Name
	}
	switch strings.ToLower(toolName) {
	case "edit", "bash", "write", "apply_patch":
		a.tools.DoneWithOutput(title, elapsed, output)
	case "read":
		a.tools.DoneWithOutput(title, elapsed, truncateLines(output, 3))
	case "grep":
		a.tools.DoneWithOutput(title, elapsed, truncateLines(output, 4))
	default:
		a.tools.Done(title, elapsed)
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
