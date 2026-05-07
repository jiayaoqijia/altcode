package tui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/jiayaoqijia/altcode/internal/event"
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
// Tool names are normalized to lowercase to match the runtime registry
// as well as legacy capitalised names.
func extractToolTarget(tc *event.ToolCall) string {
	if tc == nil || len(tc.Input) == 0 {
		return ""
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(tc.Input, &input) != nil {
		return ""
	}
	var s string
	switch strings.ToLower(tc.Name) {
	case "read", "write", "edit", "apply_patch":
		if v, ok := input["file_path"]; ok {
			json.Unmarshal(v, &s)
		}
		// Show just the filename, not full path
		if i := strings.LastIndex(s, "/"); i >= 0 {
			s = s[i+1:]
		}
	case "grep", "glob":
		if v, ok := input["pattern"]; ok {
			json.Unmarshal(v, &s)
		}
	case "bash":
		if v, ok := input["command"]; ok {
			json.Unmarshal(v, &s)
		}
	}
	return s
}

// truncateLines returns the first n lines of text, adding "… +N lines"
// if truncated. This is a LINE-count truncator operating on a `[]string`
// slice — the `lines[:n]` index is a string-slice index, not a byte
// index into a string, so it's safe for multibyte content by
// construction. Distinct from truncateRunes (which truncates within a
// single string by rune count) — both helpers coexist for different
// scopes. CC iter-9 docs-clarity note.
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

// lastUserMessage returns the last user prompt, or "".
func (a *App) lastUserMessage() string {
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].role == roleUser {
			return a.messages[i].content
		}
	}
	return ""
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

	// Show first line of actual thinking text if available
	if a.thinkingText != "" {
		preview := a.thinkingText
		if i := strings.IndexByte(preview, '\n'); i > 0 {
			preview = preview[:i]
		}
		// Rune-safe thinking-preview truncation. Iter-9.
		preview = truncateRunes(preview, 80)
		line1 += lipgloss.NewStyle().Foreground(a.theme.Muted).Italic(true).
			Render("  ⎿  "+preview) + "\n"
	}

	// Show tip after 5s
	if elapsed >= 5*time.Second {
		line1 += lipgloss.NewStyle().Foreground(a.theme.Border).Render("  ⎿") + "  " +
			lipgloss.NewStyle().Foreground(a.theme.Muted).Italic(true).
				Render("Esc cancel · Ctrl+R retry · Ctrl+K commands") + "\n"
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
