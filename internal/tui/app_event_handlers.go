package tui

import (
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
	a.thinkingText = ev.Thinking
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
	elapsed := time.Since(a.toolStart)

	if hasError {
		a.tools.DoneWithErrorOutput(title, elapsed, output)
	} else {
		a.recordToolSuccess(ev, title, elapsed, output)
	}
	a.recordToolMeta(ev, title)
	// Lightweight git dirty refresh after file-changing tools
	tn := a.activeToolName
	if ev.ToolCall != nil && ev.ToolCall.Name != "" {
		tn = ev.ToolCall.Name
	}
	if tn == "Write" || tn == "Edit" || tn == "Bash" {
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
		a.repromptForAPIKey(provider)
		return a, nil
	}
	a.messages = append(a.messages,
		chatMessage{role: roleInfo, content: ev.Error, meta: "error"})
	a.streaming = ""
	a.busy = false
	a.updateViewport()
	return a, nil
}

func (a *App) onDone() (tea.Model, tea.Cmd) {
	if len(a.tools.entries) > 0 {
		tree := a.tools.Render(a.theme, a.width-6)
		a.messages = append(a.messages, chatMessage{role: roleInfo, content: tree})
		// Don't clear live tree here — let updateViewport show the final state
		// for one more frame. Clear happens on next submit() to avoid shrink flicker.
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
func (a *App) recordToolSuccess(ev event.Event, title string, elapsed time.Duration, output string) {
	toolName := ""
	if ev.ToolCall != nil {
		toolName = ev.ToolCall.Name
	}
	switch toolName {
	case "Edit", "Bash", "Write":
		a.tools.DoneWithOutput(title, elapsed, output)
	case "Read":
		a.tools.DoneWithOutput(title, elapsed, truncateLines(output, 3))
	case "Grep":
		a.tools.DoneWithOutput(title, elapsed, truncateLines(output, 4))
	default:
		a.tools.Done(title, elapsed)
	}
}

// recordToolMeta updates tool counts, task tracking, and sidebar.
func (a *App) recordToolMeta(ev event.Event, title string) {
	toolName := ""
	if ev.ToolCall != nil {
		toolName = ev.ToolCall.Name
	}
	if toolName == "" {
		toolName = a.activeToolName // fallback from ToolStart
	}
	if toolName != "" {
		a.toolCounts[toolName]++
		a.turnToolCount++
		switch toolName {
		case "Write", "Edit":
			a.turnWrites++
		case "Read":
			a.turnReads++
		case "Bash":
			a.turnBashes++
		}
	}
	if ev.ToolCall != nil {
		a.trackTaskFromTool(ev.ToolCall)
	}
	if (toolName == "Edit" || toolName == "Write") && title != "" {
		a.sidebar.AddFile(title, 1, 0)
	}
}
