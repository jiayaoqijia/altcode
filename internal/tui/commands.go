package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/orchestrator"
	"github.com/altcode-ai/altcode/internal/workflow"
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
	case "/cost":
		a.appendInfo(a.builtinCostText())
	case "/history":
		a.appendInfo(a.builtinHistoryText())
	case "/compact":
		a.appendInfo(a.builtinCompactText())
	case "/diff":
		a.appendInfo(a.builtinDiffText())
	case "/plan":
		a.appendInfo(a.builtinPlanText())
	case "/stats":
		a.appendInfo(a.builtinStatsText())
	case "/tasks":
		a.appendInfo(a.builtinTasksText())
	case "/wf-status":
		a.appendInfo(a.builtinWorkflowStatusText())
	case "/wf-cancel":
		a.appendInfo(a.builtinWorkflowCancelText())
	case "/team":
		a.appendInfo(a.builtinTeamText())
	case "/backends":
		a.appendInfo(a.builtinBackendsText())
	case "/undo":
		a.appendInfo(a.builtinUndoText())
	case "/redo":
		a.appendInfo(a.builtinRedoText())
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
  /status    — model, session, message count
  /context   — context size breakdown
  /model     — show current model
  /clear     — clear conversation history
  /tools     — list available tools
  /sessions  — list recent sessions
  /memory    — show loaded memories
  /version   — show altcode version
  /cost      — cost breakdown per turn
  /history   — file change history this session
  /compact   — manually trigger context compaction
  /diff      — show files changed this session
  /plan      — enter plan mode
  /stats     — combined status + cost + history
  /tasks     — list background tasks
  /wf-status — show active workflow state
  /wf-cancel — clear workflow state
  /team      — show multi-AI team configuration
  /undo      — stash changes (git-backed undo)
  /redo      — restore stashed changes

