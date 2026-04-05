package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/altcode-ai/altcode/internal/compact"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/cost"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/history"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/memory"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/sandbox"
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/altcode-ai/altcode/internal/sysctl"
	"github.com/altcode-ai/altcode/internal/task"
	"github.com/altcode-ai/altcode/internal/tool"
)

const maxIterations = 50

// Skill describes a discoverable slash command/skill.
type Skill struct {
	Name        string
	Description string
	Path        string // file path so the model can read SKILL.md on demand
}

// EngineParams holds all dependencies for creating an Engine.
type EngineParams struct {
	Config       *config.Config
	Perm         *permission.Evaluator  // nil = allow all
	Store        *store.DB              // nil = no persistence
	SessionID    string                 // empty = new session
	Messages     []provider.Message     // pre-loaded for session resume
	Hooks        *hooks.Runner          // nil = no hooks
	Instructions []config.Instruction   // loaded from CLAUDE.md etc.
	Memory       *memory.Store          // nil = no persistent memory
	Sandbox      *sandbox.Sandbox       // nil = no sandboxing
	TaskQueue    *task.Queue            // nil = auto-created
	Skills       []Skill                // discovered slash commands/skills
	TokenBudget  *TokenBudget           // nil = unlimited (session-wide cap)
}

// TokenBudget is a session-wide cap shared across parent engine + subagents.
// Thread-safe via atomic operations.
type TokenBudget struct {
	used  int64 // atomic
	limit int64 // 0 = unlimited
}

// NewTokenBudget returns a budget with the given limit. limit <= 0 means unlimited.
func NewTokenBudget(limit int) *TokenBudget {
	return &TokenBudget{limit: int64(limit)}
}

// Consume adds n tokens. Returns true if the budget is still within limit.
func (b *TokenBudget) Consume(n int) bool {
	if b == nil || b.limit <= 0 {
		return true
	}
	used := atomic.AddInt64(&b.used, int64(n))
	return used <= b.limit
}

// Used returns the current token count.
func (b *TokenBudget) Used() int {
	if b == nil {
		return 0
	}
	return int(atomic.LoadInt64(&b.used))
}

// Limit returns the configured limit (0 = unlimited).
func (b *TokenBudget) Limit() int {
	if b == nil {
		return 0
	}
	return int(b.limit)
}

// Engine orchestrates conversation turns between the user and an AI provider.
type Engine struct {
	cfg          *config.Config
	provider     provider.Provider
	tools        *tool.Registry
	perm         *permission.Evaluator
	hooks        *hooks.Runner
	mem          *memory.Store
	store        *store.DB
	sandbox      *sandbox.Sandbox
	taskQueue    *task.Queue
	sessionID    string
	model        string
	messages     []provider.Message
	instructions []config.Instruction
	skills       []Skill
	totalTokens  int // running token count
	cost         *cost.Tracker
	journal      *history.Journal
	tokenBudget  *TokenBudget
}

// New creates an Engine from the given parameters.
func New(params EngineParams) (*Engine, error) {
	cfg := params.Config
	providerName, modelName := parseModel(cfg.Model)

	p, err := createProvider(providerName, cfg)
	if err != nil {
		return nil, err
	}

	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool())
	registry.Register(tool.NewGlobTool())
	registry.Register(tool.NewGrepTool())
	registry.Register(tool.NewLsTool())
	if params.Sandbox != nil {
		registry.Register(tool.NewBashToolWithSandbox(params.Sandbox))
	} else {
		registry.Register(tool.NewBashTool())
	}
	registry.Register(tool.NewEditTool())
	registry.Register(tool.NewWriteTool())
	registry.Register(tool.NewWebFetchTool())
	registry.Register(tool.NewWebSearchTool())
	registry.Register(tool.NewPatchTool())

	tq := params.TaskQueue
	if tq == nil {
		tq = task.NewQueue()
	}

	perm := params.Perm
	if perm == nil {
		perm = permission.NewEvaluator(permission.ModeBypass, "", nil)
	}

	msgs := params.Messages
	if msgs == nil {
		msgs = []provider.Message{}
	}

	hooksRunner := params.Hooks
	if hooksRunner == nil {
		hooksRunner = hooks.NewRunner(nil)
	}

	return &Engine{
		cfg:          cfg,
		provider:     p,
		tools:        registry,
		perm:         perm,
		hooks:        hooksRunner,
		mem:          params.Memory,
		store:        params.Store,
		sandbox:      params.Sandbox,
		taskQueue:    tq,
		sessionID:    params.SessionID,
		model:        modelName,
		messages:     msgs,
		instructions: params.Instructions,
		skills:       params.Skills,
		cost:         cost.NewTracker(),
		journal:      history.NewJournal(),
		tokenBudget:  params.TokenBudget,
	}, nil
}

