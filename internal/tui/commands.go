package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/compact"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/orchestrator"
	"github.com/altcode-ai/altcode/internal/plugin"
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
		if len(parts) > 1 {
			a.appendInfo(fmt.Sprintf(
				"Current model: %s\n\n/model %s — mid-session model switching isn't supported yet; restart altcode with --model %s or set 'model' in your config.",
				a.activeModel(), parts[1], parts[1],
			))
		} else {
			a.appendInfo(a.builtinModelText())
		}
	case "/clear":
		a.builtinClear()
	case "/tools":
		a.appendInfo(a.builtinToolsText())
	case "/skills":
		a.appendInfo(a.builtinSkillsText())
	case "/mcp":
		a.appendInfo(a.builtinMCPText())
	case "/plugins":
		a.appendInfo(a.builtinPluginsText())
	case "/sessions":
		a.appendInfo(a.builtinSessionsText())
	case "/memory":
		a.appendInfo(a.builtinMemoryText(parts))
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
				"       /workspace list            (show saved workspaces)\n" +
				"       /workspace status          (current workspace status)\n" +
				"Starts a multi-agent workspace.\n" +
				"Examples:\n" +
				"  /workspace add JWT auth                    (auto-detect agents)\n" +
				"  /workspace add auth claude:architect codex:coder   (pick agents)\n" +
				"  /workspace add auth codex:all              (single agent)")
			return true, nil
		}
		// Route bare subcommands (list/status) to their dedicated
		// handlers so they don't get interpreted as a single-word task
		// and spawn a workspace with 'list' or 'status' as the goal.
		switch strings.ToLower(parts[1]) {
		case "list":
			a.appendInfo(a.builtinWorkspaceListText())
			return true, nil
		case "status":
			a.appendInfo(a.builtinWorkspaceStatusText())
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
// Keep this in sync with handleBuiltinCommand in commands.go — a missing
// entry here means Tab won't complete to that command.
func (a *App) slashCommandNames() []string {
	builtins := []string{
		"/help", "/status", "/context", "/model", "/clear", "/tools",
		"/skills", "/mcp", "/plugins",
		"/cost", "/history", "/diff", "/compact", "/sessions", "/memory",
		"/version", "/stats", "/tasks", "/agents", "/team", "/workflow",
		"/backends", "/undo", "/redo", "/search",
		"/wf-status", "/wf-pause", "/wf-resume", "/wf-cancel",
		"/plan", "/rollback", "/send", "/workspace",
		"/spawn", "/init", "/doctor", "/compare",
		"/quit", "/exit",
	}
	// Add discovered slash commands (plugins + user commands)
	for name := range a.commands {
		builtins = append(builtins, "/"+name)
	}
	return builtins
}

// trySlashComplete attempts tab completion on a slash command prefix.
// Returns true if completion was performed.
//
// Behavior matches Claude Code more closely than the previous version:
//   - Exact match wins (typing /help + Tab → /help, not /help-foo).
//   - Single prefix match → fills in the command and a trailing space.
//   - Multiple prefix matches → complete to longest common prefix; if
//     no progress can be made, list each match WITH its description
//     pulled from the palette so users can pick by purpose, not name.
//   - Sorted output for stable display across runs.
func (a *App) trySlashComplete() bool {
	val := a.input.Value()
	if !strings.HasPrefix(val, "/") || strings.Contains(val, " ") {
		return false
	}

	prefix := strings.ToLower(val)
	cmds := a.slashCommandNames()

	// Exact match wins so '/help' + Tab doesn't get hijacked by
	// '/help-something-else' that happens to share the prefix.
	for _, c := range cmds {
		if strings.ToLower(c) == prefix {
			a.input.SetValue(c + " ")
			return true
		}
	}

	var matches []string
	for _, c := range cmds {
		if strings.HasPrefix(strings.ToLower(c), prefix) {
			matches = append(matches, c)
		}
	}

	if len(matches) == 0 {
		return false
	}
	sort.Strings(matches)

	if len(matches) == 1 {
		a.input.SetValue(matches[0] + " ")
		return true
	}

	// Multiple matches: complete to longest common prefix first.
	lcp := matches[0]
	for _, m := range matches[1:] {
		lcp = longestCommonPrefix(lcp, m)
	}
	if len(lcp) > len(val) {
		a.input.SetValue(lcp)
		return true
	}

	// Build a description map from the palette so users see what each
	// command actually does instead of a wall of names.
	descByName := map[string]string{}
	for _, p := range buildPaletteCommands(a.commands) {
		descByName[p.Name] = p.Description
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Completions for %q:\n", val))
	for _, m := range matches {
		desc := descByName[m]
		// Truncate to one line + 60 chars so a chatty plugin
		// description doesn't blow out the column layout.
		if i := strings.IndexByte(desc, '\n'); i >= 0 {
			desc = desc[:i]
		}
		if len(desc) > 60 {
			desc = desc[:59] + "…"
		}
		if desc != "" {
			sb.WriteString(fmt.Sprintf("  %-18s  %s\n", m, desc))
		} else {
			sb.WriteString(fmt.Sprintf("  %s\n", m))
		}
	}
	a.appendInfo(strings.TrimRight(sb.String(), "\n"))
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
		{"/skills", "list discovered skills"},
		{"/mcp", "list MCP servers"},
		{"/plugins", "show plugin warnings + search paths"},
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
		{"Ctrl+G", "edit prompt in $EDITOR"},
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
	// System content lives in SystemSection, not Messages — fold them
	// in so /context shows the real persona+tools+skills+memory cost
	// instead of zero at startup.
	for _, s := range a.engine.SystemPromptSections() {
		systemTokens += len(s.Content) / 4
	}

	limit := a.engine.ContextWindowSize()
	// Total = message tokens + system prompt tokens. Without the
	// systemTokens addend the bar showed 0% at startup even when the
	// system prompt alone consumed 20K+.
	totalTokens := compact.EstimateTokens(msgs) + systemTokens
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

func (a *App) builtinMemoryText(parts []string) string {
	if a.engine == nil || a.engine.MemoryStore() == nil {
		return "No memory store loaded."
	}
	store := a.engine.MemoryStore()

	// /memory add <text>    → create a new memory file
	// /memory rm <id>       → delete by id
	// /memory search <query> → fuzzy search stored memories
	// /memory               → list (default)
	if len(parts) >= 2 {
		sub := strings.ToLower(parts[1])
		switch sub {
		case "add":
			if len(parts) < 3 {
				return "Usage: /memory add <text>"
			}
			content := strings.Join(parts[2:], " ")
			content = strings.Trim(content, "'\"")
			if content == "" {
				return "Usage: /memory add <text>"
			}
			id := fmt.Sprintf("tui-%d", time.Now().Unix())
			title := firstLineOf(content, 60)
			if err := store.Save(id, title, content); err != nil {
				return fmt.Sprintf("Error saving memory: %v", err)
			}
			return fmt.Sprintf("Saved memory %q (%d bytes).", title, len(content))
		case "rm", "delete", "remove":
			if len(parts) < 3 {
				return "Usage: /memory rm <id>"
			}
			if err := store.Delete(parts[2]); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Sprintf("Memory %q not found. Use /memory to list ids.", parts[2])
				}
				return fmt.Sprintf("Error deleting memory: %v", err)
			}
			return fmt.Sprintf("Deleted memory %q.", parts[2])
		case "search", "find":
			if len(parts) < 3 {
				return "Usage: /memory search <query>"
			}
			query := strings.Join(parts[2:], " ")
			matches, err := store.Search(query)
			if err != nil {
				return fmt.Sprintf("Error searching memories: %v", err)
			}
			if len(matches) == 0 {
				return fmt.Sprintf("No memories match %q.", query)
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Found %d memory match(es):\n", len(matches)))
			for _, m := range matches {
				sb.WriteString(fmt.Sprintf("  - %s\n", m.Title))
			}
			return strings.TrimRight(sb.String(), "\n")
		}
		// Unknown subcommand → fall through to list.
	}

	memories, err := store.List()
	if err != nil {
		return fmt.Sprintf("Error loading memories: %v", err)
	}
	if len(memories) == 0 {
		return "No memories stored.\nUse '/memory add <text>' to save one."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Loaded memories (%d):\n", len(memories)))
	for _, m := range memories {
		sb.WriteString(fmt.Sprintf("  - %s\n", m.Title))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// firstLineOf returns the first non-empty line of text, trimmed to
// maxLen runes. Used to derive a short title from memory content.
func firstLineOf(s string, maxLen int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxLen {
			return line[:maxLen-1] + "…"
		}
		return line
	}
	return ""
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
	// Don't pretend success when compaction was a no-op. Users hitting
	// /compact early in a session got "Compaction complete: 0 → 0 (-0)"
	// and assumed the feature was broken.
	if beforeMsgs == afterMsgs && beforeTokens == afterTokens {
		return fmt.Sprintf(
			"Nothing to compact yet — context is under budget (%s tokens, %d messages).\nRun /compact again once the conversation grows.",
			formatTokens(beforeTokens), beforeMsgs)
	}
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
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("altcode v%s\n", v))
	sb.WriteString(fmt.Sprintf("  Go:       %s\n", runtime.Version()))
	sb.WriteString(fmt.Sprintf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	if info, ok := debug.ReadBuildInfo(); ok {
		var commit, modified, buildDate string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.modified":
				modified = s.Value
			case "vcs.time":
				buildDate = s.Value
			}
		}
		if commit != "" {
			short := commit
			if len(short) > 8 {
				short = short[:8]
			}
			tag := short
			if modified == "true" {
				tag = short + "-dirty"
			}
			sb.WriteString(fmt.Sprintf("  Commit:   %s\n", tag))
		}
		if buildDate != "" {
			sb.WriteString(fmt.Sprintf("  Built:    %s\n", buildDate))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
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
	n, err := workflow.ClearAll(a.projectRoot)
	if err != nil {
		return fmt.Sprintf("Error clearing workflow state: %v", err)
	}
	if n == 0 {
		return "No workflow state to clear."
	}
	return fmt.Sprintf("Cleared %d workflow state file(s).", n)
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

// builtinWorkspaceListText lists all saved workspace sessions under
// the project's .altcode/workspace directory. Returns a friendly
// message when nothing has been saved yet.
func (a *App) builtinWorkspaceListText() string {
	root := a.projectRoot
	if root == "" {
		return "[workspace] could not detect project root."
	}
	wsDir := filepath.Join(root, ".altcode", "workspace")
	store := workspace.NewStore(wsDir)
	ids, err := store.ListSessions()
	if err != nil {
		return fmt.Sprintf("[workspace] list failed: %v", err)
	}
	if len(ids) == 0 {
		return "[workspace] no saved workspaces.\nStart one with /workspace <task>."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Saved workspaces (%d):\n", len(ids)))
	for _, id := range ids {
		sess, err := store.LoadSession(id)
		if err != nil || sess == nil {
			sb.WriteString(fmt.Sprintf("  %s  (failed to load)\n", id))
			continue
		}
		task := sess.Task
		if len(task) > 50 {
			task = task[:49] + "…"
		}
		sb.WriteString(fmt.Sprintf("  %s  %-20s  %s\n", id, sess.Status, task))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// builtinWorkspaceStatusText reports the state of the active workspace
// if there is one, otherwise points users at /workspace list.
func (a *App) builtinWorkspaceStatusText() string {
	if a.wsView == nil || !a.wsView.IsActive() {
		return "[workspace] no active workspace. Use '/workspace list' to see saved ones."
	}
	sess := a.wsView.Session()
	if sess == nil {
		return "[workspace] active but session data unavailable."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Workspace: %s\n", sess.ID))
	sb.WriteString(fmt.Sprintf("  Task:     %s\n", sess.Task))
	sb.WriteString(fmt.Sprintf("  Status:   %s\n", sess.Status))
	sb.WriteString(fmt.Sprintf("  Agents:   %d\n", len(sess.Agents)))
	for role, rec := range sess.Agents {
		if rec == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("    %s  (%s)  %s\n", role, rec.Backend, rec.ActivityState))
	}
	return strings.TrimRight(sb.String(), "\n")
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

	// Skills / MCPs / plugins / memories — /agents used to only show
	// a lonely 'Skills: N' line, which felt misleading under the
	// 'Agent & Session Dashboard' title. Surface everything the user
	// would plausibly want to know about what's wired into the session.
	sb.WriteString(fmt.Sprintf("\n  Skills:         %d discovered\n", len(a.engine.Skills())))
	if cfg := a.engine.Config(); cfg != nil {
		sb.WriteString(fmt.Sprintf("  MCP servers:    %d configured\n", len(cfg.MCP)))
		sb.WriteString(fmt.Sprintf("  Providers:      %d configured\n", len(cfg.Provider)))
		if len(cfg.Hooks) > 0 {
			total := 0
			for _, ms := range cfg.Hooks {
				total += len(ms)
			}
			sb.WriteString(fmt.Sprintf("  Hooks:          %d matchers across %d events\n", total, len(cfg.Hooks)))
		}
	}
	if a.engine.MemoryStore() != nil {
		if mems, err := a.engine.MemoryStore().List(); err == nil {
			sb.WriteString(fmt.Sprintf("  Memories:       %d loaded\n", len(mems)))
		}
	}
	// Surface non-fatal plugin warnings (manifest parse errors, broken
	// command/agent loads, etc.) — these used to be silently dropped.
	if len(plugin.Warnings) > 0 {
		sb.WriteString(fmt.Sprintf("\n  Plugin warnings (%d):\n", len(plugin.Warnings)))
		for i, w := range plugin.Warnings {
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("    + %d more (run /doctor for full list)\n", len(plugin.Warnings)-5))
				break
			}
			sb.WriteString(fmt.Sprintf("    %s\n", w))
		}
	}

	// Workflow state
	wfStatus := workflow.StatusText(a.projectRoot)
	if wfStatus != "No active workflows." {
		sb.WriteString(fmt.Sprintf("\n  Workflows:\n    %s\n", wfStatus))
	}

	sb.WriteString("```")
	return sb.String()
}

// builtinSkillsText lists every skill (markdown command) discovered
// from the skills/commands cascade. Until /skills existed, users with
// installed Claude Code skills had no way to inspect them from the TUI.
func (a *App) builtinSkillsText() string {
	if a.engine == nil {
		return "No engine."
	}
	skills := a.engine.Skills()
	if len(skills) == 0 {
		return "No skills discovered. Install skills under .claude/skills/, ~/.claude/skills/, or .agents/skills/."
	}
	sorted := make([]engine.Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Discovered skills (%d):\n", len(sorted)))
	for _, s := range sorted {
		desc := strings.TrimSpace(s.Description)
		if len(desc) > 100 {
			desc = desc[:99] + "…"
		}
		if desc != "" {
			sb.WriteString(fmt.Sprintf("  - %-30s  %s\n", s.Name, desc))
		} else {
			sb.WriteString(fmt.Sprintf("  - %s\n", s.Name))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// builtinMCPText lists configured MCP servers. The /tools listing
// surfaces MCP tools mixed with native ones; this command shows just
// the servers and their connection state so users can debug a server
// that loaded zero tools.
func (a *App) builtinMCPText() string {
	if a.engine == nil {
		return "No engine."
	}
	cfg := a.engine.Config()
	if cfg == nil || len(cfg.MCP) == 0 {
		return "No MCP servers configured. Add servers to .mcp.json or settings.json."
	}
	names := make([]string, 0, len(cfg.MCP))
	for n := range cfg.MCP {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("MCP servers (%d configured):\n", len(names)))
	for _, n := range names {
		s := cfg.MCP[n]
		kind := "stdio"
		if s.URL != "" {
			kind = "sse"
		}
		sb.WriteString(fmt.Sprintf("  - %-24s %s\n", n, kind))
	}

	// Count MCP-prefixed tools so users can confirm at least one server
	// actually connected and registered tools.
	if reg := a.engine.Registry(); reg != nil {
		mcpTools := 0
		for _, t := range reg.All() {
			if strings.HasPrefix(t.Name(), "mcp__") {
				mcpTools++
			}
		}
		sb.WriteString(fmt.Sprintf("\n  Registered MCP tools: %d (use /tools for the full list)", mcpTools))
	}
	return sb.String()
}

// builtinPluginsText lists discovered plugins and any non-fatal
// warnings from manifest parsing or sub-resource loading. Plugins
// that loaded successfully don't currently surface their commands or
// agents — those are folded into /skills and /agents instead.
func (a *App) builtinPluginsText() string {
	var sb strings.Builder
	if len(plugin.Warnings) > 0 {
		sb.WriteString(fmt.Sprintf("Plugin warnings (%d):\n", len(plugin.Warnings)))
		for _, w := range plugin.Warnings {
			sb.WriteString(fmt.Sprintf("  %s\n", w))
		}
		sb.WriteString("\n")
	}
	pluginCmds := 0
	pluginAgts := 0
	if a.engine != nil {
		// Plugin commands and agents are folded into the global skill /
		// agent lists at startup; we don't keep a separate count today.
		// Surface what we can: the warnings list above is usually the
		// thing the user actually wants to debug.
		pluginCmds = -1
		pluginAgts = -1
		_ = pluginCmds
		_ = pluginAgts
	}
	if sb.Len() == 0 {
		sb.WriteString("No plugin warnings. Plugins are merged into /skills and /agents.\n")
		sb.WriteString("Plugin search paths:\n")
		sb.WriteString("  - .altcode/plugins/\n")
		sb.WriteString("  - .claude/plugins/\n")
		sb.WriteString("  - ~/.config/altcode/plugins/\n")
		sb.WriteString("  - ~/.claude/plugins/ (recursive)\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (a *App) builtinSearchText(query string) string {
	lower := strings.ToLower(query)
	var matches []string
	for i, msg := range a.messages {
		// Skip slash-command user messages and info bubbles so a
		// '/search foo' doesn't match its own command line and
		// '/help' dumps don't flood the result list.
		if msg.role == roleUser && strings.HasPrefix(strings.TrimSpace(msg.content), "/") {
			continue
		}
		if msg.role == roleInfo {
			continue
		}
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
	// Capture total BEFORE truncating so the header reports the real
	// count instead of always saying '20 match(es)'.
	total := len(matches)
	header := fmt.Sprintf("Found %d match(es) for %q:\n", total, query)
	if total > 20 {
		matches = matches[total-20:]
		header = fmt.Sprintf("Found %d match(es) for %q (showing last 20):\n", total, query)
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

// parseTurnArg parses a decimal turn number. Returns -1 on parse error.
func parseTurnArg(s string) int {
	if s == "" {
		return -1
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// builtinRollbackText handles /rollback --turn N.
func (a *App) builtinRollbackText(parts []string) string {
	if a.engine == nil {
		return "No engine available."
	}

	// Accept both '/rollback --turn N' (flag form) and '/rollback N'
	// (positional form) to match how other commands work. Return a
	// clean usage message on invalid args.
	turn := -1
	for i, p := range parts[1:] {
		if p == "--turn" && i+2 < len(parts) {
			turn = parseTurnArg(parts[i+2])
			break
		}
		// positional: first non-flag arg is the turn number
		if !strings.HasPrefix(p, "--") {
			turn = parseTurnArg(p)
			break
		}
	}
	if turn < 0 {
		return "Usage: /rollback <N>  (or /rollback --turn <N>)\n  Rolls back the conversation to turn N."
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
//
// Honest about its limitation: external agents (claude/codex spawned
// as subprocesses) have no inbound stdin protocol, so /send only adds
// an operator note to the pane and explicitly says so. The previous
// version returned "Annotated %s pane" which sounded like a successful
// delivery — users typed messages, got "success", and assumed the
// agent had received them.
func (a *App) builtinSendText(parts []string) string {
	if len(parts) < 3 {
		return "Usage: /send <role> <message>\n" +
			"  Adds an operator note to the agent's pane.\n" +
			"  NOTE: external agents (claude/codex) cannot receive\n" +
			"  inbound text after spawn. The note is for human eyes\n" +
			"  only. To actually deliver a new instruction, restart\n" +
			"  the workspace with the new prompt."
	}

	role := parts[1]
	message := strings.Join(parts[2:], " ")

	if a.wsView == nil || !a.wsView.IsActive() {
		return "No active workspace. /send requires a running workspace session."
	}

	if !a.wsView.HasRole(role) {
		return fmt.Sprintf("Unknown agent role %q. Check /agents for available roles.", role)
	}
	a.wsView.AppendAgentOutput(role, fmt.Sprintf("[operator note] %s", message))
	return fmt.Sprintf(
		"Operator note added to %s pane.\n"+
			"  Note: this is a visible annotation only. External agents\n"+
			"  have no inbound channel; restart the workspace with the\n"+
			"  new instruction to actually deliver it.",
		role,
	)
}
