# Claude Code: Complete Architecture & Implementation Analysis

## Executive Summary

Claude Code is a production-grade, 785KB single-file TypeScript CLI for AI-assisted coding, built on Bun and React/Ink. It is a deeply engineered agentic runtime with a custom terminal renderer, multi-agent orchestration, an ML-based permission classifier, prompt-cache-aware context management, background memory consolidation, and compile-time feature gating via Bun's dead-code elimination.

---

## 1. Project Structure

### Top-Level Directory Layout

```
claude-code/
├── main.tsx                    # 785KB entry point, startup, CLI argument parsing
├── query.ts                    # Core agent loop (the "heart")
├── QueryEngine.ts              # SDK/headless query lifecycle wrapper
├── Tool.ts                     # Tool interface, ToolUseContext, buildTool()
├── Task.ts                     # Background task type system
├── commands.ts                 # Slash command registry
├── context.ts                  # System/user context builders
├── cost-tracker.ts             # Token cost tracking
├── ink.ts                      # Custom Ink wrapper with ThemeProvider
│
├── bootstrap/
│   └── state.ts                # Global singleton state (session, usage, telemetry)
│
├── components/                 # 100+ React/Ink UI components
│   ├── App.tsx                 # Root UI wrapper
│   ├── PromptInput/            # Multi-file input component
│   ├── Message.tsx             # Message rendering
│   ├── MessageRow.tsx          # Row-level layout
│   ├── permissions/            # Permission request dialogs
│   └── design-system/          # ThemedBox, ThemedText, ThemeProvider
│
├── screens/
│   ├── REPL.tsx                # Main interactive REPL (largest component)
│   ├── ResumeConversation.tsx  # Session resume screen
│   └── Doctor.tsx              # Diagnostics screen
│
├── constants/
│   ├── betas.ts                # API beta header strings
│   ├── prompts.ts              # System prompt assembly
│   ├── systemPromptSections.ts # Memoized prompt section cache
│   ├── cyberRiskInstruction.ts # Security policy
│   ├── tools.ts                # Tool name lists
│   └── xml.ts                  # XML tag constants
│
├── tools/                      # 40+ tool implementations
│   ├── AgentTool/              # Subagent spawning
│   ├── BashTool/               # Shell execution
│   ├── FileEditTool/           # Targeted file editing
│   ├── FileReadTool/           # File reading with image support
│   ├── FileWriteTool/          # Full file writes
│   ├── GlobTool/               # File pattern matching
│   ├── GrepTool/               # Content search
│   ├── WebFetchTool/           # HTTP fetching
│   ├── WebSearchTool/          # Web search
│   ├── LSPTool/                # Language Server Protocol
│   ├── NotebookEditTool/       # Jupyter notebooks
│   ├── REPLTool/               # Interactive REPL
│   ├── ToolSearchTool/         # Tool discovery (deferred tools)
│   ├── EnterPlanModeTool/      # Plan mode entry
│   ├── ExitPlanModeV2Tool/     # Plan mode exit
│   ├── TaskCreateTool/         # Background task creation
│   ├── TaskGetTool/            # Task status queries
│   ├── SendMessageTool/        # Agent-to-agent messaging
│   ├── TeamCreateTool/         # Agent swarm creation
│   └── ...
│
├── services/
│   ├── api/
│   │   ├── claude.ts           # Core Anthropic API client
│   │   ├── client.ts           # HTTP client configuration
│   │   ├── withRetry.ts        # Retry logic with backoff
│   │   ├── errors.ts           # Error classification
│   │   └── logging.ts          # API request/response logging
│   ├── mcp/
│   │   ├── client.ts           # MCP server connection management
│   │   ├── config.ts           # MCP configuration loading
│   │   ├── auth.ts             # OAuth for MCP servers
│   │   └── types.ts            # MCP type definitions
│   ├── compact/
│   │   ├── compact.ts          # Conversation compaction engine
│   │   ├── autoCompact.ts      # Auto-compact threshold logic
│   │   └── microCompact.ts     # Cached microcompact
│   ├── analytics/
│   │   ├── growthbook.ts       # Feature flags via GrowthBook
│   │   └── index.ts            # Event logging
│   ├── autoDream/
│   │   ├── autoDream.ts        # Memory consolidation scheduler
│   │   └── consolidationPrompt.ts # Dream subagent prompt
│   └── lsp/                    # Language Server Protocol integration
│
├── utils/
│   ├── config.ts               # Global + project config (JSON)
│   ├── claudemd.ts             # CLAUDE.md loading and hierarchy
│   ├── permissions/            # Permission system (25+ files)
│   ├── processUserInput/       # Input processing pipeline
│   ├── messages.ts             # Message creation utilities
│   ├── sessionStorage.ts       # JSONL transcript persistence
│   ├── model/                  # Model selection, cost, context
│   ├── hooks/                  # Pre/post tool hooks
│   ├── forkedAgent.ts          # Subagent context creation
│   ├── thinking.ts             # Extended thinking config
│   └── toolResultStorage.ts    # Tool result budget management
│
├── coordinator/
│   └── coordinatorMode.ts      # Multi-agent coordinator mode
│
├── ink/                        # Custom Ink terminal renderer
│   ├── ink.tsx                 # Main Ink class
│   ├── reconciler.ts           # React reconciler (yoga layout)
│   ├── dom.ts                  # Virtual DOM
│   ├── screen.ts               # Screen buffer (cell-based)
│   ├── selection.ts            # Text selection
│   └── terminal.ts             # Terminal capability detection
│
├── entrypoints/
│   ├── agentSdkTypes.ts        # Public SDK API types
│   ├── sdk/
│   │   ├── coreTypes.ts        # Serializable message types
│   │   └── runtimeTypes.ts     # Callback/interface types
│   └── init.ts                 # Initialization (telemetry, GrowthBook)
│
├── tasks/                      # Background task implementations
│   ├── LocalShellTask/         # Bash background tasks
│   ├── LocalAgentTask/         # Async agent tasks
│   ├── InProcessTeammateTask/  # In-process swarm members
│   └── DreamTask/              # autoDream task
│
├── memdir/                     # Memory directory management
├── migrations/                 # Config migration scripts
├── hooks/                      # React hooks for UI
└── state/
    ├── AppState.tsx            # AppState context/provider
    ├── AppStateStore.ts        # AppState shape definition
    └── store.ts                # Store implementation
```