// Skills returns the discovered skills.
func (e *Engine) Skills() []Skill { return e.skills }

// TokenBudget returns the engine's shared token budget (may be nil).
func (e *Engine) TokenBudget() *TokenBudget { return e.tokenBudget }

// Sandbox returns the engine's command sandbox.
func (e *Engine) Sandbox() *sandbox.Sandbox {
	return e.sandbox
}

// TaskQueue returns the engine's task queue.
func (e *Engine) TaskQueue() *task.Queue {
	return e.taskQueue
}

// Run sends user input to the provider and returns a channel of events.
// Set ALTCODE_DEBUG=1 to log events to stderr.
func (e *Engine) Run(ctx context.Context, input string) <-chan event.Event {
	events := make(chan event.Event, 64)
	debug := os.Getenv("ALTCODE_DEBUG") == "1"
	go func() {
		defer close(events)
		if debug {
			fmt.Fprintf(os.Stderr, "[debug] engine.Run: model=%s input=%q\n", e.model, truncate(input, 80))
		}
		raw := make(chan event.Event, 64)
		go func() {
			defer close(raw)
			e.loop(ctx, input, raw)
		}()
		for ev := range raw {
			if debug {
				debugEvent(ev)
			}
			events <- ev
		}
	}()
	return events
}

func debugEvent(ev event.Event) {
	switch ev.Type {
	case event.TextDelta:
		// too noisy, skip
	case event.ToolStart:
		name := ""
		if ev.ToolCall != nil {
			name = ev.ToolCall.Name
		}
		fmt.Fprintf(os.Stderr, "[debug] tool_start: %s\n", name)
	case event.ToolResultEvent:
		title := ""
		if ev.ToolResult != nil {
			title = ev.ToolResult.Title
		}
		fmt.Fprintf(os.Stderr, "[debug] tool_result: %s\n", title)
	case event.ErrorEvent:
		fmt.Fprintf(os.Stderr, "[debug] error: %s\n", ev.Error)
	case event.Done:
		fmt.Fprintf(os.Stderr, "[debug] done\n")
	default:
		fmt.Fprintf(os.Stderr, "[debug] event: %s\n", ev.Type)
	}
}

