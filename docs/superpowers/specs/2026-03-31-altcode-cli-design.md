# Altcode CLI/TUI Design Specification

**Date**: 2026-03-31
**Status**: Draft
**Language**: Go
**Priority**: Fast (ops), Smart (agent), Small (size)

---

## 1. Overview

Altcode is a minimal, blazing-fast CLI/TUI for AI-assisted coding. It supports Claude, Codex, Gemini, and any OpenAI-compatible model. Built in Go with Bubbletea for the terminal UI, it ships as a single static binary under 15MB with sub-50ms startup.

### Design Principles

1. **Channel-pipeline architecture** — agent loop emits events on Go channels; TUI and SDK consume the same interface
2. **Goroutine-native concurrency** — parallel tool execution, streaming, subagents via goroutines + errgroup
3. **Zero runtime dependencies** — single static binary, pure Go SQLite, no CGO
4. **Best-of-both-worlds** — Claude Code's agent patterns + OpenCode's client-server separation
5. **Compatibility** — reads CLAUDE.md, AGENTS.md, and MCP configs from existing projects

### Comparison

| | Claude Code | OpenCode | **Altcode** |
|---|---|---|---|
| Language | TypeScript/Bun | TypeScript/Bun | **Go** |
| Binary | 785KB + Bun (~50MB) | Bun compile (~50MB) | **~12MB standalone** |
| Cold start | ~200ms | ~150ms | **<50ms** |
| Memory idle | ~80MB (V8) | ~60MB (Bun) | **<20MB** |
| TUI framework | Custom Ink/React | OpenTUI/SolidJS | **Bubbletea** |
| Dependencies | npm | npm (Bun) | **Zero** |
| Cross-compile | Complex | Complex | **Trivial** |
| Agent concurrency | async generators | Effect fibers | **Goroutines** |

---

## 2. Project Structure