### Build System

Built with **Bun**. Key mechanism: `feature()` from `bun:bundle` — compile-time constant-folding. Every `feature('FLAG_NAME')` evaluates to `true`/`false` at bundle time, enabling dead-code elimination of entire subsystems. Internal features like `KAIROS`, `BRIDGE_MODE`, `COORDINATOR_MODE`, `BUDDY` are completely absent from external builds.

---

## 2. Architecture

### Architectural Pattern

Reactive, event-driven architecture with functional state management:

```
Entry Point (main.tsx)
       ↓
   Init Layer (GrowthBook, auth, MCP, tools)
       ↓
   Screen Layer (REPL.tsx — React/Ink)
       ↓
   Input Processing (processUserInput → parseSlashCommand → query)
       ↓
   Agent Loop (query.ts → queryLoop)
       ↓
   Tool Orchestration (toolOrchestration.ts → toolExecution.ts)
       ↓
   API Layer (services/api/claude.ts → Anthropic SDK)
       ↓
   Permission System (useCanUseTool → permissions.ts)
```

### Global State Architecture

Two distinct state systems:

1. **Bootstrap State** (`bootstrap/state.ts`): Module-level singleton with ~60 fields. Session-scoped: cost, usage, session ID, model overrides, prompt cache latches, telemetry counters. Accessed via exported getter/setter functions.