// isContextOverflow returns true if the error indicates a context length limit.
func isContextOverflow(msg string) bool {
	msg = strings.ToLower(msg)
	for _, sig := range []string{
		"context_length_exceeded",
		"context length",
		"maximum context",
		"too many tokens",
		"token limit",
		"request too large",
		"prompt is too long",
		"input is too long",
		"context window",
		"reduce the length",
	} {
		if strings.Contains(msg, sig) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Messages returns the current conversation history.
func (e *Engine) Messages() []provider.Message {
	return e.messages
}

// SessionID returns the current session ID.
func (e *Engine) SessionID() string {
	return e.sessionID
}

// Registry returns the engine's tool registry.
func (e *Engine) Registry() *tool.Registry {
	return e.tools
}

// ProviderInstance returns the engine's provider.
func (e *Engine) ProviderInstance() provider.Provider {
	return e.provider
}

// Config returns the engine's configuration.
func (e *Engine) Config() *config.Config {
	return e.cfg
}

// PermissionEvaluator returns the engine's permission evaluator.
func (e *Engine) PermissionEvaluator() *permission.Evaluator {
	return e.perm
}

// HooksRunner returns the engine's hooks runner.
func (e *Engine) HooksRunner() *hooks.Runner {
	return e.hooks
}

// MemoryStore returns the engine's memory store.
func (e *Engine) MemoryStore() *memory.Store {
	return e.mem
}

// StoreInstance returns the engine's backing session store.
func (e *Engine) StoreInstance() *store.DB {
	return e.store
}

// Instructions returns the loaded project instructions.
func (e *Engine) Instructions() []config.Instruction {
	return e.instructions
}

// ClearMessages resets the conversation history.
func (e *Engine) ClearMessages() {
	e.messages = []provider.Message{}
}

// CostTracker returns the engine's cost tracker.
func (e *Engine) CostTracker() *cost.Tracker {
	return e.cost
}

// FileJournal returns the engine's file history journal.
func (e *Engine) FileJournal() *history.Journal {
	return e.journal
}

// Compact manually triggers context compaction on the message history.
func (e *Engine) Compact() int {
	before := len(e.messages)
	mc := compact.NewMicrocompactor(20)
	e.messages = mc.Apply(e.messages)
	return before - len(e.messages)
}

// NewWithRegistry creates an Engine with an externally-provided tool registry.
// Used by the subagent system to create restricted child engines.
func NewWithRegistry(params EngineParams, registry *tool.Registry) (*Engine, error) {
	cfg := params.Config
	_, modelName := parseModel(cfg.Model)

	p, err := createProvider(parseModelProvider(cfg.Model), cfg)
	if err != nil {
		return nil, err
	}

	perm := params.Perm
	if perm == nil {
		perm = permission.NewEvaluator(permission.ModeBypass, "", nil)
	}
	hooksRunner := params.Hooks
	if hooksRunner == nil {
		hooksRunner = hooks.NewRunner(nil)
	}
	msgs := params.Messages
	if msgs == nil {
		msgs = []provider.Message{}
	}

	return &Engine{
		cfg:          cfg,
		provider:     p,
		tools:        registry,
		perm:         perm,
		hooks:        hooksRunner,
		mem:          params.Memory,
		store:        params.Store,
		sessionID:    params.SessionID,
		model:        modelName,
		messages:     msgs,
		instructions: params.Instructions,
		skills:       params.Skills,
		cost:         cost.NewTracker(),
		journal:      history.NewJournal(),
		tokenBudget:  params.TokenBudget,
	}, nil
}

func parseModelProvider(model string) string {
	name, _ := parseModel(model)
	return name
}

func (e *Engine) persistMessage(role string, msg provider.Message) {
	if e.store == nil || e.sessionID == "" {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	e.store.AddMessage(e.sessionID, role, data, e.model, 0, 0)
}

func (e *Engine) loop(ctx context.Context, input string, out chan<- event.Event) {
	e.hooks.Fire(ctx, hooks.SessionStart, hooks.Input{
		Event:     hooks.SessionStart,
		SessionID: e.sessionID,
	})

	input = e.fireUserPromptSubmit(ctx, input)

	userMsg := provider.TextMessage("user", input)
	e.messages = append(e.messages, userMsg)
	e.persistMessage("user", userMsg)

	defer func() { out <- event.Event{Type: event.Done} }()

	for i := 0; i < maxIterations; i++ {
		if ctx.Err() != nil {
			out <- event.Event{Type: event.ErrorEvent, Error: ctx.Err().Error()}
			return
		}

		stream, err := e.callProvider(ctx)
		if err != nil {
			// Auto-compact and retry on context overflow
			if isContextOverflow(err.Error()) && len(e.messages) > 4 {
				e.Compact()
				stream, err = e.callProvider(ctx)
			}
			if err != nil {
				out <- event.Event{Type: event.ErrorEvent, Error: err.Error()}
				return
			}
		}

		turn := collectTurn(stream, out)
		e.recordTurnCost(turn)

		// Auto-continue truncated text responses
		if turn.Truncated && len(turn.ToolCalls) == 0 {
			e.appendTruncatedAndContinue(turn)
			continue
		}

		if len(turn.ToolCalls) == 0 {
			assistMsg := provider.TextMessage("assistant", turn.Text)
			e.messages = append(e.messages, assistMsg)
			e.persistMessage("assistant", assistMsg)

			// Fire Stop hooks — may block completion
			if reason := e.fireStopHooks(ctx); reason != "" {
				e.messages = append(e.messages, provider.TextMessage("user", reason))
				continue // loop back for another turn
			}
			return
		}

		e.appendAssistantWithTools(turn)
		results := e.dispatchTools(ctx, turn.ToolCalls, out)
		e.appendToolResults(turn.ToolCalls, results)
		e.maybeCompact(ctx)
	}
}

// appendTruncatedAndContinue saves the partial assistant text and appends
// a continuation prompt so the engine loop retries with the model.
func (e *Engine) appendTruncatedAndContinue(turn *turnResult) {
	assistMsg := provider.TextMessage("assistant", turn.Text)
	e.messages = append(e.messages, assistMsg)
	e.persistMessage("assistant", assistMsg)
	contMsg := provider.TextMessage("user",
		"Continue from where you left off.")
	e.messages = append(e.messages, contMsg)
	e.persistMessage("user", contMsg)
}

func (e *Engine) callProvider(ctx context.Context) (<-chan provider.StreamEvent, error) {
	env := sysctl.DetectEnv()
	system := sysctl.BuildSystemPrompt(e.cfg, e.tools, e.instructions, env)

	// Inject available skills/slash commands into system prompt
	if len(e.skills) > 0 {
		infos := make([]sysctl.SkillInfo, len(e.skills))
		for i, s := range e.skills {
			infos[i] = sysctl.SkillInfo{Name: s.Name, Description: s.Description, Path: s.Path}
		}
		system = append(system, provider.SystemSection{
			Content:      sysctl.SkillsSection(infos),
			CacheControl: &provider.CacheControl{Type: "ephemeral"},
		})
	}

	// Inject persistent memories into system prompt
	if e.mem != nil {
		if memCtx := e.mem.ForContext(25 * 1024); memCtx != "" {
			system = append(system, provider.SystemSection{
				Content:      memCtx,
				CacheControl: &provider.CacheControl{Type: "ephemeral"},
			})
		}
	}

	req := &provider.Request{
		Model:     e.model,
		Messages:  e.messages,
		System:    system,
		Tools:     e.toolSchemas(),
		MaxTokens: 16384,
	}
	if temp, ok := providerDefaultTemperature(e.cfg.Model); ok {
		req.Temperature = &temp
	}
	return e.provider.Stream(ctx, req)
}

// providerDefaultTemperature returns a model-specific temperature and
// whether to send it. Some providers (altllm, some reasoning models)
// reject the temperature parameter entirely.
func providerDefaultTemperature(model string) (float64, bool) {
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "altllm"):
		return 0, false // altllm rejects the parameter
	case strings.HasPrefix(lower, "deepseek"):
		return 0.3, true
	case strings.HasPrefix(lower, "qwen"):
		return 0.3, true
	case strings.HasPrefix(lower, "moonshot"), strings.HasPrefix(lower, "kimi"):
		return 0.5, true
	case strings.HasPrefix(lower, "zhipu"), strings.HasPrefix(lower, "glm"):
		return 0.4, true
	case strings.HasPrefix(lower, "minimax"):
		return 0.4, true
	case strings.HasPrefix(lower, "anthropic"):
		return 0.2, true
	default:
		return 0.3, true
	}
}

func (e *Engine) appendAssistantWithTools(turn *turnResult) {
	var parts []provider.ContentPart
	if turn.Text != "" {
		parts = append(parts, provider.ContentPart{Type: "text", Text: turn.Text})
	}
	for _, tc := range turn.ToolCalls {
		parts = append(parts, provider.ContentPart{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		})
	}
	e.messages = append(e.messages, provider.Message{Role: "assistant", Parts: parts})
}

func (e *Engine) dispatchTools(
	ctx context.Context,
	toolCalls []collectedToolCall,
	out chan<- event.Event,
) []tool.Result {
	calls := make([]tool.Call, 0, len(toolCalls))
	for _, tc := range toolCalls {
		t, ok := e.tools.Get(tc.Name)
		if !ok {
			calls = append(calls, tool.Call{
				ID: tc.ID, Tool: nil, Input: tc.Input,
				EagerResult: &tool.Result{
					Output: fmt.Sprintf("Error: unknown tool %q", tc.Name),
					Title:  tc.Name,
				},
			})
			continue
		}

		action := e.checkPermission(t, tc)
		switch action {
		case permission.ActionDeny:
			// Fire PermissionDenied hook
			e.hooks.Fire(ctx, hooks.PermissionDenied, hooks.Input{
				Event:     hooks.PermissionDenied,
				ToolName:  tc.Name,
				ToolInput: tc.Input,
			})
			calls = append(calls, tool.Call{
				ID: tc.ID, Tool: t, Input: tc.Input,
				EagerResult: &tool.Result{
					Output: fmt.Sprintf("Permission denied for tool %q", tc.Name),
					Title:  tc.Name,
				},
			})
		case permission.ActionAsk:
			result := e.askPermission(tc, out)
			if result.Error != nil {
				calls = append(calls, tool.Call{
					ID: tc.ID, Tool: t, Input: tc.Input,
					EagerResult: &tool.Result{
						Output: fmt.Sprintf("Permission denied for tool %q", tc.Name),
						Title:  tc.Name,
					},
				})
			} else {
				calls = append(calls, tool.Call{ID: tc.ID, Tool: t, Input: tc.Input})
			}
		default:
			calls = append(calls, tool.Call{ID: tc.ID, Tool: t, Input: tc.Input})
		}

		e.perm.RecordCall(tc.Name, t.PermissionPattern(tc.Input))
	}

	// Fire PreToolUse hooks — may deny individual calls
	for i, tc := range toolCalls {
		hookResults, _ := e.hooks.Fire(ctx, hooks.PreToolUse, hooks.Input{
			Event:     hooks.PreToolUse,
			ToolName:  tc.Name,
			ToolInput: tc.Input,
		})
		if hooks.HasDeny(hookResults) {
			msg := "Blocked by hook"
			if msgs := hooks.Messages(hookResults); len(msgs) > 0 {
				msg = msgs[0]
			}
			calls[i].EagerResult = &tool.Result{
				Output: msg,
				Title:  tc.Name,
			}
		}
	}

	results := tool.Dispatch(ctx, calls)

	for i, r := range results {
		e.journalToolResult(toolCalls[i], r)
		out <- event.Event{
			Type:       event.ToolResultEvent,
			ToolResult: &event.Result{Output: r.Output, Title: r.Title},
			ToolCall:   &event.ToolCall{ID: toolCalls[i].ID, Name: toolCalls[i].Name},
		}
	}

	// Fire PostToolUse hooks
	e.firePostToolUseHooks(ctx, toolCalls, results)

	return results
}

func (e *Engine) firePostToolUseHooks(
	ctx context.Context,
	toolCalls []collectedToolCall,
	results []tool.Result,
) {
	for i, tc := range toolCalls {
		e.hooks.Fire(ctx, hooks.PostToolUse, hooks.Input{
			Event:      hooks.PostToolUse,
			ToolName:   tc.Name,
			ToolInput:  tc.Input,
			ToolOutput: results[i].Output,
		})
	}
}

func (e *Engine) checkPermission(t tool.Tool, tc collectedToolCall) permission.ActionType {
	pattern := t.PermissionPattern(tc.Input)
	return e.perm.CheckWithReadOnly(tc.Name, pattern, t.IsReadOnly())
}

func (e *Engine) askPermission(tc collectedToolCall, out chan<- event.Event) tool.Result {
	respCh := make(chan event.PermResponse, 1)
	out <- event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: tc.Name,
			Pattern:  tc.Name + ":" + string(tc.Input),
			Response: respCh,
		},
	}

	resp, ok := <-respCh
	if !ok || resp.Action != event.Allow {
		return tool.Result{Error: fmt.Errorf("permission denied by user")}
	}
	return tool.Result{}
}