```
altcode/
├── cmd/altcode/             # main.go — entry point, CLI parsing
├── internal/
│   ├── engine/              # Agent loop, session management
│   │   ├── engine.go        # Core Engine type, Run(), loop()
│   │   ├── runner.go        # Per-session serialized execution
│   │   ├── compact.go       # Context compaction (3 layers)
│   │   └── fork.go          # Subagent forking
│   ├── provider/            # LLM provider abstraction
│   │   ├── provider.go      # Provider interface
│   │   ├── anthropic.go     # Anthropic Messages API
│   │   ├── openai.go        # OpenAI Chat/Responses API
│   │   ├── gemini.go        # Google Gemini API
│   │   ├── compat.go        # OpenAI-compatible endpoints
│   │   ├── transform.go     # Per-provider request/response transforms
│   │   ├── stream.go        # SSE stream decoder
│   │   └── retry.go         # Exponential backoff + retry
│   ├── tool/                # Tool interface + implementations
│   │   ├── tool.go          # Tool interface, Result type
│   │   ├── registry.go      # Tool registry, dispatch
│   │   ├── dispatch.go      # Concurrency partitioning
│   │   ├── bash.go          # Shell execution with PTY
│   │   ├── read.go          # File read with line ranges
│   │   ├── edit.go          # Search-and-replace edits
│   │   ├── write.go         # Full file write
│   │   ├── glob.go          # File pattern matching
│   │   ├── grep.go          # ripgrep-powered search
│   │   ├── ls.go            # Directory listing
│   │   ├── fetch.go         # HTTP fetch, HTML->markdown
│   │   ├── search.go        # Web search
│   │   ├── agent.go         # Spawn subagent
│   │   ├── task.go          # Background task management
│   │   ├── ask.go           # Ask user blocking question
│   │   ├── plan.go          # Enter/exit plan mode
│   │   └── mcp.go           # MCP tool bridge
│   ├── permission/          # Permission rules, glob matching
│   │   ├── permission.go    # Rule evaluation, modes
│   │   ├── rules.go         # Rule parsing, glob patterns
│   │   ├── defaults.go      # Default allow rules
│   │   └── doom.go          # Doom loop detection
│   ├── tui/                 # Bubbletea TUI components
│   │   ├── app.go           # Root App model
│   │   ├── messages.go      # Message list (virtualized)
│   │   ├── prompt.go        # Multi-line input with history
│   │   ├── palette.go       # Command palette (Ctrl+K)
│   │   ├── permission.go    # Permission dialog
│   │   ├── header.go        # Model, tokens, cost, context %
│   │   ├── status.go        # Spinner, current tool, agent depth
│   │   ├── theme.go         # Theme system + built-in themes
│   │   ├── markdown.go      # Streaming markdown renderer
│   │   ├── diff.go          # Split-pane diff view
│   │   └── keybinds.go      # Keybinding registry
│   ├── config/              # Config loading, project detection
│   │   ├── config.go        # Config struct, JSONC loading
│   │   ├── hierarchy.go     # Config cascade (CLI > project > user > defaults)
│   │   ├── project.go       # Project root detection (git)
│   │   └── instructions.go  # CLAUDE.md / ALTCODE.md loading
│   ├── context/             # System prompt assembly
│   │   ├── system.go        # Static/dynamic prompt split
│   │   ├── sections.go      # Cached prompt sections
│   │   └── env.go           # Environment context (cwd, date, git)
│   ├── compact/             # Context compaction strategies
│   │   ├── budget.go        # Tool result budget (layer 1)
│   │   ├── micro.go         # Microcompact — prune old tool results (layer 2)
│   │   └── auto.go          # Auto-compact via summarization agent (layer 3)
│   ├── mcp/                 # MCP client
│   │   ├── client.go        # MCP client, transport management
│   │   ├── stdio.go         # Stdio transport (subprocess)
│   │   ├── http.go          # SSE + StreamableHTTP transport
│   │   └── bridge.go        # Tool bridging into registry
│   ├── store/               # SQLite storage
│   │   ├── db.go            # Database connection, pragmas, migrations
│   │   ├── session.go       # Session CRUD
│   │   ├── message.go       # Message CRUD
│   │   └── permission.go    # Permission rule storage
│   ├── event/               # Event bus (typed channels)
│   │   └── bus.go           # Pub/sub, typed events
│   ├── server/              # Optional HTTP API
│   │   ├── server.go        # Hono-equivalent HTTP server
│   │   ├── routes.go        # REST endpoints
│   │   └── sse.go           # SSE event stream
│   └── lsp/                 # LSP client (optional)
│       ├── client.go        # JSON-RPC client
│       └── diagnostics.go   # Diagnostic aggregation
├── pkg/
│   └── sdk/                 # Public SDK for embedding
│       └── sdk.go           # Client type, Run(), Cancel()
├── themes/                  # Built-in theme JSON files
├── docs/
├── go.mod
└── go.sum
```

---

## 3. Core Architecture

### Channel-Pipeline Pattern

The agent loop runs in a goroutine and emits events on a channel. The TUI and SDK consume the same `<-chan Event` interface:

```go
type Event struct {
    Type      EventType
    Text      string       // TextDelta, TextDone
    ToolCall  *ToolCall    // ToolStart, ToolDelta, ToolDone
    ToolResult *Result     // ToolResult
    Error     error        // Error
    Usage     *Usage       // TokenUsage
    Thinking  string       // ThinkingDelta
    Permission *PermReq    // PermissionRequest
}

type EventType int
const (
    EventTextDelta EventType = iota
    EventTextDone
    EventToolStart
    EventToolDelta
    EventToolDone
    EventToolResult
    EventThinkingDelta
    EventUsage
    EventPermissionRequest
    EventPermissionResponse
    EventError
    EventDone
)
```

### Engine

```go
type Engine struct {
    config     *config.Config
    provider   provider.Provider
    tools      *tool.Registry
    perms      *permission.Evaluator
    store      *store.DB
    bus        *event.Bus
    mcp        *mcp.Manager
    compactor  *compact.Compactor

    // Per-session state
    session    *store.Session
    messages   []provider.Message
    model      string
    mode       permission.Mode
}

func (e *Engine) Run(ctx context.Context, input string) <-chan Event {
    events := make(chan Event, 64)
    go func() {
        defer close(events)
        e.loop(ctx, input, events)
    }()
    return events
}
```

### Agent Loop

