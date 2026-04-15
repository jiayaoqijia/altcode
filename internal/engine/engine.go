package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	Perm         *permission.Evaluator // nil = allow all
	Store        *store.DB             // nil = no persistence
	SessionID    string                // empty = new session
	Messages     []provider.Message    // pre-loaded for session resume
	Hooks        *hooks.Runner         // nil = no hooks
	Instructions []config.Instruction  // loaded from CLAUDE.md etc.
	Memory       *memory.Store         // nil = no persistent memory
	Sandbox      *sandbox.Sandbox      // nil = no sandboxing
	TaskQueue    *task.Queue           // nil = auto-created
	Skills       []Skill               // discovered slash commands/skills
	TokenBudget  *TokenBudget          // nil = unlimited (session-wide cap)
	CostBudget   *CostBudget           // nil = unlimited USD cap

	// MaxTurns overrides the default per-run agent-loop cap
	// (internal `maxIterations = 50`). 0 means "use default".
	// Phase 8 wires --max-turns through here.
	MaxTurns int

	// PendingInputParts are content blocks to prepend to the user
	// message on the FIRST Run() call (and only the first). Used by
	// Phase 5 CLI flags --image and --file to attach image bytes
	// and pre-loaded file context to the initial prompt without
	// touching the Run signature. Consumed on first use.
	PendingInputParts []provider.ContentPart
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

// CostBudget is a session-wide USD cap shared across parent engine +
// subagents. Sibling of TokenBudget. Phase 8 post-turn enforcement
// only — mid-turn abort needs provider-side usage checkpoints which
// aren't yet wired.
//
// Storage is in 1/100000th-of-a-dollar units (micro-cents) to avoid
// floating-point drift when many small turns add up. Tracked as
// int64 atomics for lock-free parallel consumption from subagents.
type CostBudget struct {
	usedMicro  int64 // atomic — accumulated cost in 100000ths of a dollar
	limitMicro int64 // 0 = unlimited
}

// NewCostBudget returns a budget with the given limit in USD.
// A limit <= 0 means unlimited.
func NewCostBudget(limitUSD float64) *CostBudget {
	if limitUSD <= 0 {
		return &CostBudget{}
	}
	return &CostBudget{limitMicro: int64(limitUSD * 100000)}
}

// Consume adds costUSD to the budget. Returns true if still within
// limit. A negative or zero costUSD is a no-op.
func (b *CostBudget) Consume(costUSD float64) bool {
	if b == nil || b.limitMicro <= 0 {
		return true
	}
	if costUSD <= 0 {
		return b.Used() < b.Limit()
	}
	delta := int64(costUSD * 100000)
	newUsed := atomic.AddInt64(&b.usedMicro, delta)
	return newUsed < b.limitMicro
}

// Used returns accumulated cost in USD.
func (b *CostBudget) Used() float64 {
	if b == nil {
		return 0
	}
	return float64(atomic.LoadInt64(&b.usedMicro)) / 100000
}

// Limit returns the cap in USD. 0 = unlimited.
func (b *CostBudget) Limit() float64 {
	if b == nil {
		return 0
	}
	return float64(b.limitMicro) / 100000
}

// Exceeded reports whether the current used cost meets or exceeds
// the limit. Always false for an unlimited budget.
func (b *CostBudget) Exceeded() bool {
	if b == nil || b.limitMicro <= 0 {
		return false
	}
	return atomic.LoadInt64(&b.usedMicro) >= b.limitMicro
}