func (e *Engine) appendToolResults(toolCalls []collectedToolCall, results []tool.Result) {
	providerName, _ := parseModel(e.cfg.Model)

	// Auto-verify: if any edit/write touched a .go file, append go build result
	autoCheck := e.goAutoVerify(toolCalls, results)

	for i, tc := range toolCalls {
		output := results[i].Output
		if results[i].Error != nil {
			output = fmt.Sprintf("Error: %v", results[i].Error)
		}
		if autoCheck != "" && (tc.Name == "edit" || tc.Name == "write") {
			output += "\n\n[auto-verify] " + autoCheck
			autoCheck = "" // only append once
		}

		// Anthropic batches all results into one message (handled below).
		// All other providers use OpenAI-compatible format: one role="tool" message per result.
		if providerName != "anthropic" {
			e.messages = append(e.messages, provider.Message{
				Role: "tool",
				Parts: []provider.ContentPart{
					provider.NewToolResultPart(tc.ID, output),
				},
			})
		}
	}

	// Anthropic: batch all results into one user message
	provName, _ := parseModel(e.cfg.Model)
	if provName == "anthropic" {
		autoCheckAnth := e.goAutoVerify(toolCalls, results)
		var parts []provider.ContentPart
		for i, tc := range toolCalls {
			output := results[i].Output
			if results[i].Error != nil {
				output = fmt.Sprintf("Error: %v", results[i].Error)
			}
			if autoCheckAnth != "" && (tc.Name == "edit" || tc.Name == "write") {
				output += "\n\n[auto-verify] " + autoCheckAnth
				autoCheckAnth = ""
			}
			parts = append(parts, provider.NewToolResultPart(tc.ID, output))
		}
		e.messages = append(e.messages, provider.ToolResultMessage(parts))
	}
}