```go
func (e *Engine) loop(ctx context.Context, input string, out chan<- Event) {
    // Append user message
    e.appendUserMessage(input)

    for {
        select {
        case <-ctx.Done():
            out <- Event{Type: EventError, Error: ctx.Err()}
            return
        default:
        }

        // 1. Apply compaction layers
        e.compactor.ApplyBudget(&e.messages)       // Layer 1: tool result budget
        e.compactor.Microcompact(&e.messages)       // Layer 2: prune old tool results
        if e.compactor.ShouldAutocompact(e.messages, e.model) {
            e.compactor.Autocompact(ctx, &e.messages) // Layer 3: summarize
        }

        // 2. Build request
        req := e.buildRequest()

        // 3. Stream from provider
        stream, err := e.provider.Stream(ctx, req)
        if err != nil {
            e.handleError(err, out)
            return
        }

        // 4. Process stream events, dispatch tools as they arrive
        toolCalls := e.processStream(ctx, stream, out)

        // 5. No tool calls → done
        if len(toolCalls) == 0 {
            out <- Event{Type: EventDone}
            return
        }

        // 6. Dispatch tools (concurrent reads, serial writes)
        results := e.dispatchTools(ctx, toolCalls, out)

        // 7. Append assistant message + tool results, continue loop
        e.appendToolResults(results)
    }
}
```

### Client-Server Separation

The engine runs in-process by default. With `--serve` or `altcode serve`, it exposes an HTTP API:

```go
// In-process (default): TUI calls engine directly
events := engine.Run(ctx, "fix the bug")

// Remote: TUI talks to HTTP API, same Event channel interface
events := httpClient.Run(ctx, "fix the bug")

// Both implement the same interface:
type Runner interface {
    Run(ctx context.Context, input string) <-chan Event
    Cancel()
}
```

---

## 4. Provider Abstraction

### Interface

```go
type Provider interface {
    Name() string
    Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error)
    CountTokens(ctx context.Context, msgs []Message) (int, error)
    Models() []ModelInfo
}

type Request struct {
    Model       string
    Messages    []Message
    System      []SystemSection
    Tools       []ToolSchema
    MaxTokens   int
    Temperature *float64
    Thinking    *ThinkingConfig
    Metadata    map[string]any  // Provider-specific (beta headers, etc.)
}

type StreamEvent struct {
    Type    StreamEventType
    Delta   string
    ToolUse *ToolCallEvent
    Usage   *UsageInfo
    Error   error
}
```

### Providers at Launch

| Provider | Implementation | Notes |
|---|---|---|
| `anthropic` | Messages API, SSE streaming | Prompt caching, extended thinking, beta headers |
| `openai` | Chat Completions API | GPT-4o, o3. Responses API for Codex/GPT-5+ |
| `gemini` | Google AI Studio generativelanguage API | Gemini 2.5 Pro/Flash |
| `compat` | OpenAI Chat Completions | Any compatible endpoint (Ollama, LM Studio, vLLM, Groq). Model string: `compat/<model-name>`, base URL from config. |

### Provider Transforms

```go
type ProviderTransform interface {
    TransformRequest(req *Request) *Request
    TransformStream(event StreamEvent) StreamEvent
}
```

Per-provider normalizations:
- **Anthropic**: `cache_control` on static system sections, scrub non-alphanumeric from tool IDs, beta headers for thinking/fast mode
- **OpenAI**: Route newer models to Responses API, system prompt in `instructions` for certain modes
- **Gemini**: Different tool call format, no prompt caching support

### Prompt Cache Architecture

System prompt split into static (cached) and dynamic (volatile) sections:

```go
type SystemSection struct {
    Content      string
    CacheControl *CacheControl  // nil for dynamic sections
}

// Static sections (tool descriptions, persona, rules) get:
// cache_control: {type: "ephemeral"}
// Dynamic sections (cwd, date, permission mode) are appended after.
```

On Anthropic, the static prefix is cached server-side across turns — major cost and latency savings.

### Retry Logic

```go
type RetryConfig struct {
    MaxRetries    int           // default: 10
    BaseDelay     time.Duration // default: 500ms
    MaxDelay      time.Duration // default: 30s
    RetryOn529    int           // default: 3 (capacity errors)
}
```

Exponential backoff. Respects `Retry-After` headers. Non-retryable: context overflow, auth errors.