// Consume adds n tokens. Returns true if the budget is still within limit.
// Clamps n to non-negative — a buggy or corrected usage report from a
// provider that hands back a negative count would otherwise rewind the
// session-spend counter.
func (b *TokenBudget) Consume(n int64) bool {
	if b == nil || b.limit <= 0 {
		return true
	}
	if n < 0 {
		n = 0
	}
	used := atomic.AddInt64(&b.used, n)
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
	// msgMu serializes reads and writes to `messages`. Without it,
	// the streaming loop appending to `messages` raced with /clear,
	// /compact, and /rollback mutating the same slice — go test
	// -race caught this when a user slash-cleared mid-stream.
	msgMu        sync.Mutex
	messages     []provider.Message
	instructions []config.Instruction
	skills       []Skill
	// pendingInputParts is consumed on first Run() and merged into
	// the first user message. Populated by EngineParams.PendingInputParts
	// for Phase 5 --image / --file / etc. CLI flags.
	pendingInputParts []provider.ContentPart
	// costBudget is a session-wide USD cap. Sibling of tokenBudget
	// for Phase 8. Nil = unlimited.
	costBudget *CostBudget
	// maxTurns overrides the default agent-loop iteration cap.
	// 0 = use the package-level maxIterations constant.
	maxTurns          int
	totalTokens       int // running token count
	cost               *cost.Tracker
	journal            *history.Journal
	tokenBudget         *TokenBudget
	cachedContextWindow int // cached from /v1/models API, 0 = not fetched
	compactCount        int // consecutive compactions (thrash detection)
	titleSet            bool // true once the session title has been backfilled
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
	registry.Register(tool.NewAgentTool())

	tq := params.TaskQueue
	if tq == nil {
		tq = task.NewQueue()
	}
	registry.Register(tool.NewTaskCreateTool(tq))
	registry.Register(tool.NewTaskUpdateTool(tq))
	registry.Register(tool.NewTaskListTool(tq))
	registry.Register(tool.NewTaskGetTool(tq))

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
		cfg:               cfg,
		provider:          p,
		tools:             registry,
		perm:              perm,
		hooks:             hooksRunner,
		mem:               params.Memory,
		store:             params.Store,
		sandbox:           params.Sandbox,
		taskQueue:         tq,
		sessionID:         params.SessionID,
		model:             modelName,
		messages:          msgs,
		instructions:      params.Instructions,
		skills:            params.Skills,
		pendingInputParts: params.PendingInputParts,
		cost:              cost.NewTracker(),
		journal:           history.NewJournal(),
		tokenBudget:       params.TokenBudget,
		costBudget:        params.CostBudget,
		maxTurns:          params.MaxTurns,
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

// Messages returns a snapshot of the current conversation history.
// Returns a copy so callers can't mutate the engine's backing slice
// concurrently with a running turn.
func (e *Engine) Messages() []provider.Message {
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	out := make([]provider.Message, len(e.messages))
	copy(out, e.messages)
	return out
}

// SystemPromptSections returns a snapshot of the system prompt sections
// that would be sent on the next call. Built using the same code path
// as callProvider so /context can show accurate system token counts
// before the first turn — without this, /context displayed System: 0
// at startup even though persona + tools + skills + memory were
// non-empty.
func (e *Engine) SystemPromptSections() []provider.SystemSection {
	env := sysctl.DetectEnv()
	system := sysctl.BuildSystemPrompt(e.cfg, e.tools, e.instructions, env)
	if len(e.skills) > 0 {
		infos := make([]sysctl.SkillInfo, len(e.skills))
		for i, s := range e.skills {
			infos[i] = sysctl.SkillInfo{Name: s.Name, Description: s.Description, Path: s.Path}
		}
		system = append(system, provider.SystemSection{
			Content: sysctl.SkillsSection(infos),
		})
	}
	if e.mem != nil {
		if memCtx := e.mem.ForContext(25 * 1024); memCtx != "" {
			system = append(system, provider.SystemSection{Content: memCtx})
		}
	}
	return system
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
//
// Mutators take e.msgMu so they don't race with the streaming loop
// appending to e.messages from a turn goroutine. Without the mutex,
// /clear or /rollback during an in-flight turn produced a data race
// the race detector caught immediately.
func (e *Engine) ClearMessages() {
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	e.messages = []provider.Message{}
}

// TruncateMessages keeps only the first n messages, discarding the rest.
func (e *Engine) TruncateMessages(n int) {
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	if n < 0 {
		n = 0
	}
	if n < len(e.messages) {
		e.messages = e.messages[:n]
	}
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
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	before := len(e.messages)
	mc := compact.NewMicrocompactor(20)
	e.messages = mc.Apply(e.messages)
	return before - len(e.messages)
}

// appendMessageLocked appends m to e.messages under the mutex. Used
// by the streaming loop and tool dispatch path so concurrent /clear
// /compact /rollback callers don't race with the appends.
func (e *Engine) appendMessageLocked(m provider.Message) {
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	e.messages = append(e.messages, m)
}

// messageCountLocked returns len(e.messages) under the mutex.
func (e *Engine) messageCountLocked() int {
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	return len(e.messages)
}

// messagesSnapshot returns a copy of e.messages under the mutex.
// Use this when callers need the slice for read-only iteration.
func (e *Engine) messagesSnapshot() []provider.Message {
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	out := make([]provider.Message, len(e.messages))
	copy(out, e.messages)
	return out
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
		cfg:               cfg,
		provider:          p,
		tools:             registry,
		perm:              perm,
		hooks:             hooksRunner,
		mem:               params.Memory,
		store:             params.Store,
		sessionID:         params.SessionID,
		model:             modelName,
		messages:          msgs,
		instructions:      params.Instructions,
		skills:            params.Skills,
		pendingInputParts: params.PendingInputParts,
		cost:              cost.NewTracker(),
		journal:           history.NewJournal(),
		tokenBudget:       params.TokenBudget,
		costBudget:        params.CostBudget,
		maxTurns:          params.MaxTurns,
	}, nil
}

// CostBudget returns the engine's shared USD budget (may be nil).
func (e *Engine) CostBudget() *CostBudget { return e.costBudget }

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
		// Log to stderr at minimum so users notice when their session
		// can't be persisted. Previously this error was silently dropped
		// (not even an _ assignment), so a marshal failure left the
		// in-memory turn intact and the on-disk session missing the
		// message — users discovered the loss only on next launch.
		fmt.Fprintf(os.Stderr, "altcode: failed to marshal %s message for persistence: %v\n", role, err)
		return
	}
	if _, err := e.store.AddMessage(e.sessionID, role, data, e.model, 0, 0); err != nil {
		// Same rationale as the marshal branch — surface the failure
		// instead of pretending the write succeeded.
		fmt.Fprintf(os.Stderr, "altcode: failed to persist %s message: %v\n", role, err)
	}

	// Backfill session title on the first user message so the session
	// switcher stops showing '(untitled)' for every row. We only write
	// the title once per engine instance to avoid a DB roundtrip on
	// every turn.
	if role == "user" && !e.titleSet {
		e.titleSet = true
		if title := deriveSessionTitle(msg); title != "" {
			_ = e.store.UpdateSessionTitle(e.sessionID, title)
		}
	}
}

