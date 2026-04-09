package tui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
	tea "github.com/charmbracelet/bubbletea"
)

// handleEvent processes a streaming event from the engine.
func (a *App) handleEvent(ev event.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case event.TextDelta:
		// Flush any accumulated thinking text as a collapsed message
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
	case event.TextDone:
		a.thinking = false
		return a, a.waitForEvent()
	case event.ThinkingDelta:
		a.thinking = true
		a.thinkingText = ev.Thinking
		a.updateViewport()
		return a, a.waitForEvent()
	case event.ToolStart:
		// Flush any streaming text to messages BEFORE showing the tool.
		// This makes text appear above the tool entry like CC does.
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
			a.activeToolDetail = target // show target in HUD: "Bash: go test"
			// For Bash, show the command as detail in CC style: Bash(command)
			detail := target
			if ev.ToolCall.Name == "Bash" && target != "" {
				detail = target // already extracted by extractToolTarget
			}
			a.tools.Start(ev.ToolCall.Name, detail)
			a.toolStart = time.Now()
		}
		a.updateViewport()
		// Schedule a stuck-tool check after 30s. Capture name and start
		// time NOW so the closure doesn't read stale values from a later tool.
		stuckName := a.activeToolName
		stuckStart := a.toolStart
		stuckCmd := tea.Tick(30*time.Second, func(_ time.Time) tea.Msg {
			return stuckToolMsg{name: stuckName, startedAt: stuckStart}
		})
		return a, tea.Batch(a.waitForEvent(), stuckCmd)
	case event.ToolResultEvent:
		a.thinking = false
		title := ""
		output := ""
		if ev.ToolResult != nil {
			title = ev.ToolResult.Title
			output = ev.ToolResult.Output
			if ev.ToolResult.Error != "" {
				output = ev.ToolResult.Error
			}
		}
		hasError := ev.ToolResult != nil && ev.ToolResult.Error != ""
		elapsed := time.Since(a.toolStart)
		if hasError {
			a.tools.DoneWithError(title, elapsed)
		} else {
			// Use DoneWithOutput for tools that benefit from inline display
			toolName := ""
			if ev.ToolCall != nil {
				toolName = ev.ToolCall.Name
			}
			if toolName == "Edit" || toolName == "Bash" || toolName == "Write" {
				a.tools.DoneWithOutput(title, elapsed, output)
			} else {
				a.tools.Done(title, elapsed)
			}
		}
		if ev.ToolCall != nil && ev.ToolCall.Name != "" {
			a.toolCounts[ev.ToolCall.Name]++
			// Track task progress from tool calls
			a.trackTaskFromTool(ev.ToolCall)
		}
		a.activeToolName = ""
		a.activeToolDetail = ""
		// Track file changes for sidebar
		if ev.ToolCall != nil && (ev.ToolCall.Name == "edit" || ev.ToolCall.Name == "write") {
			path := title
			if path != "" {
				a.sidebar.AddFile(path, 1, 0)
			}
		}
		a.updateViewport()
		return a, a.waitForEvent()
	case event.UsageEvent:
		if ev.Usage != nil {
			a.tokensIn += ev.Usage.InputTokens
			a.tokensOut += ev.Usage.OutputTokens
			a.tokenInfo = fmt.Sprintf("tokens: %d in / %d out",
				a.tokensIn, a.tokensOut)
		}
		if a.engine != nil {
			a.costUSD = a.engine.CostTracker().TotalCost()
		}
		// HUD redraws on every Bubbletea render cycle, so returning
		// here triggers a redraw with the updated token/cost counts.
		return a, a.waitForEvent()
	case event.InfoEvent:
		if ev.Info != "" {
			a.messages = append(a.messages,
				chatMessage{role: roleInfo, content: ev.Info})
			a.updateViewport()
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
			chatMessage{role: roleInfo, content: ev.Error, meta: "error"})
		a.streaming = ""
		a.busy = false
		a.updateViewport()
		return a, nil
	case event.Done:
		if a.streaming != "" {
			// Add response time + model info (using real turn timing)
			meta := ""
			if a.engine != nil && !a.turnStart.IsZero() {
				elapsed := time.Since(a.turnStart)
				model := a.engine.Config().Model
				if i := strings.LastIndex(model, "/"); i >= 0 {
					model = model[i+1:]
				}
				meta = fmt.Sprintf("%s (%s)", model, formatDuration(elapsed))
			}
			a.messages = append(a.messages, chatMessage{role: roleAssistant, content: a.streaming, meta: meta})
			a.streaming = ""
		}
		// Append tool tree as a tool message if there were tool calls
		if len(a.tools.entries) > 0 {
			tree := a.tools.Render(a.theme, a.width-6)
			a.messages = append(a.messages, chatMessage{role: roleTool, content: tree})
			a.tools.Clear()
		}
		a.busy = false
		a.updateViewport()
		return a, nil
	}
	return a, a.waitForEvent()
}