### Streaming Pipeline

```
HTTP Response Body (io.ReadCloser)
    → bufio.Scanner (SSE line splitting)
    → json.Decoder (per event)
    → StreamEvent channel (buffered)
    → Engine processStream() (tool dispatch, text accumulation)
    → Event channel
    → TUI renders / SDK consumes
```

---

## 5. Tool System

### Interface

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage          // JSON Schema
    Execute(ctx context.Context, input json.RawMessage) (*Result, error)
    IsConcurrencySafe() bool
    IsReadOnly() bool
    PermissionPattern(input json.RawMessage) string
}

type Result struct {
    Output   string
    Title    string            // Short description for UI
    Metadata map[string]any
    Error    error
}
```

### Core Tools (14)

| Tool | Concurrent | ReadOnly | Description |
|---|---|---|---|
| `bash` | no | no | Shell execution with PTY |
| `read` | yes | yes | File read with line ranges, image support |
| `edit` | no | no | Search-and-replace edits |
| `write` | no | no | Full file write |
| `glob` | yes | yes | File pattern matching via doublestar |
| `grep` | yes | yes | Content search via ripgrep exec |
| `ls` | yes | yes | Directory listing |
| `fetch` | yes | yes | HTTP fetch, HTML to markdown |
| `search` | yes | yes | Web search |
| `agent` | no | no | Spawn subagent goroutine |
| `task` | no | no | Background task management |
| `ask` | no | yes | Ask user a blocking question |
| `plan` | no | no | Enter/exit plan mode |
| `mcp` | varies | varies | Bridge to MCP server tools |

### Tool Concurrency Dispatch

```go
func (e *Engine) dispatchTools(ctx context.Context, calls []ToolCall, out chan<- Event) []Result {
    batches := partitionByConcurrency(calls)
    var results []Result

    for _, batch := range batches {
        if len(batch) == 1 || !batch[0].Tool.IsConcurrencySafe() {
            // Serial execution
            for _, call := range batch {
                out <- Event{Type: EventToolStart, ToolCall: &call}
                r := call.Tool.Execute(ctx, call.Input)
                out <- Event{Type: EventToolResult, ToolResult: r}
                results = append(results, *r)
            }
        } else {
            // Parallel execution via errgroup
            g, gctx := errgroup.WithContext(ctx)
            batchResults := make([]Result, len(batch))
            for i, call := range batch {
                out <- Event{Type: EventToolStart, ToolCall: &call}
                g.Go(func() error {
                    batchResults[i] = *call.Tool.Execute(gctx, call.Input)
                    return nil
                })
            }
            g.Wait()
            for i, r := range batchResults {
                out <- Event{Type: EventToolResult, ToolResult: &r}
            }
            results = append(results, batchResults...)
        }
    }
    return results
}

func partitionByConcurrency(calls []ToolCall) [][]ToolCall {
    var batches [][]ToolCall
    var current []ToolCall
    currentSafe := true

    for _, call := range calls {
        safe := call.Tool.IsConcurrencySafe()
        if safe == currentSafe && safe {
            current = append(current, call)
        } else {
            if len(current) > 0 {
                batches = append(batches, current)
            }
            current = []ToolCall{call}
            currentSafe = safe
        }
    }
    if len(current) > 0 {
        batches = append(batches, current)
    }
    return batches
}
```

### Streaming Tool Execution

Tools start executing as soon as their `tool_use` block completes in the stream — before the full assistant message finishes. This overlaps network latency with tool execution:

```go
func (e *Engine) processStream(ctx context.Context, stream <-chan StreamEvent, out chan<- Event) []ToolCall {
    var toolCalls []ToolCall
    var pendingTool *ToolCallBuilder
    eagerResults := &sync.Map{} // toolCallID -> *Result (for eager-executed tools)

    for event := range stream {
        switch event.Type {
        case StreamToolCallEnd:
            call := pendingTool.Build()
            toolCalls = append(toolCalls, call)
            // Start execution immediately if concurrent-safe (eager)
            if call.Tool.IsConcurrencySafe() {
                call.Eager = true
                go func(c ToolCall) {
                    r := c.Tool.Execute(ctx, c.Input)
                    eagerResults.Store(c.ID, r)
                    out <- Event{Type: EventToolResult, ToolResult: r}
                }(call)
            }
        // ... text deltas, thinking deltas forwarded to out
        }
    }

    // Attach eager results so dispatchTools skips already-executed tools
    for i := range toolCalls {
        if v, ok := eagerResults.Load(toolCalls[i].ID); ok {
            toolCalls[i].EagerResult = v.(*Result)
        }
    }
    return toolCalls
}

