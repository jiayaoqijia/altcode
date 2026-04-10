package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/compact"
	"github.com/altcode-ai/altcode/internal/orchestrator"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/workflow"
	"github.com/altcode-ai/altcode/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

// handleBuiltinCommand checks if the input is a built-in slash command
// and handles it locally without calling the model. Returns (handled, cmd).
func (a *App) handleBuiltinCommand(text string) (bool, tea.Cmd) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false, nil
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
	case "/wf-pause":
		a.appendInfo(a.builtinWorkflowPauseText())
	case "/wf-resume":
		a.appendInfo(a.builtinWorkflowResumeText())
	case "/agents":
		a.appendInfo(a.builtinAgentsText())
	case "/workspace":
		if len(parts) < 2 {
			a.appendInfo("Usage: /workspace <task> [backend:role ...]\n" +
				"Starts a multi-agent workspace.\n" +
				"Examples:\n" +
				"  /workspace add JWT auth                    (auto-detect agents)\n" +
				"  /workspace add auth claude:architect codex:coder   (pick agents)\n" +
				"  /workspace add auth codex:all              (single agent)")
			return true, nil
		}
		// Parse backend:role pairs from the end of the command
		task, agentSpecs := parseWorkspaceArgs(parts[1:])
		cmd := a.startWorkspaceFromTUIWithAgents(task, agentSpecs)
		return true, cmd
	case "/team":
		if len(parts) >= 3 && parts[1] == "run" {
			task := strings.Join(parts[2:], " ")
			a.startTeamRun(task)
			return true, nil
		}
		a.appendInfo(a.builtinTeamText())
	case "/workflow":
		if len(parts) < 2 {
			a.appendInfo("Usage: /workflow <name> <prompt>\nBuilt-in: ship-feature, review, fix\nLegacy: interview, plan, ralph\nExample: /workflow ship-feature add user auth")
			return true, nil
		}
		name := parts[1]
		prompt := strings.Join(parts[2:], " ")
		if prompt == "" {
			a.appendInfo("Usage: /workflow " + name + " <prompt>")
			return true, nil
		}
		switch name {
		case "interview", "plan", "ralph":
			a.appendInfo(fmt.Sprintf("Starting legacy workflow: %s — %s", name, prompt))
			return false, nil
		default:
			cmd := a.discoverAndRunWorkflow(name, prompt)
			return true, cmd
		}
	case "/backends":
		a.appendInfo(a.builtinBackendsText())
	case "/rollback":
		a.appendInfo(a.builtinRollbackText(parts))
	case "/send":
		a.appendInfo(a.builtinSendText(parts))
	case "/spawn":
		if a.wsView == nil || !a.wsView.IsActive() {
			a.appendInfo("[spawn] No active workspace. Start one with /workspace first.")
			return true, nil
		}
		if len(parts) < 3 {
			a.appendInfo("Usage: /spawn <role> <backend>\nExample: /spawn reviewer claude\n         /spawn security codex")
			return true, nil
		}
		role, backend := parts[1], parts[2]
		cmd := a.spawnAdditionalAgent(role, backend)
		return true, cmd
	case "/undo":
		a.appendInfo(a.builtinUndoText())
	case "/redo":
		a.appendInfo(a.builtinRedoText())
	case "/search":
		if len(parts) < 2 {
			a.appendInfo("Usage: /search <query>")
		} else {
			query := strings.Join(parts[1:], " ")
			a.appendInfo(a.builtinSearchText(query))
		}
	case "/init":
		return true, a.runInit()
	case "/doctor":
		a.appendInfo(a.runDoctor())
	case "/compare":
		if len(parts) < 2 {
			a.appendInfo("Usage: /compare <prompt>\nRuns the same prompt through multiple models and compares results.")
		} else {
			task := strings.Join(parts[1:], " ")
			a.appendInfo("[compare] Running across models... (use /workspace for full multi-agent)")
			return true, a.startCompare(task)
		}
	case "/quit", "/exit", "/q":
		return true, tea.Quit
	default:
		return false, nil
	}
	return true, nil
}