2. **AppState** (`state/AppStateStore.ts`): React context state for UI-bound data. Custom `Store` class provided via `AppStateProvider`. Contains: tool permission context, MCP clients, active tasks, messages, speculation state, effort value, fast mode, plugins, todo lists.

Bootstrap state is process-wide; AppState is React-tree scoped.

### Module Dependency Graph

```
main.tsx
  → query.ts              (agent loop)
    → services/api/claude.ts  (API calls)
    → services/tools/toolOrchestration.ts  (parallel tool execution)
      → services/tools/toolExecution.ts    (single tool execution)
        → hooks/useCanUseTool.ts           (permission gate)
          → utils/permissions/permissions.ts
    → services/compact/autoCompact.ts     (context window management)
    → utils/attachments.ts               (CLAUDE.md injection)
  → Tool.ts               (tool interface)
  → services/mcp/client.ts  (MCP connections)
  → bootstrap/state.ts    (global state)
  → utils/config.ts       (project/global config)
  → utils/claudemd.ts     (memory file loading)
```

---

## 3. TUI/Terminal UI

### Technology: Custom Ink Fork

Ships a **completely custom terminal renderer** in `ink/`:

- **React reconciler** (`ink/reconciler.ts`): `react-reconciler` with custom host environment. Nodes backed by Yoga (CSS Flexbox via WebAssembly/native).
- **Screen buffer** (`ink/screen.ts`): Cell-based rendering with `CellWidth`, `CharPool`, `StylePool`, `HyperlinkPool`.
- **Frame system** (`ink/frame.ts`): Diffs between frames, writes only changed cells (`writeDiffToTerminal`).
- **Selection system** (`ink/selection.ts`): Full text selection — start anchor, focus, word/line selection, URL detection, clipboard copy.
- **Optimizer** (`ink/optimizer.ts`): Post-layout optimization pass.
- **Custom Ink class** (`ink/ink.tsx`): 800+ lines managing render loop (throttled `FRAME_INTERVAL_MS`), keyboard dispatch, mouse tracking, cursor, alt-screen, scroll-to-follow, resize.

### Component Hierarchy

```
ThemeProvider (ink.ts wrapper)
  └── App (components/App.tsx)
        ├── FpsMetricsProvider
        ├── StatsProvider
        └── AppStateProvider
              └── REPL.tsx (screens/REPL.tsx — the main screen)
                    ├── KeybindingSetup
                    ├── GlobalKeybindingHandlers
                    ├── VirtualMessageList (virtualized message scroll)
                    │     └── MessageRow → Message (tool use, text, errors)
                    ├── PromptInput
                    │     ├── BaseTextInput (cursor, vim mode)
                    │     ├── PromptInputFooter (model, cost, context %)
                    │     └── ShimmeredInput (streaming suggestion overlay)
                    ├── PermissionRequest (tool approval dialog)
                    ├── Spinner
                    └── Various Dialogs
```

### Rendering Pipeline

1. User types → `useInput` captures keystrokes
2. Submit → `handlePromptSubmit` → `processUserInput` → async generator `query()`
3. REPL.tsx iterates `query()` via `for await`
4. Each yielded `Message` pushed to AppState messages array
5. React re-renders → `VirtualMessageList` renders visible messages
6. Ink reconciler → Yoga layout → screen diff → terminal write

### Input Handling

PromptInput.tsx (~1000+ lines):
- Multi-line input with cursor tracking (custom `Cursor` class)
- Vim mode (normal/insert/visual)
- Arrow key history navigation
- Tab completion for slash commands, `@` file mentions
- Image paste from clipboard
- File drag-and-drop
- AI typeahead suggestion overlay
- Permission mode cycling (Shift+Tab)
- Token budget indicator
- Effort level control

---

## 4. Agent/Tool System

### Tool Interface (`Tool.ts`)

Every tool implements `Tool<Input, Output, Progress>`:

- `call(args, context, canUseTool, parentMessage, onProgress)` → `ToolResult<Output>`
- `checkPermissions(input, context)` — tool-specific permission logic
- `validateInput(input, context)` — validation before permission check
- `isConcurrencySafe(input)` — parallel execution safety
- `isReadOnly(input)` / `isDestructive(input)` — state mutation flags
- `renderToolUseMessage/renderToolResultMessage` — UI rendering
- `toAutoClassifierInput(input)` — compact repr for YOLO classifier
- `preparePermissionMatcher(input)` — pattern matching for rules (e.g., `Bash(git *)`)
- `prompt(options)` — generates tool's system prompt section

### Tool Registry

Assembled via `getAllBaseTools()`:

- **Always present**: Bash, FileRead, FileEdit, FileWrite, Glob, Grep, WebFetch, WebSearch, Notebook, LSP, Agent, AskUserQuestion, EnterPlanMode, ExitPlanMode, MCP tools, Sleep, ToolSearch
- **Feature-gated**: REPLTool, WorkflowTool, SnipTool, team tools
- **MCP tools**: Dynamically created from connected MCP servers as `MCPTool` instances

### Tool Dispatch

Path: `query.ts → runTools() → toolOrchestration.ts → runToolUse() → toolExecution.ts`

**Concurrency model** (`toolOrchestration.ts`):
- Tool calls partitioned into batches via `partitionToolCalls()`
- Consecutive `isConcurrencySafe` tools run concurrently (up to `CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY`, default 10)
- Non-concurrent tools run serially
- Concurrent batches use `all()` — async generator combiner

**Streaming Tool Executor**: When enabled, tool calls dispatched as soon as `tool_use` block arrives in stream (before full assistant message), overlapping execution with streaming.

### Single Tool Execution (`toolExecution.ts`)

1. Parse/validate input via `tool.inputSchema.safeParse()`
2. `validateInput()` (returns error to model if fails)
3. Run `PreToolUse` hooks
4. `canUseTool()` — permission gate
5. Start OTel tracing span
6. Execute `tool.call()`
7. Process result through `processToolResultBlock()` — applies size budget
8. Run `PostToolUse` hooks
9. Yield tool result message

### The Agent Loop (`query.ts`)

`query()` is an async generator. Inner `queryLoop()` runs `while(true)`:

```
Per iteration:
  1. Apply tool result budget
  2. Apply snip compaction (if enabled)
  3. Apply microcompact (cached or classic)
  4. Apply context collapse (if enabled)
  5. Check autocompact threshold → trigger if needed
  6. Check blocking token limit → error if exceeded
  7. Call API (queryModelWithStreaming)
     - Yield stream events
     - StreamingToolExecutor executes tools during stream
  8. Post-sampling hooks
  9. If aborted → yield interruption, return
  10. Handle errors (prompt_too_long → reactive compact, max_output_tokens → resume)
  11. If no tool_use → stop hooks → return Terminal
  12. If tool_use → run tools → append results → continue
```

---

## 5. Model Integration

### API Client (`services/api/claude.ts`)

Core: `queryModelWithStreaming()`:

1. Assembles beta headers (model capabilities, feature flags, latched state)
2. Builds tool schemas (with `tokenEfficientTools` beta)
3. Handles deferred tools (`defer_loading: true`)
4. Applies prompt caching: static sections get `cache_control: {type: 'ephemeral', ttl: '1hour'}`
5. Calls `client.beta.messages.stream()` via `@anthropic-ai/sdk`
6. Yields `StreamEvent` objects

**Prompt cache architecture**: System prompt split at `SYSTEM_PROMPT_DYNAMIC_BOUNDARY`. Static prefix is cached at API. Beta headers "latched" on first use — stay for session to avoid cache busting.

**Extended thinking**: `ThinkingConfig` with `adaptive`/`disabled`/`enabled`. `interleaved-thinking-2025-05-14` beta enables interleaved thinking with tool calls.