// deriveSessionTitle pulls a short, one-line title from the first user
// message. Slash commands and control text are ignored.
func deriveSessionTitle(msg provider.Message) string {
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "/") {
		return "" // slash commands aren't meaningful titles
	}
	// First line only, trimmed to ~60 chars.
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(text)
	const maxTitleLen = 60
	if len(text) > maxTitleLen {
		text = text[:maxTitleLen-1] + "…"
	}
	return text
}

func (e *Engine) loop(ctx context.Context, input string, out chan<- event.Event) {
	e.hooks.Fire(ctx, hooks.SessionStart, hooks.Input{
		Event:     hooks.SessionStart,
		SessionID: e.sessionID,
	})

	input = e.fireUserPromptSubmit(ctx, input)

	// Classify intent to auto-adjust engine behavior
	intent := ClassifyIntent(input)

	// Phase 5: if PendingInputParts were set at engine construction
	// (--image / --file CLI flags), merge them into the user message
	// alongside the text prompt and consume them. This runs once —
	// subsequent Run() calls on the same engine see a plain text
	// message. The engine holds no lock here because pendingInputParts
	// is mutated only from this single-goroutine loop path.
	userMsg := provider.TextMessage("user", input)
	if len(e.pendingInputParts) > 0 {
		parts := make([]provider.ContentPart, 0, len(e.pendingInputParts)+1)
		parts = append(parts, e.pendingInputParts...)
		parts = append(parts, provider.NewTextPart(input))
		userMsg = provider.Message{Role: "user", Parts: parts}
		e.pendingInputParts = nil
	}
	e.appendMessageLocked(userMsg)
	e.persistMessage("user", userMsg)

	defer func() {
		// Emit final summary only in debug mode (noisy for regular users)
		if os.Getenv("ALTCODE_DEBUG") != "" {
			summary := e.buildTurnSummary(intent)
			if summary != "" {
				sendEvent(ctx, out, event.Event{Type: event.InfoEvent, Info: summary})
			}
		}
		// sendEvent uses select+ctx so the deferred Done can't deadlock
		// when the TUI has stopped draining (e.g. after onError or
		// when the consumer channel is full).
		sendEvent(ctx, out, event.Event{Type: event.Done})
	}()

	// Resolve the per-run iteration cap. EngineParams.MaxTurns (set
	// by exec.Params.MaxTurns via --max-turns) overrides the
	// package-level default so scripts can tighten or loosen the
	// agent loop without recompiling.
	turnCap := maxIterations
	if e.maxTurns > 0 {
		turnCap = e.maxTurns
	}
	for i := 0; i < turnCap; i++ {
		if ctx.Err() != nil {
			// User cancels (Ctrl+C / Esc) surface as context.Canceled.
			// The TUI already prints '[cancelled]' when it cancels, so
			// don't double up with a bogus error message.
			if !errors.Is(ctx.Err(), context.Canceled) {
				sendEvent(ctx, out, event.Event{Type: event.ErrorEvent, Error: ctx.Err().Error()})
			}
			return
		}

		// Pre-turn compaction: proactively compact before sending to avoid overflow
		e.maybePreTurnCompact(ctx)

		stream, err := e.callProvider(ctx)
		if err != nil {
			// Reactive compact on overflow (fallback if pre-turn didn't catch it)
			if isContextOverflow(err.Error()) && e.messageCountLocked() > 4 {
				e.Compact()
				stream, err = e.callProvider(ctx)
			}
			if err != nil {
				// Same filter: swallow cancel-induced provider errors.
				if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
					sendEvent(ctx, out, event.Event{Type: event.ErrorEvent, Error: err.Error()})
				}
				return
			}
		}

		turn := collectTurn(ctx, stream, out)
		e.recordTurnCost(turn)

		// Phase 8: after recording the turn cost, check whether
		// the USD budget is exhausted. Post-turn enforcement
		// only — mid-turn abort needs provider-side usage
		// checkpoints which aren't wired yet. Emit a
		// BudgetExceeded event with a human-readable Info
		// message and return early. The deferred Done event
		// still fires via the top-of-loop defer.
		if e.costBudget != nil && e.costBudget.Exceeded() {
			sendEvent(ctx, out, event.Event{
				Type: event.BudgetExceeded,
				Info: fmt.Sprintf(
					"cost budget $%.4f exceeded ($%.4f used)",
					e.costBudget.Limit(), e.costBudget.Used()),
			})
			return
		}

		// Auto-continue truncated text responses
		if turn.Truncated && len(turn.ToolCalls) == 0 {
			e.appendTruncatedAndContinue(turn)
			continue
		}

		if len(turn.ToolCalls) == 0 {
			assistMsg := provider.TextMessage("assistant", turn.Text)
			e.appendMessageLocked(assistMsg)
			e.persistMessage("assistant", assistMsg)

			// Fire Stop hooks — may block completion
			if reason := e.fireStopHooks(ctx); reason != "" {
				stopMsg := provider.TextMessage("user", reason)
				e.appendMessageLocked(stopMsg)
				e.persistMessage("user", stopMsg)
				continue // loop back for another turn
			}
			return
		}

		e.appendAssistantWithTools(turn)
		results := e.dispatchTools(ctx, turn.ToolCalls, out)
		e.appendToolResults(turn.ToolCalls, results)
		e.maybeCompact(ctx)
	}

	// Phase 8: falling out of the loop means we hit the iteration
	// cap without the model producing a tool-free final turn.
	// Emit a BudgetExceeded event so headless users can tell a
	// "ran out of turns" stop apart from a normal completion.
	sendEvent(ctx, out, event.Event{
		Type: event.BudgetExceeded,
		Info: fmt.Sprintf("max-turns %d reached without completion", turnCap),
	})
}