// goAutoVerify runs 'go build' on the package containing any edited .go files.
// Returns a short status string, or empty if no Go files were edited.
func (e *Engine) goAutoVerify(toolCalls []collectedToolCall, results []tool.Result) string {
	pkgDirs := map[string]bool{}
	for i, tc := range toolCalls {
		if tc.Name != "edit" && tc.Name != "write" {
			continue
		}
		if results[i].Error != nil {
			continue
		}
		var input map[string]any
		if json.Unmarshal(tc.Input, &input) != nil {
			continue
		}
		filePath, _ := input["file_path"].(string)
		if filePath == "" {
			filePath, _ = input["path"].(string)
		}
		if !strings.HasSuffix(filePath, ".go") {
			continue
		}
		pkgDirs[filepath.Dir(filePath)] = true
	}
	if len(pkgDirs) == 0 {
		return ""
	}

	var sb strings.Builder
	for dir := range pkgDirs {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, "go", "build", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("go build in %s FAILED:\n%s", dir, strings.TrimSpace(string(out))))
		} else {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("go build in %s OK", dir))
		}
	}
	return sb.String()
}

func (e *Engine) fireUserPromptSubmit(ctx context.Context, input string) string {
	results, _ := e.hooks.Fire(ctx, hooks.UserPromptSubmit, hooks.Input{
		Event:     hooks.UserPromptSubmit,
		SessionID: e.sessionID,
	})
	for _, r := range results {
		if r.Message != "" {
			input = r.Message + "\n\n" + input
		}
	}
	return input
}