**Fast Mode**: `speed: 'fast'` parameter via `BetaOutputConfig`.

**Model selection**: `getMainLoopModel()` from bootstrap state or global config. Agents have per-agent model overrides.

### Retry Logic (`services/api/withRetry.ts`)

- Max retries: 10
- 529 (capacity): 3 retries for foreground sources
- Base delay: 500ms, exponential backoff
- Fast mode: on overage → cooldown + retry without fast mode
- Fallback model: on `FallbackTriggeredError` → switch model + retry

### Provider Support

`getAPIProvider()`: `'anthropic'` (default), `'vertex'`, `'bedrock'`. Each has different beta header handling. Inference profile support for Bedrock.

---

## 6. Permission System

### Permission Modes

- **`default`**: Interactive prompts for unapproved tools
- **`auto`** (AFK mode): ML-based auto-approval via YOLO classifier
- **`bypass`**: Skip permission checks
- **`plan`**: Read-only mode (write tools blocked)

### Permission Decision Flow

1. `useCanUseTool()` → `canUseTool()` → `checkToolPermission()`
2. Evaluates rules: always-deny → always-allow → tool's `checkPermissions()` → mode defaults
3. If `ask`: show `PermissionRequest` dialog (REPL) or deny (SDK)
4. Pre-permission hooks can override

### Rule System

```typescript
{ source: PermissionRuleSource, ruleBehavior: 'allow'|'deny'|'ask', ruleValue: PermissionRuleValue }
```

Sources: `session`, `localSettings`, `userSettings`, `policySettings`, `flagSettings`, `cliArg`, `command`.

`Bash(git *)` syntax: `permissionRuleValueFromString()` parses `ToolName(pattern)`. Each tool's `preparePermissionMatcher(input)` tests against patterns.

### YOLO Classifier

In `auto` mode: separate Claude API call with conversation transcript + pending tool call. Uses small fast model. Returns `allow`/`deny` with reasoning. Results cached with 30min refresh.

### Denial Tracking

After `DENIAL_LIMITS.fallbackToPrompting` consecutive auto-denials → falls back to interactive prompt (prevents infinite denial loops).

---

## 7. Configuration

### Config Hierarchy

- **Global**: `~/.claude/config.json` — API key, model, theme, history, stats (file locking)
- **Project**: `.claude/settings.json` — tool permissions, MCP servers, onboarding
- **Managed**: `/etc/claude-code/managed_config.json` (MDM policies)
- **Remote managed**: Fetched from Anthropic admin API for enterprise
- **Priority** (low→high): managed → user → project → local → CLI args → session → command

### CLAUDE.md Loading

Hierarchical cascade:
1. Managed: `/etc/claude-code/CLAUDE.md`
2. User: `~/.claude/CLAUDE.md`
3. Project: Traverse cwd→root: `CLAUDE.md`, `.claude/CLAUDE.md`, `.claude/rules/*.md`
4. Local: `CLAUDE.local.md` (gitignored)

Supports `@include` directive. Files closer to cwd = higher priority.

### Feature Flags

GrowthBook client initialized with user attributes. `getFeatureValue_CACHED_MAY_BE_STALE()` for non-critical, `getFeatureValue_CACHED_WITH_REFRESH()` for security-critical.

---

## 8. Session Management

### Transcript Persistence

Sessions stored as **JSONL files** in `~/.claude/projects/<hashed-path>/`. Each line = JSON `Entry` (messages, metadata, special records). `recordTranscript(messages)` awaited before API call (resumable if killed).

### Context Window Management

Multiple layers:

1. **Token estimation**: From last API `usage.input_tokens` + new message sizes
2. **Auto-compact**: Triggers at effective context − 13,000 buffer. Max 3 failures.
3. **Manual compact** (`/compact`): Forked subagent summarizes conversation
4. **Microcompact**: Removes old tool results. Cached variant uses cache editing.
5. **Snip compaction**: Removes marked history segments
6. **Context collapse**: Progressive summarization, drains on 413 error
7. **Tool result budget**: Per-conversation budget, overflow saved to disk + stub
8. **max_output_tokens recovery**: Injects "resume" user message (up to 3 attempts)