// appendTruncatedAndContinue saves the partial assistant text and appends
// a continuation prompt so the engine loop retries with the model.
func (e *Engine) appendTruncatedAndContinue(turn *turnResult) {
	assistMsg := provider.TextMessage("assistant", turn.Text)
	e.appendMessageLocked(assistMsg)
	e.persistMessage("assistant", assistMsg)
	contMsg := provider.TextMessage("user",
		"Continue from where you left off.")
	e.appendMessageLocked(contMsg)
	e.persistMessage("user", contMsg)
}

// sendEvent forwards an event to the TUI channel without blocking
// forever when the consumer has stopped draining (TUI onError, channel
// full, etc.). Falls out on ctx.Done so the deferred Done emit can't
// leak the engine goroutine.
func sendEvent(ctx context.Context, out chan<- event.Event, ev event.Event) {
	select {
	case out <- ev:
	case <-ctx.Done():
	}
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

	// Snapshot under the mutex so a concurrent /clear or /compact
	// can't mutate the slice underneath the provider request.
	req := &provider.Request{
		Model:     e.model,
		Messages:  e.messagesSnapshot(),
		System:    system,
		Tools:     e.toolSchemas(),
		MaxTokens: 16384,
	}
	if temp, ok := providerDefaultTemperature(e.cfg.Model); ok {
		req.Temperature = &temp
	}
	// NOTE: RetryableStream from internal/provider exists and is
	// callable, but wrapping it here changes the semantics existing
	// engine tests rely on (they expect a 500 to surface immediately
	// as an error event, not be retried with backoff). Wire it in
	// once the test fixture supports a "no retry" mode and the
	// retry budget is tuned for both real providers and the test
	// mock. Until then, the helper is available to provider
	// implementations that want to opt in.
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
	e.appendMessageLocked(provider.Message{Role: "assistant", Parts: parts})
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
			result := e.askPermission(ctx, tc, out)
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

	// Fire PreToolUse hooks — may deny individual calls.
	// Aggregate ALL deny messages (a chain of validators may produce
	// several useful explanations) and mark the eager result as an
	// error so the tool tree renders red ✗ instead of green ✓.
	for i, tc := range toolCalls {
		hookResults, _ := e.hooks.Fire(ctx, hooks.PreToolUse, hooks.Input{
			Event:     hooks.PreToolUse,
			ToolName:  tc.Name,
			ToolInput: tc.Input,
		})
		if hooks.HasDeny(hookResults) {
			msg := "Blocked by hook"
			if msgs := hooks.Messages(hookResults); len(msgs) > 0 {
				msg = strings.Join(msgs, "; ")
			}
			calls[i].EagerResult = &tool.Result{
				Output: msg,
				Title:  tc.Name,
				Error:  fmt.Errorf("hook deny: %s", msg),
			}
		}
	}

	results := tool.Dispatch(ctx, calls)

	for i, r := range results {
		e.journalToolResult(toolCalls[i], r)
		// Propagate tool errors so the TUI can render the tree line
		// with a red ✗ instead of a misleading green ✓. Some tools set
		// r.Error explicitly; others leak 'Error: ...' through Output —
		// fall back on a prefix sniff so both styles surface correctly.
		errStr := ""
		if r.Error != nil {
			errStr = r.Error.Error()
		} else if strings.HasPrefix(strings.TrimSpace(r.Output), "Error:") {
			errStr = strings.TrimSpace(r.Output)
		}
		sendEvent(ctx, out, event.Event{
			Type: event.ToolResultEvent,
			ToolResult: &event.Result{
				Output: r.Output,
				Title:  r.Title,
				Error:  errStr,
			},
			// Carry the original Input forward so the TUI can key its
			// sidebar / bookkeeping off file_path instead of each
			// tool's bespoke Title string.
			ToolCall: &event.ToolCall{
				ID:    toolCalls[i].ID,
				Name:  toolCalls[i].Name,
				Input: toolCalls[i].Input,
			},
		})
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
		// Skip tool calls that never actually ran — denied by
		// permission, hook, or unknown tool. PostToolUse hooks that
		// auto-format files / refresh sidebars / bump journals on
		// every tool use would otherwise fire on the canned
		// "Permission denied" message and corrupt their state.
		if results[i].Error != nil {
			continue
		}
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

func (e *Engine) askPermission(ctx context.Context, tc collectedToolCall, out chan<- event.Event) tool.Result {
	respCh := make(chan event.PermResponse, 1)
	select {
	case out <- event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: tc.Name,
			Pattern:  tc.Name + ":" + string(tc.Input),
			Response: respCh,
		},
	}:
	case <-ctx.Done():
		return tool.Result{Error: ctx.Err()}
	}

	// Don't deadlock on <-respCh if the TUI disappears or the user
	// cancels the turn while waiting for a permission answer. Select
	// on ctx.Done() too so cancellation cleanly unblocks the engine.
	select {
	case resp, ok := <-respCh:
		if !ok || resp.Action != event.Allow {
			return tool.Result{Error: fmt.Errorf("permission denied by user")}
		}
		return tool.Result{}
	case <-ctx.Done():
		return tool.Result{Error: ctx.Err()}
	}
}