// trackTaskFromTool updates task counters from TaskCreate/TaskUpdate tool calls.
func (a *App) trackTaskFromTool(tc *event.ToolCall) {
	if tc == nil || len(tc.Input) == 0 {
		return
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(tc.Input, &input) != nil {
		return
	}
	switch tc.Name {
	case "TaskCreate":
		a.tasksTotal++
		var subject string
		if v, ok := input["subject"]; ok {
			json.Unmarshal(v, &subject)
		}
		var activeForm string
		if v, ok := input["activeForm"]; ok {
			json.Unmarshal(v, &activeForm)
		}
		if activeForm != "" {
			a.activeTaskName = activeForm
		} else if subject != "" {
			a.activeTaskName = subject
		}
	case "TaskUpdate":
		var status string
		if v, ok := input["status"]; ok {
			json.Unmarshal(v, &status)
		}
		switch status {
		case "completed":
			a.tasksDone++
			a.activeTaskName = ""
		case "in_progress":
			var subject string
			if v, ok := input["subject"]; ok {
				json.Unmarshal(v, &subject)
			}
			var activeForm string
			if v, ok := input["activeForm"]; ok {
				json.Unmarshal(v, &activeForm)
			}
			if activeForm != "" {
				a.activeTaskName = activeForm
			} else if subject != "" {
				a.activeTaskName = subject
			}
		}
	}
}

// extractToolTarget extracts a human-readable target from a tool call
// (file path for Read/Edit/Write, pattern for Grep/Glob, command for Bash).
func extractToolTarget(tc *event.ToolCall) string {
	if tc == nil || len(tc.Input) == 0 {
		return ""
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(tc.Input, &input) != nil {
		return ""
	}
	var s string
	switch tc.Name {
	case "Read", "Write", "Edit":
		if v, ok := input["file_path"]; ok {
			json.Unmarshal(v, &s)
		}
		// Show just the filename, not full path
		if i := strings.LastIndex(s, "/"); i >= 0 {
			s = s[i+1:]
		}
	case "Grep", "Glob":
		if v, ok := input["pattern"]; ok {
			json.Unmarshal(v, &s)
		}
	case "Bash":
		if v, ok := input["command"]; ok {
			json.Unmarshal(v, &s)
		}
		if len(s) > 30 {
			s = s[:27] + "..."
		}
	}
	return s
}

// lastAssistantMessage returns the last assistant response text, or "".
func (a *App) lastAssistantMessage() string {
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].role == roleAssistant {
			return a.messages[i].content
		}
	}
	return ""
}

// copyToClipboard writes text to the system clipboard.
// Tries: 1) OSC 52 (works over SSH), 2) platform clipboard commands.
func copyToClipboard(text string) {
	// Try OSC 52 escape sequence (works in most modern terminals + SSH)
	fmt.Printf("\033]52;c;%s\a", base64Enc(text))

	// Also try system clipboard as fallback
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try xclip first, then xsel, then wl-copy
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		}
	}
	if cmd != nil {
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	}
}

// base64Enc encodes text for OSC 52 clipboard using stdlib.
func base64Enc(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