// dispatchTools checks EagerResult before executing:
// if call.EagerResult != nil { use it } else { execute now }
```

### Subagent System

```go
func (e *Engine) Fork(opts ForkOpts) *Engine {
    child := &Engine{
        config:   e.config,
        provider: e.provider,
        tools:    e.tools.Subset(opts.Tools),
        perms:    e.perms.Clone(),
        store:    e.store,
        bus:      e.bus,
        mcp:      e.mcp,
        model:    opts.Model,
        // Fresh conversation history
        messages: []provider.Message{},
    }
    if opts.SystemPrompt != "" {
        child.systemOverride = opts.SystemPrompt
    }
    return child
}
```

Subagents share parent's tool registry (subset), permission rules, MCP connections, and provider. They have independent conversation history and context budget.

---

## 6. Permission System

### Modes

| Mode | Behavior |
|---|---|
| `default` | Ask user for unapproved tools |
| `auto` | Auto-approve based on rules, deny unmatched |
| `bypass` | Skip all checks |
| `plan` | Read-only, write tools blocked |

### Rule Structure

```go
type Rule struct {
    Tool    string  // "bash", "edit", "*"
    Pattern string  // glob: "git *", "src/**"
    Action  Action  // Allow, Deny, Ask
    Source  Source  // CLI, Session, Project, User
}
```

Pattern syntax: `<tool>:<pattern>`
- `bash:git *` — allow git commands
- `bash:npm run *` — allow npm scripts
- `edit:src/**` — allow editing in src/
- `read:*` — allow all reads
- `mcp:github:*` — allow all GitHub MCP tools

### Evaluation Order

1. Check deny rules (instant reject)
2. Check allow rules (instant approve)
3. Check mode: bypass→approve, plan→reject writes, auto→deny unmatched, default→ask user

Priority: CLI flags > session > project (`.altcode/rules.json`) > user (`~/.config/altcode/rules.json`)

### Default Allow Rules

```go
var DefaultRules = []Rule{
    // Deny: external directory writes (outside project root)
    {Tool: "edit", Pattern: "!projectroot/**", Action: Deny},
    {Tool: "write", Pattern: "!projectroot/**", Action: Deny},

    // Allow: read-only operations
    {Tool: "read", Pattern: "*", Action: Allow},
    {Tool: "glob", Pattern: "*", Action: Allow},
    {Tool: "grep", Pattern: "*", Action: Allow},
    {Tool: "ls", Pattern: "*", Action: Allow},
    {Tool: "fetch", Pattern: "*", Action: Allow},

    // Allow: safe git commands
    {Tool: "bash", Pattern: "git status", Action: Allow},
    {Tool: "bash", Pattern: "git diff *", Action: Allow},
    {Tool: "bash", Pattern: "git log *", Action: Allow},
}
// "!projectroot/**" is a magic pattern: matches paths outside the detected project root.
```

### Interactive Rule Persistence ("Remember This Decision")

When the user is prompted for permission in `default` mode, they choose from:

| Key | Action | Persistence |
|---|---|---|
| `y` | Allow this once | Session only |
| `n` | Deny this once | Session only |
| `a` | Always allow this pattern | Saved to project `.altcode/rules.json` |
| `!` | Always allow this tool (any args) | Saved to project `.altcode/rules.json` |

On `a`, the tool's `PermissionPattern()` output is persisted as an allow rule. On `!`, a wildcard rule `<tool>:*` is persisted. Session-scoped decisions are stored in-memory and cleared on exit.

Slash command `/allow <pattern>` and `/deny <pattern>` allow manual rule creation mid-session (persisted to project config).

### Doom Loop Detection

If same tool+args called 3 consecutive times, force ask even in auto mode.

---

## 7. TUI Design

### Framework

Bubbletea (Elm architecture: Model → Update → View) + Lipgloss (styling) + Bubbles (components).

### Component Tree

```
App (root tea.Model)
├── Header         — model name, session title, token cost, context %
├── MessageList    — virtualized scrollable message history
│   └── MessageRow
│       ├── UserMessage    — user text
│       ├── AssistantText  — streaming markdown with code blocks
│       ├── ToolCall       — compact tool invocation display
│       ├── ToolResult     — collapsible tool output
│       ├── ThinkingBlock  — collapsible reasoning (dimmed)
│       └── ErrorMessage   — styled error
├── StatusBar      — spinner, current tool, agent tree depth
├── PermissionDialog — blocking tool approval overlay
├── CommandPalette — fuzzy slash commands (Ctrl+K)
└── PromptInput    — multi-line input with history
```

### Key UX Features

**From Claude Code**:
- Shift+Tab cycles permission modes
- Escape cancels current generation (context.Cancel)
- Virtualized message list
- Streaming markdown rendering
- Context % indicator in header

**From OpenCode**:
- Command palette (Ctrl+K) with fuzzy search
- Theme system with built-in themes (catppuccin, dracula, tokyonight, nord, etc.)
- Session list on home screen with one-keypress resume
- Collapsible tool outputs
- Toast notifications

**New in altcode**:
- Split-pane diff view for edit/write tools
- Agent tree indicator: `agent[2] > explore[1/3]`
- Context-sensitive keybind hints in bottom bar

### Theme System

```go
type Theme struct {
    Name       string
    Primary    lipgloss.Color
    Secondary  lipgloss.Color
    Error      lipgloss.Color
    Warning    lipgloss.Color
    Success    lipgloss.Color
    Muted      lipgloss.Color
    Background lipgloss.Color
    Foreground lipgloss.Color
    Border     lipgloss.Color
}
```

~10 built-in themes. Custom themes from `~/.config/altcode/themes/*.json`. Auto-detect dark/light terminal.

### Rendering Performance

- Bubbletea frame throttle (60fps cap)
- Viewport virtualization (only visible rows)
- Lipgloss style caching (immutable value types)
- Lazy markdown re-render (only changed messages)

### Streaming Markdown Strategy

Custom incremental renderer (not glamour, which requires complete input):
- Parse markdown tokens incrementally as text deltas arrive
- Code blocks use `alecthomas/chroma` for syntax highlighting
- Incomplete fenced blocks render with a "streaming..." indicator
- Only re-render the last (active) message on each delta; completed messages cached as rendered strings
- Prototype this component first — it is the highest-risk TUI element

### Event Channel Backpressure

The `Event` channel (capacity 64) uses a drop-oldest policy for non-critical events (`TextDelta`) when the consumer is slow. Critical events (`ToolResult`, `PermissionRequest`, `Error`, `Done`) are never dropped — they block until consumed. For remote SDK clients over HTTP, the SSE writer maintains a 256-entry ring buffer; if the client falls behind, intermediate text deltas are coalesced.

### Model-Aware Tool Selection

The tool registry accepts a `ModelCapabilities` struct when resolving tools:

```go
type ModelCapabilities struct {
    SupportsToolUse   bool
    SupportsThinking  bool
    MaxOutputTokens   int
    ContextWindow     int
}
```

Tools can declare minimum requirements. Models with weak tool-use (small local models) get a reduced tool set. Future: `apply_patch` tool for models that handle unified diffs better than search-and-replace.

---

## 8. Storage

### SQLite (Pure Go)

`modernc.org/sqlite` — no CGO, trivial cross-compilation.

Location: `~/.local/share/altcode/altcode.db` (XDG data directory).

```sql
CREATE TABLE session (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    title       TEXT,
    model       TEXT,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    summary     TEXT
);

CREATE TABLE message (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES session(id),
    role        TEXT NOT NULL,
    content     BLOB NOT NULL,
    model       TEXT,
    tokens_in   INTEGER,
    tokens_out  INTEGER,
    created_at  INTEGER NOT NULL
);

CREATE TABLE permission (
    id          TEXT PRIMARY KEY,
    source      TEXT NOT NULL,
    tool        TEXT NOT NULL,
    pattern     TEXT NOT NULL,
    action      TEXT NOT NULL,
    created_at  INTEGER NOT NULL
);

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -8000;   -- 8MB (tuned for <20MB idle memory target)
```

---

## 9. Configuration

### Hierarchy (high to low priority)

1. CLI flags (`--model`, `--provider`)
2. Session overrides (`/model`)
3. Project local (`.altcode/config.local.json`, gitignored)
4. Project (`.altcode/config.json`)
5. User (`~/.config/altcode/config.json`)
6. Built-in defaults

Format: JSONC (JSON with comments).

### Schema

```json
{
    "model": "anthropic/claude-sonnet-4-20250514",
    "provider": {
        "anthropic": { "apiKey": "$ANTHROPIC_API_KEY" },
        "openai": { "apiKey": "$OPENAI_API_KEY" },
        "gemini": { "apiKey": "$GEMINI_API_KEY" },
        "compat": {
            "baseURL": "http://localhost:11434/v1",
            "apiKey": "ollama"
        }
    },
    "permission": [
        { "tool": "bash", "pattern": "git *", "action": "allow" }
    ],
    "mcp": {
        "github": {
            "command": "npx",
            "args": ["-y", "@modelcontextprotocol/server-github"],
            "env": { "GITHUB_TOKEN": "$GITHUB_TOKEN" }
        }
    },
    "theme": "catppuccin-mocha",
    "agent": {
        "explore": {
            "model": "anthropic/claude-haiku-4-5-20251001",
            "tools": ["read", "glob", "grep", "ls"]
        }
    },
    "hooks": {
        "post_tool": [
            { "tool": "edit", "command": "go fmt {{.Input.FilePath}}" }
        ]
    }
}
```

Environment variables expanded at load time (`$ANTHROPIC_API_KEY`).

### Instructions Loading

Compatible with existing ecosystems:

```
1. ~/.config/altcode/instructions.md      (user global)
2. <project-root>/CLAUDE.md               (compatibility)
3. <project-root>/AGENTS.md               (compatibility)
4. <project-root>/ALTCODE.md              (native)
5. <project-root>/.altcode/rules/*.md     (modular rules)
6. <cwd>/ALTCODE.md                       (subdirectory override)
```

Supports `@include ./path` directive.

---

## 10. Context Compaction

Three layers, applied in order before each API call:

### Layer 1: Tool Result Budget

Per-session budget (default 512KB). When aggregate tool output exceeds budget, oldest results saved to `.altcode/cache/` and replaced with `[result saved to .altcode/cache/<hash>]`.

### Layer 2: Microcompact

Between turns, remove tool call/result pairs older than N turns (default 10) where the result was successfully processed. Keep recent results and errors.

### Layer 3: Auto-compact

When estimated tokens > (model context window - 16K buffer):
1. Run compaction agent (haiku-class model) to summarize older messages
2. Replace old messages with compact summary
3. Max 3 retry attempts before circuit breaker

---

## 11. MCP Client

### Architecture

```go
type MCPManager struct {
    clients map[string]*MCPClient
}

type MCPClient struct {
    name      string
    transport Transport
    tools     []tool.Tool   // Bridged into tool registry
}

type Transport interface {
    Send(ctx context.Context, msg json.RawMessage) error
    Recv(ctx context.Context) (json.RawMessage, error)
    Close() error
}
```

### Transports

- **Stdio**: Subprocess with stdin/stdout JSON-RPC
- **SSE**: HTTP Server-Sent Events
- **StreamableHTTP**: HTTP with streaming responses

### Tool Bridging

MCP tools registered as `mcp:<server>:<tool>` in the tool registry. Subject to same permission system. Tool list change notifications trigger re-discovery.

### Lazy Initialization

MCP servers start connecting in background goroutines during startup. Tools become available as servers respond. TUI is interactive before MCP is ready.

---

## 12. Hooks

Shell-command hooks configured in `.altcode/config.json`:

```go
type Hook struct {
    Tool    string // optional: filter to specific tool
    Command string // shell command with Go template variables
    Timeout time.Duration // default: 30s
}
```

Hook points:
- `pre_tool` — before tool execution (can block)
- `post_tool` — after tool execution
- `pre_session` — on session start
- `post_session` — on session end
- `on_permission` — on permission decision

Template variables via `text/template`: `{{.Input.Command}}`, `{{.Input.FilePath}}`, `{{.Tool.Name}}`, `{{.Result.Output}}`.

---

## 13. Embeddable SDK

```go
package sdk

type Client struct { ... }
type Options struct {
    Model      string
    Provider   string
    ConfigPath string
    WorkDir    string
    Tools      []string  // tool subset
    Permission PermissionMode
}

func New(opts Options) (*Client, error)
func (c *Client) Run(ctx context.Context, prompt string) <-chan Event
func (c *Client) Cancel()
func (c *Client) SendMessage(msg string) <-chan Event
func (c *Client) SetModel(model string)
func (c *Client) SetPermissionMode(mode PermissionMode)
```

### HTTP API (optional)

`altcode serve` or `--serve` flag:

```
GET  /api/sessions          — list sessions
POST /api/sessions/:id/run  — send message
GET  /api/events            — SSE event stream
POST /api/cancel            — cancel generation
GET  /api/config            — current config
```

Enables `altcode attach <url>` for remote sessions.

---

## 14. Startup Sequence

```
t=0ms   Parse CLI args (cobra)
t=1ms   ┬─ Load config (file read + merge)
        ├─ Detect project root (git rev-parse)
        ├─ Open SQLite DB (WAL mode)
        └─ Detect terminal capabilities
t=10ms  ┬─ Load instructions (CLAUDE.md, ALTCODE.md)
        ├─ Initialize provider (validate API key)
        └─ Start MCP servers (background goroutines)
t=20ms  Build system prompt (static sections cached)
t=30ms  TUI ready — render prompt input
t=???   MCP tools arrive (background, lazy)
```

Target: prompt visible in <50ms.

### Graceful Shutdown

On `SIGINT`/`SIGTERM`:
1. Cancel the root `context.Context` (stops agent loop, provider stream, tool execution)
2. Wait up to 5s for in-flight tool executions to complete (drain via `sync.WaitGroup`)
3. Flush pending session writes to SQLite
4. Send `SIGTERM` to MCP subprocess children, wait 2s, then `SIGKILL`
5. Close SQLite connection (WAL checkpoint)
6. Exit

The TUI captures `SIGINT` (Ctrl+C) first — single press cancels current generation, double press within 500ms exits the app.

---

## 15. Build & Distribution

```bash
# Static binary, all platforms
GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/altcode-linux-amd64   ./cmd/altcode
GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o dist/altcode-darwin-arm64  ./cmd/altcode
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/altcode-windows.exe  ./cmd/altcode
```

Install methods:
- `go install github.com/user/altcode@latest`
- `brew install altcode`
- `curl -fsSL https://altcode.dev/install.sh | sh`
- GitHub Releases (prebuilt binaries + checksums)

Target binary size: ~12-15MB stripped.

---

## 16. What Comes From Where

### From Claude Code
- Agent-as-stream pattern (async generator → Go channels)
- Permission glob syntax (`bash:git *`)
- Tool result budget (overflow to disk)
- Prompt cache split (static/dynamic boundary)
- Streaming tool execution (start before stream finishes)
- Tool concurrency partitioning (concurrent reads, serial writes)
- CLAUDE.md hierarchy loading
- Context compaction (3 layers: budget, microcompact, autocompact)
- Doom loop detection (3 consecutive identical calls)
- Subagent forking with shared caches

### From OpenCode
- Client-server separation (in-process + HTTP API)
- SSE event stream for remote clients
- Theme system with built-in themes
- Command palette (Ctrl+K)
- Session forking and resume
- SQLite with WAL mode + Drizzle-like pragmas
- Config cascade with JSONC format
- Per-project instance isolation
- Collapsible tool outputs
- MCP lazy initialization

### New in Altcode
- Go channels (not async generators or Effect fibers)
- <50ms cold start, <15MB binary, <20MB idle memory
- Zero runtime dependencies (single static binary)
- Split-pane diff view for edits
- Agent tree indicator in status bar
- Shell-based hooks (not npm plugins)
- Embeddable Go SDK (`pkg/sdk/`)
- Pure Go SQLite (no CGO, trivial cross-compile)
