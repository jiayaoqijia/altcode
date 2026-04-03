package tui

import (
	"fmt"
	"strings"
)

// handleBuiltinCommand checks if the input is a built-in slash command
// and handles it locally without calling the model. Returns true if handled.
func (a *App) handleBuiltinCommand(text string) bool {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "/help":
		a.appendInfo(builtinHelpText())
	case "/status":
		a.appendInfo(a.builtinStatusText())
	case "/context":
		a.appendInfo(a.builtinContextText())
	case "/model":
		a.appendInfo(a.builtinModelText())
	case "/clear":
		a.builtinClear()
	case "/tools":
		a.appendInfo(a.builtinToolsText())
	case "/sessions":
		a.appendInfo(a.builtinSessionsText())
	case "/memory":
		a.appendInfo(a.builtinMemoryText())
	case "/version":
		a.appendInfo(a.builtinVersionText())
	default:
		return false
	}
	return true
}

// appendInfo adds an info message and refreshes the viewport.
func (a *App) appendInfo(text string) {
	a.messages = append(a.messages, text)
	a.updateViewport()
}

func builtinHelpText() string {
	return `Available commands:
  /help      — show this help
  /status    — model, session, message count, cost
  /context   — context size breakdown
  /model     — show current model
  /clear     — clear conversation history
  /tools     — list available tools
  /sessions  — list recent sessions
  /memory    — show loaded memories
  /version   — show altcode version

Keyboard shortcuts:
  Enter      — submit prompt
  Ctrl+J     — insert newline
  Esc        — cancel streaming / quit`
}

func (a *App) builtinStatusText() string {
	model := a.activeModel()
	sessionID := a.engineSessionID()
	msgCount := a.engineMessageCount()
	tokens := a.tokenInfo
	if tokens == "" {
		tokens = "n/a"
	}
	return fmt.Sprintf("Model: %s\nSession: %s\nMessages: %d\nTokens: %s",
		model, sessionID, msgCount, tokens)
}

func (a *App) builtinContextText() string {
	var sb strings.Builder
	sb.WriteString("Context breakdown:\n")
	sb.WriteString(fmt.Sprintf("  Messages: %d\n", a.engineMessageCount()))
	sb.WriteString(fmt.Sprintf("  System prompt sections: %d\n",
		a.engineInstructionCount()))
	memCount := a.engineMemoryCount()
	sb.WriteString(fmt.Sprintf("  Memories loaded: %d", memCount))
	return sb.String()
}

func (a *App) builtinModelText() string {
	return fmt.Sprintf("Current model: %s", a.activeModel())
}

func (a *App) builtinClear() {
	a.messages = nil
	a.streaming = ""
	if a.engine != nil {
		a.engine.ClearMessages()
	}
	a.messages = append(a.messages, "Conversation cleared.")
	a.updateViewport()
}

func (a *App) builtinToolsText() string {
	if a.engine == nil {
		return "No tools loaded."
	}
	reg := a.engine.Registry()
	if reg == nil {
		return "No tools loaded."
	}
	tools := reg.All()
	if len(tools) == 0 {
		return "No tools registered."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Registered tools (%d):\n", len(tools)))
	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("  - %s\n", t.Name()))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinSessionsText() string {
	if a.engine == nil || a.engine.StoreInstance() == nil {
		return "No session store available."
	}
	sessions, err := a.engine.StoreInstance().ListSessions()
	if err != nil {
		return fmt.Sprintf("Error listing sessions: %v", err)
	}
	if len(sessions) == 0 {
		return "No sessions found."
	}
	var sb strings.Builder
	limit := len(sessions)
	if limit > 10 {
		limit = 10
	}
	sb.WriteString(fmt.Sprintf("Recent sessions (%d shown):\n", limit))
	for _, s := range sessions[:limit] {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		sb.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			s.ID[:8], s.CreatedAt.Format("2006-01-02"), title))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinMemoryText() string {
	if a.engine == nil || a.engine.MemoryStore() == nil {
		return "No memory store loaded."
	}
	memories, err := a.engine.MemoryStore().List()
	if err != nil {
		return fmt.Sprintf("Error loading memories: %v", err)
	}
	if len(memories) == 0 {
		return "No memories stored."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Loaded memories (%d):\n", len(memories)))
	for _, m := range memories {
		sb.WriteString(fmt.Sprintf("  - %s\n", m.Title))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinVersionText() string {
	v := a.version
	if strings.TrimSpace(v) == "" {
		v = "dev"
	}
	return fmt.Sprintf("altcode v%s", v)
}

// engineSessionID returns the current session ID safely.
func (a *App) engineSessionID() string {
	if a.engine == nil {
		return "(none)"
	}
	id := a.engine.SessionID()
	if id == "" {
		return "(none)"
	}
	return id
}

// engineMessageCount returns the number of messages safely.
func (a *App) engineMessageCount() int {
	if a.engine == nil {
		return 0
	}
	return len(a.engine.Messages())
}

// engineInstructionCount returns the number of loaded instructions.
func (a *App) engineInstructionCount() int {
	if a.engine == nil {
		return 0
	}
	return len(a.engine.Instructions())
}

// engineMemoryCount returns the number of loaded memories.
func (a *App) engineMemoryCount() int {
	if a.engine == nil || a.engine.MemoryStore() == nil {
		return 0
	}
	memories, err := a.engine.MemoryStore().List()
	if err != nil {
		return 0
	}
	return len(memories)
}