// maxToolResultLen caps tool result text stored in context to prevent bloat.
const maxToolResultLen = 30000

func (e *Engine) appendToolResults(toolCalls []collectedToolCall, results []tool.Result) {
	providerName, _ := parseModel(e.cfg.Model)

	// Auto-verify: run verification ladder on edited Go files (once)
	autoCheck := e.runPostEditVerify(toolCalls, results)

	// Build processed outputs (shared between Anthropic and OpenAI paths)
	outputs := make([]string, len(toolCalls))
	for i, tc := range toolCalls {
		output := results[i].Output
		if results[i].Error != nil {
			output = fmt.Sprintf("Error: %v", results[i].Error)
		}
		if autoCheck != "" && (tc.Name == "edit" || tc.Name == "write") {
			output += "\n\n[auto-verify]\n" + autoCheck
			autoCheck = "" // only append once
		}
		// Truncate very long tool results to avoid context bloat
		if len(output) > maxToolResultLen {
			output = output[:maxToolResultLen] + "\n\n... [truncated, " +
				fmt.Sprintf("%d", len(results[i].Output)) + " bytes total]"
		}
		outputs[i] = output
	}

	if providerName == "anthropic" {
		// Anthropic: batch all results into one user message
		var parts []provider.ContentPart
		for i, tc := range toolCalls {
			parts = append(parts, provider.NewToolResultPart(tc.ID, outputs[i]))
		}
		e.appendMessageLocked(provider.ToolResultMessage(parts))
	} else {
		// OpenAI-compatible: one role="tool" message per result
		for i, tc := range toolCalls {
			e.appendMessageLocked(provider.Message{
				Role: "tool",
				Parts: []provider.ContentPart{
					provider.NewToolResultPart(tc.ID, outputs[i]),
				},
			})
		}
	}
}

// runPostEditVerify runs the verification ladder on directories containing
// edited .go files. Uses build → vet (stops at first failure per dir).
// Returns formatted results, or empty if no Go files were edited.
func (e *Engine) runPostEditVerify(toolCalls []collectedToolCall, results []tool.Result) string {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var sb strings.Builder
	for dir := range pkgDirs {
		// Quick ladder: build + vet (skip tests for speed during tool dispatch)
		vResults := RunVerificationLadder(ctx, dir, []VerifyLevel{VerifyBuild, VerifyVet})
		sb.WriteString(fmt.Sprintf("  %s:\n", dir))
		sb.WriteString(FormatVerificationResults(vResults))
	}
	return sb.String()
}