// appendInfo adds an info message and refreshes the viewport.
func (a *App) appendInfo(text string) {
	a.messages = append(a.messages, chatMessage{role: roleInfo, content: text})
	a.updateViewport()
}

// slashCommandNames returns all known slash command names for tab completion.
func (a *App) slashCommandNames() []string {
	builtins := []string{
		"/help", "/status", "/context", "/model", "/clear", "/tools",
		"/cost", "/history", "/diff", "/compact", "/sessions", "/memory",
		"/version", "/stats", "/tasks", "/agents", "/team", "/workflow",
		"/backends", "/undo", "/redo", "/search",
		"/wf-status", "/wf-pause", "/wf-resume", "/wf-cancel",
		"/plan", "/rollback", "/send", "/workspace",
	}
	// Add discovered slash commands
	for name := range a.commands {
		builtins = append(builtins, "/"+name)
	}
	return builtins
}

// trySlashComplete attempts tab completion on a slash command prefix.
// Returns true if completion was performed.
func (a *App) trySlashComplete() bool {
	val := a.input.Value()
	if !strings.HasPrefix(val, "/") || strings.Contains(val, " ") {
		return false
	}

	prefix := strings.ToLower(val)
	cmds := a.slashCommandNames()

	var matches []string
	for _, c := range cmds {
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, c)
		}
	}

	if len(matches) == 0 {
		return false
	}
	if len(matches) == 1 {
		a.input.SetValue(matches[0] + " ")
		return true
	}

	// Multiple matches: complete to longest common prefix
	lcp := matches[0]
	for _, m := range matches[1:] {
		lcp = longestCommonPrefix(lcp, m)
	}
	if len(lcp) > len(val) {
		a.input.SetValue(lcp)
		return true
	}

	// Show available completions as info
	a.appendInfo("Completions: " + strings.Join(matches, "  "))
	return true
}

func longestCommonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}