func (e *Engine) maybeCompact(ctx context.Context) {
	if len(e.messages) < 100 {
		return
	}
	e.hooks.Fire(ctx, hooks.PreCompact, hooks.Input{
		Event: hooks.PreCompact, SessionID: e.sessionID,
	})
	mc := compact.NewMicrocompactor(20)
	e.messages = mc.Apply(e.messages)
}

func (e *Engine) fireStopHooks(ctx context.Context) string {
	results, _ := e.hooks.Fire(ctx, hooks.Stop, hooks.Input{
		Event:     hooks.Stop,
		SessionID: e.sessionID,
	})
	if hooks.HasDeny(results) {
		if msgs := hooks.Messages(results); len(msgs) > 0 {
			return msgs[0]
		}
		return "Stop hook blocked completion."
	}
	return ""
}

func (e *Engine) recordTurnCost(turn *turnResult) {
	if turn.InputTokens > 0 || turn.OutputTokens > 0 {
		e.cost.RecordTurn(e.model, turn.InputTokens, turn.OutputTokens)
		if e.tokenBudget != nil {
			e.tokenBudget.Consume(turn.InputTokens + turn.OutputTokens)
		}
	}
}

// BudgetExceeded reports whether the session-wide token budget has been hit.
func (e *Engine) BudgetExceeded() bool {
	if e.tokenBudget == nil || e.tokenBudget.Limit() <= 0 {
		return false
	}
	return e.tokenBudget.Used() >= e.tokenBudget.Limit()
}