func (e *Engine) fireUserPromptSubmit(ctx context.Context, input string) string {
	// Pass the user's prompt text to the hook payload — without
	// UserPrompt set, command hooks reading stdin saw an empty value
	// and prompt hooks expanding $USER_PROMPT got "".
	results, _ := e.hooks.Fire(ctx, hooks.UserPromptSubmit, hooks.Input{
		Event:      hooks.UserPromptSubmit,
		SessionID:  e.sessionID,
		UserPrompt: input,
	})
	for _, r := range results {
		if r.Message != "" {
			input = r.Message + "\n\n" + input
		}
	}
	return input
}

// maxConsecutiveCompactions is the thrash detection limit.
// If context refills immediately after this many consecutive compactions,
// we stop compacting to avoid infinite loops (matches Claude Code behavior).
const maxConsecutiveCompactions = 3

// messageBytes estimates the serialized size of the current message history.
// This is a byte-level safety check for the API's 20MB request ceiling,
// which token-based estimates can miss when tool results contain dense data.
func (e *Engine) messageBytes() int {
	e.msgMu.Lock()
	defer e.msgMu.Unlock()
	total := 0
	for _, m := range e.messages {
		total += len(m.Content)
		for _, p := range m.Parts {
			total += len(p.Text) + len(p.Content) + len(p.Input)
		}
	}
	return total
}

// maxRequestBytes is a safety margin below the API's 20MB hard limit.
// When we cross this, compact aggressively to avoid "request too large" errors.
const maxRequestBytes = 15 * 1024 * 1024

// maybePreTurnCompact runs BEFORE sending a request to the provider.
// Triggers on either token count (90% of window) or byte count (15MB).
func (e *Engine) maybePreTurnCompact(ctx context.Context) {
	snap := e.messagesSnapshot()
	tokens := compact.EstimateTokens(snap)
	limit := e.contextWindowSize()
	bytes := e.messageBytes()
	// Trigger at 90% of context window OR 15MB of raw bytes
	if tokens < limit*9/10 && bytes < maxRequestBytes {
		e.compactCount = 0 // reset thrash counter when below threshold
		return
	}

	// Thrash detection: stop if we've compacted too many times consecutively.
	if e.compactCount >= maxConsecutiveCompactions {
		e.logCompaction("thrash-skip", len(snap), tokens, len(snap))
		return
	}

	beforeMsgs := len(snap)
	beforeTokens := tokens

	e.hooks.Fire(ctx, hooks.PreCompact, hooks.Input{
		Event: hooks.PreCompact, SessionID: e.sessionID,
	})
	summarizer := compact.NewSummarizer(e.provider, e.model)
	compacted, err := summarizer.Compact(ctx, snap, 5)
	if err != nil {
		mc := compact.NewMicrocompactor(10)
		e.msgMu.Lock()
		e.messages = mc.Apply(e.messages)
		afterMsgs := len(e.messages)
		e.msgMu.Unlock()
		e.logCompaction("micro", beforeMsgs, beforeTokens, afterMsgs)
		e.compactCount++
		return
	}
	e.msgMu.Lock()
	e.messages = compacted
	afterMsgs := len(e.messages)
	e.msgMu.Unlock()
	e.logCompaction("llm", beforeMsgs, beforeTokens, afterMsgs)
	e.compactCount++
}

// contextWindowSize returns the model's context window in tokens.
// Priority: config override > cached API query > model-name heuristic.
func (e *Engine) contextWindowSize() int {
	if e.cfg.ContextWindow > 0 {
		return e.cfg.ContextWindow
	}

	// Try to get from provider API (cached after first call)
	if e.cachedContextWindow > 0 {
		return e.cachedContextWindow
	}

	// Query provider's /v1/models endpoint (skip for localhost test servers)
	provName, _ := parseModel(e.cfg.Model)
	if pcfg, ok := e.cfg.Provider[provName]; ok && !strings.Contains(pcfg.BaseURL, "127.0.0.1") {
		info := provider.FetchModelInfo(pcfg.BaseURL, pcfg.APIKey, e.model)
		if info != nil && info.ContextSize() > 0 {
			e.cachedContextWindow = info.ContextSize()
			return e.cachedContextWindow
		}
	}

	// Fallback: model-name heuristic
	model := strings.ToLower(e.model)
	switch {
	case strings.Contains(model, "gpt-5"):
		return 1000000
	case strings.Contains(model, "claude"):
		return 200000
	case strings.Contains(model, "minimax-m2"):
		return 1000000 // MiniMax M2-7: 1M context
	case strings.Contains(model, "glm-4"):
		return 128000 // GLM-4.7: 128K
	case strings.Contains(model, "kimi"), strings.Contains(model, "k2"):
		return 131072 // Kimi K2: 128K
	case strings.Contains(model, "deepseek"):
		return 64000
	default:
		return 128000
	}
}

// ContextWindowSize exposes the context window size for TUI/HUD use.
func (e *Engine) ContextWindowSize() int {
	return e.contextWindowSize()
}