func builtinHelpText() string {
	type row struct{ cmd, desc string }
	commands := []row{
		{"/help", "show this help"},
		{"/status", "model, session, tokens"},
		{"/model", "current model"},
		{"/clear", "clear conversation"},
		{"/tools", "list tools"},
		{"/cost", "cost breakdown"},
		{"/history", "file changes this session"},
		{"/diff", "diff of changed files"},
		{"/compact", "trigger context compaction"},
		{"/search", "search in output"},
		{"/sessions", "list sessions"},
		{"/memory", "loaded memories"},
		{"/version", "version info"},
		{"/stats", "status + cost + history"},
		{"/tasks", "background tasks"},
		{"/agents", "agent + context overview"},
		{"/wf-status", "workflow state"},
		{"/wf-pause", "pause workflows"},
		{"/wf-resume", "resume workflows"},
		{"/wf-cancel", "clear workflow state"},
		{"/workspace", "start workspace [backend:role ...]"},
		{"/spawn", "add agent mid-run: /spawn role backend"},
		{"/workflow", "run phased workflow"},
		{"/team", "multi-AI team config"},
		{"/backends", "detected CLI backends"},
		{"/init", "generate CLAUDE.md from codebase"},
		{"/doctor", "check environment health"},
		{"/compare", "A/B test prompt across models"},
		{"/rollback", "rollback to turn N"},
		{"/send", "send msg to agent"},
		{"/context", "token breakdown"},
		{"/plan", "show plan"},
		{"/undo", "git-backed undo"},
		{"/redo", "restore undo"},
	}
	keys := []row{
		{"Enter", "send prompt"},
		{"Ctrl+J", "newline"},
		{"Tab", "complete /command (or cycle focus)"},
		{"Ctrl+K", "command palette"},
		{"Ctrl+A", "switch sessions"},
		{"@file", "file completion"},
		{"Up/Down", "browse prompt history"},
		{"Ctrl+C", "cancel (busy) / copy last response (idle)"},
		{"Ctrl+L", "clear screen"},
		{"Ctrl+R", "retry last prompt"},
		{"Esc", "cancel / clear input / vim mode"},
		{"Ctrl+D", "quit"},
	}
	wsKeys := []row{
		{"Ctrl+Z", "pause workspace/workflow"},
		{"Ctrl+R", "resume after pause"},
		{"Ctrl+Q", "abort workspace/workflow"},
		{"Ctrl+S", "send to focused agent"},
		{"Tab", "cycle agent focus"},
		{"Ctrl+1/2/3", "focus agent pane"},
	}

	var sb strings.Builder
	sb.WriteString("```\n")
	sb.WriteString("Commands\n")
	for _, r := range commands {
		sb.WriteString(fmt.Sprintf("  %-14s %s\n", r.cmd, r.desc))
	}
	sb.WriteString("\nShortcuts\n")
	for _, r := range keys {
		sb.WriteString(fmt.Sprintf("  %-14s %s\n", r.cmd, r.desc))
	}
	sb.WriteString("\nWorkspace Mode\n")
	for _, r := range wsKeys {
		sb.WriteString(fmt.Sprintf("  %-14s %s\n", r.cmd, r.desc))
	}
	sb.WriteString("```")
	return sb.String()
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
	if a.engine == nil {
		return "No engine available."
	}
	var sb strings.Builder
	sb.WriteString("```\n")
	sb.WriteString("Context Window\n\n")

	// Per-section token breakdown
	msgs := a.engine.Messages()
	var systemTokens, userTokens, assistantTokens, toolTokens int
	for _, m := range msgs {
		t := estimateMessageTokens(m)
		switch m.Role {
		case "system":
			systemTokens += t
		case "user":
			userTokens += t
		case "assistant":
			assistantTokens += t
		case "tool":
			toolTokens += t
		}
	}

	limit := a.engine.ContextWindowSize()
	totalTokens := compact.EstimateTokens(msgs)
	pct := 0
	if limit > 0 {
		pct = totalTokens * 100 / limit
	}

	// Visual bar
	barWidth := 30
	filled := pct * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	status := "OK"
	if pct >= 90 {
		status = "CRITICAL"
	} else if pct >= 70 {
		status = "HIGH"
	}

	sb.WriteString(fmt.Sprintf("  [%s] %d%% %s\n\n", bar, pct, status))
	sb.WriteString(fmt.Sprintf("  Total:        %s / %s\n", formatTokens(totalTokens), formatTokens(limit)))
	sb.WriteString(fmt.Sprintf("  System:       %s  (persona + tools + instructions)\n", formatTokens(systemTokens)))
	sb.WriteString(fmt.Sprintf("  User:         %s  (%d messages)\n", formatTokens(userTokens), countRole(msgs, "user")))
	sb.WriteString(fmt.Sprintf("  Assistant:    %s  (%d messages)\n", formatTokens(assistantTokens), countRole(msgs, "assistant")))
	sb.WriteString(fmt.Sprintf("  Tool results: %s  (%d results)\n", formatTokens(toolTokens), countRole(msgs, "tool")))
	sb.WriteString(fmt.Sprintf("  Instructions: %d sections\n", a.engineInstructionCount()))
	sb.WriteString(fmt.Sprintf("  Memories:     %d loaded\n", a.engineMemoryCount()))
	sb.WriteString("```")
	return sb.String()
}

func estimateMessageTokens(m provider.Message) int {
	t := len(m.Content) / 4
	for _, p := range m.Parts {
		t += len(p.Text)/4 + len(p.Content)/4
	}
	return t
}