Keyboard shortcuts:
  Enter      — submit prompt
  Ctrl+J     — insert newline
  Ctrl+K     — command palette
  Ctrl+A     — session switcher
  @file      — file completion popup
  Esc        — vim mode (j/k/G/gg/ctrl+d/ctrl+u)
  Esc Esc    — quit`
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

func (a *App) builtinCostText() string {
	if a.engine == nil || a.engine.CostTracker() == nil {
		return "No cost data available."
	}
	tracker := a.engine.CostTracker()
	turns := tracker.Turns()
	if len(turns) == 0 {
		return "No turns recorded yet."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Cost breakdown (%d turns):\n", len(turns)))
	for i, t := range turns {
		sb.WriteString(fmt.Sprintf("  Turn %d: %s — %d in / %d out — $%.4f\n",
			i+1, t.Model, t.InputTokens, t.OutputTokens, t.CostUSD))
	}
	sb.WriteString(fmt.Sprintf("\nTotal: %s", tracker.Summary()))
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinHistoryText() string {
	if a.engine == nil || a.engine.FileJournal() == nil {
		return "No file history available."
	}
	entries := a.engine.FileJournal().Entries()
	if len(entries) == 0 {
		return "No file changes recorded this session."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File history (%d operations):\n", len(entries)))
	for _, e := range entries {
		ts := e.Timestamp.Format(time.TimeOnly)
		sb.WriteString(fmt.Sprintf("  %s  %-8s %-6s %s\n",
			ts, e.Tool, e.Action, e.Path))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinCompactText() string {
	if a.engine == nil {
		return "No engine available."
	}
	before := a.engineMessageCount()
	a.engine.Compact()
	after := a.engineMessageCount()
	return fmt.Sprintf(
		"Compaction complete: %d messages (removed %d stale tool results).",
		after, before-after)
}

func (a *App) builtinDiffText() string {
	if a.engine == nil || a.engine.FileJournal() == nil {
		return "No file history available."
	}
	entries := a.engine.FileJournal().Entries()
	if len(entries) == 0 {
		return "No files changed this session."
	}
	seen := make(map[string]bool)
	var paths []string
	for _, e := range entries {
		if !seen[e.Path] {
			seen[e.Path] = true
			paths = append(paths, e.Path)
		}
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Files changed this session (%d):\n", len(paths)))
	for _, p := range paths {
		sb.WriteString(fmt.Sprintf("  %s\n", p))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinPlanText() string {
	return "Plan mode: describe your goal and altcode will outline steps " +
		"before making changes.\n" +
		"Tip: prefix your next message with the plan and altcode will " +
		"execute it step by step."
}

func (a *App) builtinStatsText() string {
	var sb strings.Builder
	sb.WriteString(a.builtinStatusText())
	sb.WriteString("\n\n")
	sb.WriteString(a.builtinCostSummary())
	sb.WriteString("\n")
	sb.WriteString(a.builtinFileSummary())
	return sb.String()
}

func (a *App) builtinCostSummary() string {
	if a.engine == nil || a.engine.CostTracker() == nil {
		return "Cost: n/a"
	}
	return "Cost: " + a.engine.CostTracker().Summary()
}

func (a *App) builtinFileSummary() string {
	if a.engine == nil || a.engine.FileJournal() == nil {
		return "Files: n/a"
	}
	return "Files: " + a.engine.FileJournal().Summary()
}

func (a *App) builtinVersionText() string {
	v := a.version
	if strings.TrimSpace(v) == "" {
		v = "dev"
	}
	return fmt.Sprintf("altcode v%s", v)
}

func (a *App) builtinTasksText() string {
	if a.engine == nil || a.engine.TaskQueue() == nil {
		return "No task queue available."
	}
	tasks := a.engine.TaskQueue().List()
	if len(tasks) == 0 {
		return "No tasks."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tasks (%d):\n", len(tasks)))
	for _, t := range tasks {
		ts := t.UpdatedAt.Format(time.TimeOnly)
		sb.WriteString(fmt.Sprintf(
			"  %s  %-10s %-9s %s\n",
			ts, t.ID, t.Status, t.Subject,
		))
	}
	sb.WriteString("\n" + a.engine.TaskQueue().Summary())
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinWorkflowStatusText() string {
	return workflow.StatusText(a.projectRoot)
}

func (a *App) builtinWorkflowCancelText() string {
	if err := workflow.ClearAll(a.projectRoot); err != nil {
		return fmt.Sprintf("Error clearing workflow state: %v", err)
	}
	return "Workflow state cleared."
}

func (a *App) builtinTeamText() string {
	if a.engine == nil {
		return "No engine available."
	}
	cfg := a.engine.Config()
	if cfg == nil || cfg.Team == nil {
		return `No multi-AI team configured.

Add a team to your config.json:

  {
    "team": {
      "name": "my-team",
      "models": {
        "architect":   {"model": "anthropic/claude-sonnet-4-20250514"},
        "implementer": {"model": "openai/deepseek/deepseek-chat-v3-0324"},
        "reviewer":    {"model": "openai/gpt-5.4"},
        "challenger":  {"model": "openai/qwen/qwen3-coder-next"}
      }
    }
  }

Or use OpenRouter for all models:

  {
    "provider": {"openai": {"apiKey": "sk-or-...", "baseURL": "https://openrouter.ai/api"}},
    "team": {
      "models": {
        "architect":   {"model": "openai/anthropic/claude-sonnet-4"},
        "implementer": {"model": "openai/deepseek/deepseek-chat-v3-0324"},
        "reviewer":    {"model": "openai/openai/gpt-4o"},
        "challenger":  {"model": "openai/qwen/qwen3-coder-next"}
      }
    }
  }

Roles: architect, implementer, reviewer, challenger, evaluator`
	}

	team := cfg.Team
	var sb strings.Builder
	name := team.Name
	if name == "" {
		name = "default"
	}
	sb.WriteString(fmt.Sprintf("Team: %s (%d models)\n\n", name, len(team.Models)))

	for role, m := range team.Models {
		sb.WriteString(fmt.Sprintf("  %-14s → %s\n", role, m.Model))
	}

	sb.WriteString("\nUse 'altcode team \"prompt\"' to run multi-AI orchestration.")
	return sb.String()
}

func (a *App) builtinBackendsText() string {
	backends := orchestrator.DetectBackends()
	return orchestrator.BackendsSummary(backends)
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
