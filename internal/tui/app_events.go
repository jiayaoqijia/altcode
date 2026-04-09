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
	"github.com/charmbracelet/lipgloss"
)


// handleEvent and all event handlers are in app_event_handlers.go

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

// truncateLines returns the first n lines of text, adding "… +N lines" if truncated.
func truncateLines(text string, n int) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= n {
		return text
	}
	result := strings.Join(lines[:n], "\n")
	remaining := len(lines) - n
	result += fmt.Sprintf("\n… +%d lines", remaining)
	return result
}

// thinkingVerbs are fun CC-style verbs that rotate during thinking.
var thinkingVerbs = []string{
	"Contemplating", "Pondering", "Reasoning", "Analyzing",
	"Deliberating", "Cogitating", "Synthesizing", "Evaluating",
	"Formulating", "Assembling", "Considering", "Processing",
	"Investigating", "Deciphering", "Constructing", "Architecting",
}

// renderThinkingIndicator returns the CC-style thinking display:
// ✶ Contemplating… (1m 5s · ↑ 1.2k tokens)
// ⎿  Tip: Press Esc to cancel, Ctrl+K for commands
func (a *App) renderThinkingIndicator() string {
	elapsed := time.Since(a.turnStart)
	if elapsed < 0 {
		elapsed = 0
	}

	// Rotate verb every 3 seconds
	verbIdx := int(elapsed.Seconds()/3) % len(thinkingVerbs)
	verb := thinkingVerbs[verbIdx]

	star := lipgloss.NewStyle().Foreground(a.theme.Warning).Bold(true).Render("✶")
	verbStyle := lipgloss.NewStyle().Foreground(a.theme.Primary).Bold(true).Render(verb + "…")

	// Build info parts: duration · ↓ tokens · effort
	var infoParts []string
	dur := formatToolDuration(elapsed)
	infoParts = append(infoParts, dur)

	// Per-turn token delta (CC shows "↓ 24.2k tokens" during thinking)
	turnTokens := (a.tokensIn + a.tokensOut) - a.turnTokenStart
	if turnTokens > 0 {
		infoParts = append(infoParts, "↓ "+formatTokens(turnTokens)+" tokens")
	}

	// CC shows "thinking with max effort" for long waits
	if elapsed >= 30*time.Second {
		infoParts = append(infoParts, "thinking with max effort")
	}

	infoStr := lipgloss.NewStyle().Foreground(a.theme.Muted).
		Render("(" + strings.Join(infoParts, " · ") + ")")

	line1 := star + " " + verbStyle + " " + infoStr + "\n"

	// Only show tip after 5s of thinking — avoids orphaned "Tip." flash
	if elapsed >= 5*time.Second {
		tip := lipgloss.NewStyle().Foreground(a.theme.Border).Render("  ⎿") + "  " +
			lipgloss.NewStyle().Foreground(a.theme.Muted).Italic(true).
				Render("Esc cancel · Ctrl+K commands") + "\n"
		return line1 + tip
	}

	return line1
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