func countRole(msgs []provider.Message, role string) int {
	n := 0
	for _, m := range msgs {
		if m.Role == role {
			n++
		}
	}
	return n
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
	a.messages = append(a.messages, chatMessage{role: roleInfo, content: "Conversation cleared."})
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
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Registered tools (%d):\n", len(names)))
	for _, n := range names {
		sb.WriteString("  - " + n + "\n")
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
	beforeMsgs := a.engineMessageCount()
	beforeTokens := compact.EstimateTokens(a.engine.Messages())
	a.engine.Compact()
	afterMsgs := a.engineMessageCount()
	afterTokens := compact.EstimateTokens(a.engine.Messages())
	return fmt.Sprintf(
		"Compaction complete:\n  Messages: %d → %d (-%d)\n  Tokens:   %s → %s (-%s)",
		beforeMsgs, afterMsgs, beforeMsgs-afterMsgs,
		formatTokens(beforeTokens), formatTokens(afterTokens), formatTokens(beforeTokens-afterTokens))
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
	// When a workspace is active, show its shared task list.
	if a.wsView != nil && a.wsView.IsActive() {
		return a.workspaceTasksText()
	}

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

func (a *App) workspaceTasksText() string {
	sess := a.wsView.Session()
	if sess == nil {
		return "Workspace session unavailable."
	}
	wsDir := filepath.Join(".altcode", "workspace", sess.ID)
	tl := workspace.NewTaskList(filepath.Join(wsDir, "tasks.json"))
	if err := tl.Load(); err != nil {
		return fmt.Sprintf("No workspace tasks (load: %v).", err)
	}
	tasks := tl.List()
	if len(tasks) == 0 {
		return "Workspace task list is empty."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workspace Tasks (%d):\n", len(tasks)))
	for _, t := range tasks {
		assignee := t.Assignee
		if assignee == "" {
			assignee = "-"
		}
		blocked := ""
		if tl.IsBlocked(t.ID) {
			blocked = " [blocked]"
		}
		sb.WriteString(fmt.Sprintf(
			"  %-10s %-12s %-10s %s%s\n",
			t.ID, t.Status, assignee, t.Title, blocked,
		))
	}
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

func (a *App) builtinWorkflowPauseText() string {
	n := workflow.PauseActive(a.projectRoot)
	if n == 0 {
		return "No active workflows to pause."
	}
	return fmt.Sprintf("Paused %d workflow(s).", n)
}

func (a *App) builtinWorkflowResumeText() string {
	n := workflow.ResumeActive(a.projectRoot)
	if n == 0 {
		return "No paused workflows to resume."
	}
	return fmt.Sprintf("Resumed %d workflow(s).", n)
}

func (a *App) builtinAgentsText() string {
	var sb strings.Builder
	sb.WriteString("```\n")
	sb.WriteString("Agent & Session Dashboard\n\n")

	// Session info
	sb.WriteString(fmt.Sprintf("  Model:          %s\n", a.activeModel()))
	sb.WriteString(fmt.Sprintf("  Session:        %s\n", formatDuration(time.Since(a.sessionStart))))
	sb.WriteString(fmt.Sprintf("  Tokens:         %s in / %s out\n",
		formatTokens(a.tokensIn), formatTokens(a.tokensOut)))
	if a.costUSD > 0 {
		sb.WriteString(fmt.Sprintf("  Cost:           $%.4f\n", a.costUSD))
	}

	// Context window (use API-reported tokens for accuracy)
	tokens := a.tokensIn + a.tokensOut
	limit := a.engine.ContextWindowSize()
	pct := 0
	if limit > 0 {
		pct = tokens * 100 / limit
	}
	barWidth := 20
	filled := pct * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	sb.WriteString(fmt.Sprintf("  Context:        [%s] %d%% (%s/%s)\n", bar, pct,
		formatTokens(tokens), formatTokens(limit)))

	// Tool usage
	sb.WriteString(fmt.Sprintf("\n  Tool calls:     %d\n", a.totalToolCalls()))
	if len(a.toolCounts) > 0 {
		for name, count := range a.toolCounts {
			sb.WriteString(fmt.Sprintf("    %-14s ×%d\n", name, count))
		}
	}

	// Skills
	sb.WriteString(fmt.Sprintf("\n  Skills:         %d discovered\n", len(a.engine.Skills())))

	// Workflow state
	wfStatus := workflow.StatusText(a.projectRoot)
	if wfStatus != "No active workflows." {
		sb.WriteString(fmt.Sprintf("\n  Workflows:\n    %s\n", wfStatus))
	}

	sb.WriteString("```")
	return sb.String()
}

func (a *App) builtinSearchText(query string) string {
	lower := strings.ToLower(query)
	var matches []string
	for i, msg := range a.messages {
		if strings.Contains(strings.ToLower(msg.content), lower) {
			// Show message number, role, and a snippet
			snippet := msg.content
			if len(snippet) > 120 {
				idx := strings.Index(strings.ToLower(snippet), lower)
				start := idx - 40
				if start < 0 {
					start = 0
				}
				end := idx + len(query) + 80
				if end > len(snippet) {
					end = len(snippet)
				}
				snippet = "..." + snippet[start:end] + "..."
			}
			role := "?"
			switch msg.role {
			case roleUser:
				role = "user"
			case roleAssistant:
				role = "assistant"
			case roleTool:
				role = "tool"
			case roleInfo:
				role = "info"
			}
			matches = append(matches, fmt.Sprintf("  #%d [%s] %s", i+1, role, strings.ReplaceAll(snippet, "\n", " ")))
		}
	}
	if len(matches) == 0 {
		return fmt.Sprintf("No matches for %q", query)
	}
	header := fmt.Sprintf("Found %d match(es) for %q:\n", len(matches), query)
	if len(matches) > 20 {
		matches = matches[len(matches)-20:]
		header = fmt.Sprintf("Found %d match(es) for %q (showing last 20):\n", len(matches), query)
	}
	return header + strings.Join(matches, "\n")
}

func (a *App) totalToolCalls() int {
	total := 0
	for _, n := range a.toolCounts {
		total += n
	}
	return total
}

func (a *App) builtinTeamText() string {
	// Show available backends
	backends := agent.DetectAvailableBackends()
	backendsStr := "none"
	if len(backends) > 0 {
		parts := make([]string, len(backends))
		for i, b := range backends {
			parts[i] = string(b)
		}
		backendsStr = strings.Join(parts, ", ")
	}

	if a.engine == nil {
		return fmt.Sprintf("Available CLI backends: %s\n\nUsage: /team run <task>", backendsStr)
	}
	cfg := a.engine.Config()
	if cfg == nil || cfg.Team == nil {
		return fmt.Sprintf(`Available CLI backends: %s

Usage: /team run <task>
  Spawns available backends in parallel with split-pane view.

Or configure a team in config.json:`, backendsStr) + `

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

// builtinRollbackText handles /rollback --turn N.
func (a *App) builtinRollbackText(parts []string) string {
	if a.engine == nil {
		return "No engine available."
	}

	turn := -1
	for i, p := range parts {
		if p == "--turn" && i+1 < len(parts) {
			n := 0
			for _, c := range parts[i+1] {
				if c < '0' || c > '9' {
					return "Usage: /rollback --turn N (N must be a positive integer)"
				}
				n = n*10 + int(c-'0')
			}
			turn = n
			break
		}
	}
	if turn < 0 {
		return "Usage: /rollback --turn N\n  Rolls back the conversation to turn N."
	}

	msgs := a.engine.Messages()
	// Count user turns
	turnIdx := 0
	cutoff := len(msgs)
	for i, m := range msgs {
		if m.Role == "user" {
			turnIdx++
			if turnIdx > turn {
				cutoff = i
				break
			}
		}
	}
	if turn > turnIdx {
		return fmt.Sprintf("Only %d turns exist. Cannot roll back to turn %d.", turnIdx, turn)
	}

	a.engine.TruncateMessages(cutoff)
	kept := len(a.engine.Messages())
	return fmt.Sprintf("Rolled back to turn %d (%d messages retained).", turn, kept)
}

// builtinSendText handles /send <role> <message>.
func (a *App) builtinSendText(parts []string) string {
	if len(parts) < 3 {
		return "Usage: /send <role> <message>\n  Sends a message to the named agent."
	}

	role := parts[1]
	message := strings.Join(parts[2:], " ")

	if a.wsView == nil || !a.wsView.IsActive() {
		return "No active workspace. /send requires a running workspace session."
	}

	if !a.wsView.HasRole(role) {
		return fmt.Sprintf("Unknown agent role %q. Check /agents for available roles.", role)
	}
	a.wsView.AppendAgentOutput(role, fmt.Sprintf("[operator] %s", message))
	return fmt.Sprintf("Sent to %s: %s", role, message)
}