func (e *Engine) maybeCompact(ctx context.Context) {
	// Token-based trigger using model-specific context window (like Codex).
	// Also check raw request size — pre-turn compaction has a 15MB byte
	// guard to prevent oversized requests; post-tool compaction needs the
	// same guard or large byte-heavy tool results (file dumps, screenshots,
	// page snapshots) can blow past the byte cap with a low token estimate.
	snap := e.messagesSnapshot()
	tokens := compact.EstimateTokens(snap)
	threshold := e.contextWindowSize() * 7 / 10 // 70% of context window
	if e.cfg.CompactThreshold > 0 {
		threshold = e.cfg.CompactThreshold
	}
	bytes := e.messageBytes()
	if tokens < threshold && len(snap) < 100 && bytes < maxRequestBytes {
		return
	}

	beforeMsgs := len(snap)
	beforeTokens := tokens

	e.hooks.Fire(ctx, hooks.PreCompact, hooks.Input{
		Event: hooks.PreCompact, SessionID: e.sessionID,
	})

	// Try LLM-summarized compaction first (like Codex)
	summarizer := compact.NewSummarizer(e.provider, e.model)
	compacted, err := summarizer.Compact(ctx, snap, 5)
	if err != nil {
		// Fallback to mechanical compaction
		mc := compact.NewMicrocompactor(20)
		e.msgMu.Lock()
		e.messages = mc.Apply(e.messages)
		afterMsgs := len(e.messages)
		e.msgMu.Unlock()
		e.logCompaction("micro", beforeMsgs, beforeTokens, afterMsgs)
		return
	}
	e.msgMu.Lock()
	e.messages = compacted
	afterMsgs := len(e.messages)
	e.msgMu.Unlock()
	e.logCompaction("llm", beforeMsgs, beforeTokens, afterMsgs)
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
	if turn.InputTokens == 0 && turn.OutputTokens == 0 &&
		turn.CacheCreationTokens == 0 && turn.CacheReadTokens == 0 {
		return
	}
	// Pass cache tokens through so Anthropic prompt-caching is billed
	// correctly. Without this the tracker silently undercounted by
	// roughly 10x for cache-heavy turns.
	e.cost.RecordTurnWithCache(
		e.model,
		turn.InputTokens, turn.OutputTokens,
		turn.CacheCreationTokens, turn.CacheReadTokens,
	)
	if e.tokenBudget != nil {
		e.tokenBudget.Consume(
			turn.InputTokens + turn.OutputTokens +
				turn.CacheCreationTokens + turn.CacheReadTokens,
		)
	}
	// Phase 8: consume the turn cost into the session-wide USD
	// budget. Reads the just-appended TurnCost from the tracker
	// so each Engine (parent + subagents) reports only its own
	// delta to the shared budget — avoids the race where the
	// "session total minus budget used" diff would compute
	// wrong cross-engine totals under parallel turns.
	if e.costBudget != nil {
		turns := e.cost.Turns()
		if len(turns) > 0 {
			last := turns[len(turns)-1]
			if last.CostUSD > 0 {
				e.costBudget.Consume(last.CostUSD)
			}
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
	// Skip failed tool calls — recording them as if they succeeded
	// corrupts the audit trail. The dispatcher uses Result.Error to
	// signal failure; some tools also leak "Error: ..." through Output.
	if r.Error != nil {
		return
	}
	if strings.HasPrefix(strings.TrimSpace(r.Output), "Error:") {
		return
	}
	path := extractPath(tc.Input)
	if path == "" {
		return
	}
	// Read the post-edit content so the journal stores the real file
	// state instead of the tool's stdout ("wrote 42 bytes to foo.go").
	// Pre-image is left empty here for backward compatibility — a
	// proper before/after capture would have to read the file BEFORE
	// dispatch and thread it through.
	after := ""
	if data, err := os.ReadFile(path); err == nil {
		after = string(data)
	}
	// Action is based on whether the file existed before the tool ran.
	// We can no longer detect that here (post-tool), so use the tool
	// name as the best signal: write usually creates, edit modifies.
	action := "modify"
	if name == "write" {
		action = "create"
	}
	e.journal.Record(name, path, action, "", after)
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
		return newOpenAICompat(cfg, "deepseek", "https://api.deepseek.com")
	case "zhipu", "glm":
		// GLM coding plan: Anthropic-compat at api.z.ai/api/anthropic
		// Regular API: OpenAI-compat at open.bigmodel.cn
		return newChineseProvider(cfg, "zhipu",
			"https://open.bigmodel.cn/api/paas/v4",
			"https://api.z.ai/api/anthropic")
	case "moonshot", "kimi":
		// Kimi coding plan: Anthropic-compat at api.kimi.com/coding/
		// Regular API: OpenAI-compat at api.moonshot.cn/v1
		return newChineseProvider(cfg, "moonshot",
			"https://api.moonshot.cn/v1",
			"https://api.kimi.com/coding/")
	case "minimax":
		// MiniMax coding plan: Anthropic-compat at api.minimax.io/anthropic
		// Regular API: OpenAI-compat at api.minimax.io/v1
		return newChineseProvider(cfg, "minimax",
			"https://api.minimax.io/v1",
			"https://api.minimax.io/anthropic")
	case "altllm":
		return newOpenAICompat(cfg, "altllm", "https://api.altllm.ai")
	case "qwen", "dashscope":
		return newOpenAICompat(cfg, "qwen", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	case "ollama":
		return newOpenAICompat(cfg, "ollama", "http://localhost:11434")
	case "lmstudio":
		return newOpenAICompat(cfg, "lmstudio", "http://localhost:1234")
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
			slog.Warn("unknown provider, falling back to OpenAI-compatible",
				"provider", name)
			return provider.NewOpenAI(provider.OpenAIConfig{
				APIKey: pcfg.APIKey, BaseURL: pcfg.BaseURL,
			}), nil
		}
		return nil, fmt.Errorf("unsupported provider: %s (set API key via config or env)", name)
	}
}

// newChineseProvider creates a provider for Chinese AI services that offer
// both OpenAI-compatible and Anthropic-compatible (coding plan) endpoints.
// If the user's baseURL contains "/anthropic" or "/coding/", use the Anthropic
// provider. Otherwise use OpenAI-compatible.
func newChineseProvider(
	cfg *config.Config,
	name, defaultOpenAIBase, defaultAnthropicBase string,
) (provider.Provider, error) {
	pcfg := cfg.Provider[name]
	if pcfg.APIKey == "" {
		return nil, fmt.Errorf("provider %s: API key not configured (set %s_API_KEY)", name, strings.ToUpper(name))
	}
	base := pcfg.BaseURL

	// Auto-detect: if baseURL points to an Anthropic-compat endpoint, use Anthropic provider
	if base != "" && (strings.Contains(base, "/anthropic") || strings.Contains(base, "/coding")) {
		return provider.NewAnthropic(provider.AnthropicConfig{
			APIKey: pcfg.APIKey, BaseURL: base,
		}), nil
	}

	// Default: OpenAI-compatible
	if base == "" {
		base = defaultOpenAIBase
	}
	return provider.NewOpenAI(provider.OpenAIConfig{
		APIKey: pcfg.APIKey, BaseURL: base,
	}), nil
}

func newOpenAICompat(cfg *config.Config, name, defaultBase string) (provider.Provider, error) {
	pcfg := cfg.Provider[name]
	if pcfg.APIKey == "" {
		return nil, fmt.Errorf("provider %s: API key not configured (set %s_API_KEY)", name, strings.ToUpper(name))
	}
	base := pcfg.BaseURL
	if base == "" {
		base = defaultBase
	}
	return provider.NewOpenAI(provider.OpenAIConfig{
		APIKey: pcfg.APIKey, BaseURL: base,
	}), nil
}

// buildTurnSummary creates a structured summary for the completed turn.
// Section 17.5 of the design doc: what changed, verification, risks.
func (e *Engine) buildTurnSummary(intent TaskIntent) string {
	entries := e.journal.Entries()
	if len(entries) == 0 && intent.Class == TaskQA {
		return "" // no summary needed for pure Q&A
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s | risk:%s]", intent.Description, riskName(intent.Risk)))

	if len(entries) > 0 {
		sb.WriteString(fmt.Sprintf(" %d file(s) changed", len(entries)))
	}

	cost := e.cost.Summary()
	if cost != "" {
		sb.WriteString(" | " + cost)
	}

	return sb.String()
}

// logCompaction records a compaction event for debugging and audit.
func (e *Engine) logCompaction(method string, beforeMsgs, beforeTokens, afterMsgs int) {
	afterTokens := compact.EstimateTokens(e.messagesSnapshot())
	if os.Getenv("ALTCODE_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[debug] compaction (%s): %d→%d msgs, ~%d→~%d tokens\n",
			method, beforeMsgs, afterMsgs, beforeTokens, afterTokens)
	}
	// Record in journal for /stats visibility
	e.journal.Record("compact", method,
		fmt.Sprintf("%d→%d msgs", beforeMsgs, afterMsgs),
		fmt.Sprintf("~%d→~%d tokens", beforeTokens, afterTokens), "")
}

func riskName(r RiskLevel) string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "HIGH"
	default:
		return "unknown"
	}
}

func parseModel(model string) (providerName, modelName string) {
	for i, c := range model {
		if c == '/' {
			return model[:i], model[i+1:]
		}
	}
	// Infer provider from model name prefix for known providers.
	lower := strings.ToLower(model)
	for _, prefix := range []string{
		"altllm", "deepseek", "moonshot", "kimi", "minimax",
		"zhipu", "glm", "qwen", "dashscope", "ollama",
	} {
		if strings.HasPrefix(lower, prefix) {
			return prefix, model
		}
	}
	return "anthropic", model
}