### Conversation Resume

`/resume` loads past session JSONL, reconstructs messages, resumes with existing context.

---

## 9. Plugin/Extension System

### MCP Servers

Primary extension mechanism. Claude Code is MCP **client**.

**Config sources**: `~/.claude/config.json`, `.mcp.json`, `.claude/settings.json`, Anthropic's official registry.

**Transports**: stdio, SSE, StreamableHTTP, WebSocket. OAuth support. mTLS.

**MCP tools**: `MCPTool` instances with `name: 'mcp__server__tool'`. Can be `alwaysLoad` to bypass deferred loading.

**Elicitation**: MCP servers request user input via `-32042` error code → routed to REPL dialog or SDK callback.

### Hooks

- `PreToolUse` / `PostToolUse`: Before/after tool execution
- `UserPromptSubmit`: On user message
- `Stop` / `StopFailure`: Agent turn completion
- `SessionStart` / `PreCompact` / `PostCompact`: Lifecycle
- `PermissionRequest`: Custom permission logic

### Skills

Markdown-based instruction sets with scripts/templates/references. Loaded as slash commands. Support frontmatter YAML. Semantic search via `EXPERIMENTAL_SKILL_SEARCH`.

---

## 10. Performance

### Startup Optimizations

- **Parallel init**: MDM reading, Keychain prefetch, GrowthBook init — all kicked off before main sequence
- **Module lazy loading**: `require()` inside conditionals/functions
- **System prompt caching**: Computed once, cached until `/clear` or `/compact`
- **Prompt cache latches**: Beta headers sticky-on to prevent mid-session cache busting
- **File state cache**: LRU, cloned for subagents
- **GrowthBook caching**: Deliberate staleness tolerance
- **Tool schema cache**: JSON schemas cached to avoid re-serialization

---

## 11. Key Design Decisions

### REPL-as-Generator Pattern

`REPL.tsx` drives agent loop via `for await (const message of query(...))`. Agent loop is a pure async generator yielding messages. UI and SDK consume identically. Pause/resume trivial (abort controller).

### System Prompt Architecture

Modular assembly:
- **Static cacheable sections**: Tool descriptions, persona, core instructions (cached, prompt-cache-friendly)
- **Dynamic volatile sections**: Working dir, date, MCP instructions, permission mode
- **`SYSTEM_PROMPT_DYNAMIC_BOUNDARY`**: Splits prompt for cache optimization

### Permission Rules as Glob Patterns

`Bash(git *)` syntax: `ToolName(pattern)`. Each tool's `preparePermissionMatcher(input)` creates a matching closure. Users write: `Bash(npm run *)`, `mcp__github__*`, `FileEdit(src/*)`.

### Concurrency Model

Partitioning tool calls into concurrent vs. serial batches (based on `isConcurrencySafe`). Multiple reads run in parallel, writes run serially. Capped at `CLAUDE_CODE_MAX_TOOL_USE_CONCURRENCY` (default 10).

### autoDream (Background Memory Consolidation)

Three-gate trigger (time ≥ 24h, sessions ≥ 5, acquires lock). Forked subagent with read-only bash access consolidates session history into organized markdown.

### Compile-Time Feature Gating

Bun's `feature()` evaluates at build time. Dead code branches, imports, and strings absent from external builds. `excluded-strings.txt` lint enforces no internal strings leak.

### Tool Result Budget

Per-conversation budget for tool result content. Older results overflow to disk, replaced with file-path stub. Prevents unbounded context growth.

### Speculation / Prompt Suggestions

`SpeculationState`: Claude silently starts processing current suggestion before user commits. If user submits matching speculation, result immediately available. Overlaps API latency with user thinking time.