func (e *Engine) journalToolResult(tc collectedToolCall, r tool.Result) {
	name := tc.Name
	if name != "write" && name != "edit" {
		return
	}
	path := extractPath(tc.Input)
	if path == "" {
		return
	}
	action := "modify"
	if name == "write" {
		action = "create"
	}
	e.journal.Record(name, path, action, "", r.Output)
}

func extractPath(input json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	if p, ok := m["file_path"].(string); ok {
		return p
	}
	if p, ok := m["path"].(string); ok {
		return p
	}
	return ""
}

func (e *Engine) toolSchemas() []provider.ToolSchema {
	var schemas []provider.ToolSchema
	for _, t := range e.tools.All() {
		schemas = append(schemas, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		})
	}
	return schemas
}

func createProvider(name string, cfg *config.Config) (provider.Provider, error) {
	switch name {
	case "anthropic":
		pcfg := cfg.Provider["anthropic"]
		return provider.NewAnthropic(provider.AnthropicConfig{
			APIKey: pcfg.APIKey, BaseURL: pcfg.BaseURL,
		}), nil
	case "openai":
		pcfg := cfg.Provider["openai"]
		return provider.NewOpenAI(provider.OpenAIConfig{
			APIKey: pcfg.APIKey, BaseURL: pcfg.BaseURL,
		}), nil
	case "deepseek":
		return newOpenAICompat(cfg, "deepseek", "https://api.deepseek.com"), nil
	case "zhipu", "glm":
		return newOpenAICompat(cfg, "zhipu", "https://open.bigmodel.cn/api/paas/v4"), nil
	case "moonshot", "kimi":
		return newOpenAICompat(cfg, "moonshot", "https://api.moonshot.cn/v1"), nil
	case "minimax":
		return newOpenAICompat(cfg, "minimax", "https://api.minimax.chat/v1"), nil
	case "altllm":
		return newOpenAICompat(cfg, "altllm", "https://api.altllm.ai"), nil
	case "qwen", "dashscope":
		return newOpenAICompat(cfg, "qwen", "https://dashscope.aliyuncs.com/compatible-mode/v1"), nil
	case "ollama":
		return newOpenAICompat(cfg, "ollama", "http://localhost:11434"), nil
	case "lmstudio":
		return newOpenAICompat(cfg, "lmstudio", "http://localhost:1234"), nil
	default:
		// Unknown provider prefix — try as OpenAI-compatible.
		// If the provider has its own config entry, use it.
		// Otherwise fall back to the openai config (e.g. OpenRouter).
		if pcfg, ok := cfg.Provider[name]; ok && pcfg.APIKey != "" {
			return provider.NewOpenAI(provider.OpenAIConfig{
				APIKey: pcfg.APIKey, BaseURL: pcfg.BaseURL,
			}), nil
		}
		if pcfg, ok := cfg.Provider["openai"]; ok && pcfg.APIKey != "" {
			return provider.NewOpenAI(provider.OpenAIConfig{
				APIKey: pcfg.APIKey, BaseURL: pcfg.BaseURL,
			}), nil
		}
		return nil, fmt.Errorf("unsupported provider: %s (set API key via config or env)", name)
	}
}

func newOpenAICompat(cfg *config.Config, name, defaultBase string) provider.Provider {
	pcfg := cfg.Provider[name]
	base := pcfg.BaseURL
	if base == "" {
		base = defaultBase
	}
	return provider.NewOpenAI(provider.OpenAIConfig{
		APIKey: pcfg.APIKey, BaseURL: base,
	})
}

func parseModel(model string) (providerName, modelName string) {
	for i, c := range model {
		if c == '/' {
			return model[:i], model[i+1:]
		}
	}
	return "anthropic", model
}
