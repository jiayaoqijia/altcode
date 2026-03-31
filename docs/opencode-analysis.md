# OpenCode Codebase Analysis

## 1. Project Structure and Build System

### Repository Layout

The opencode repository is a **TypeScript/Bun monorepo** managed with Turborepo. The primary runtime is Bun (a fast JavaScript/TypeScript runtime), and the package manager is Bun 1.3.11.

Root manifest: `opencode/package.json`

Workspace packages:
```
opencode/
  packages/
    opencode/          # Core CLI + TUI + server — the main application
    app/               # SolidJS web frontend (browser UI via Vite)
    desktop/           # Tauri desktop wrapper (Rust shell)
    desktop-electron/  # Electron desktop wrapper (alternative)
    ui/                # Shared UI component library
    storybook/         # Storybook for UI components
    plugin/            # Plugin SDK (@opencode-ai/plugin)
    script/            # Utility scripts
    util/              # Shared utilities (@opencode-ai/util)
    sdk/               # TypeScript SDK (@opencode-ai/sdk)
    function/          # Serverless function handlers
    enterprise/        # Enterprise-specific code
    console/app        # Cloud console application
    slack/             # Slack integration
  .opencode/           # OpenCode's own configuration (dogfoods itself)
```

Entry point: `packages/opencode/bin/opencode` → `packages/opencode/src/index.ts`

### Build System

- **Runtime**: Bun (JS runtime + bundler)
- **Package manager**: Bun workspaces
- **Build orchestration**: Turborepo (`turbo.json`)
- **Type checking**: `tsgo` (TypeScript native preview compiler)
- **Bundling**: Custom `packages/opencode/script/build.ts` using Bun's bundler → single standalone binary
- **Database migrations**: `drizzle-kit` — SQL migrations bundled as `OPENCODE_MIGRATIONS` constant

---

## 2. Architecture

### Core Pattern: Effect-ts Service Layers

The most distinctive architectural feature is **Effect** (version 4.0.0-beta.42) — a typed functional programming library providing:

- **Service / DI via `ServiceMap`**: Every module is a typed service
  ```ts
  export class Service extends ServiceMap.Service<Service, Interface>()("@opencode/MyService") {}
  export const layer = Layer.effect(Service, Effect.gen(function* () { ... }))
  ```
- **Scoped resource management**: `Effect.acquireRelease`, `Scope`, `ScopedCache`
- **Structured concurrency**: `Effect.forkScoped`, `Stream`, `PubSub`
- **Error typing**: All errors flow through Effect's typed channel

Key files:
- `src/effect/run-service.ts` — `makeRuntime` wraps Effect layers into promise-returning async functions
- `src/effect/instance-state.ts` — `InstanceState`, a `ScopedCache` keyed by instance directory

### Instance / Multi-Project Architecture

Each opened directory is an "instance" with isolated service state. `InstanceState<A>` is a `ScopedCache<string, A>` where key = `Instance.directory`. Each service (Agent, ToolRegistry, LSP, etc.) has per-project lazy-initialized state automatically invalidated on dispose.

### Server Architecture: Hono HTTP API

Core backend is a **Hono** HTTP server (`src/server/server.ts`) serving:
- REST API routes for sessions, providers, config, tools, MCP
- SSE stream at `/event` for real-time updates
- Basic auth protection (configurable)
- CORS for localhost, Tauri origins, `*.opencode.ai`

**In-process routing**: When running TUI/CLI, a custom `fetch` routes directly to Hono without TCP:
```ts
const fetchFn = async (input, init) => {
  const request = new Request(input, init)
  return Server.Default().fetch(request)
}
const sdk = createOpencodeClient({ baseUrl: "http://opencode.internal", fetch: fetchFn })
```

This enables remote attachment (`opencode run --attach http://server:4096`).

### Module Dependency Graph

```
SessionPrompt
  → Session (storage)
  → Agent (agent configuration)
  → LLM (AI streaming)
    → Provider (model resolution)
  → ToolRegistry (tool dispatch)
    → Tool definitions (bash, edit, read, glob, grep, etc.)
  → Permission (access control)
  → MCP (Model Context Protocol servers)
  → LSP (Language Server Protocol clients)
  → Bus (pub/sub events)
  → SessionCompaction (context pruning)
  → SessionProcessor (stream event handling)
  → Plugin (hook system)
```

---

## 3. TUI / Terminal UI

### Framework: OpenTUI + SolidJS

Uses **OpenTUI** (`@opentui/core`, `@opentui/solid`) — a custom terminal rendering library — with **SolidJS** as the component model.

OpenTUI provides:
- `CliRenderer` rendering at up to 60fps using ANSI escape codes
- Flexbox layout engine for terminal cells
- Mouse event support (click, scroll, drag, selection)
- Keyboard handling including Kitty keyboard protocol
- Debug overlay and console panel
- `BoxRenderable` / `ScrollBoxRenderable` with Shiki syntax highlighting

`@opentui/solid` provides:
- `render()` — mounts SolidJS component tree into CLI renderer
- `useKeyboard()` — reactive keyboard events
- `useTerminalDimensions()` — reactive width/height
- `useRenderer()` — direct renderer access

### Component Provider Tree

```
ErrorBoundary
  ArgsProvider          — CLI args
  ExitProvider          — onExit / onBeforeExit lifecycle
  KVProvider            — Persistent key-value store
  ToastProvider         — Toast notifications
  RouteProvider         — Client-side routing (home / session / plugin)
  TuiConfigProvider     — Parsed tui.json config
  SDKProvider           — HTTP SDK client + SSE event stream
  SyncProvider          — Reactive store of all server state
  ThemeProvider         — Theme colors + dark/light mode
  LocalProvider         — Local UI state
  KeybindProvider       — Keybinding registry
  PromptStashProvider   — Saved prompt drafts
  DialogProvider        — Modal dialog stack
  CommandProvider       — Command palette registry
  FrecencyProvider      — Frecency-ranked suggestions
  PromptHistoryProvider — Prompt input history
  PromptRefProvider     — Ref to active prompt input
    App                 — Main application component
```

### Routing

Simple custom router with discriminated unions:
```ts
type RouteData =
  | { type: "home"; initialPrompt?: ...; workspaceID?: string }
  | { type: "session"; sessionID: string }
  | { type: "plugin"; id: string; data?: Record<string, unknown> }
```

### SSE Event Batching

16ms debounce: if last flush >16ms ago, events process immediately. If <16ms, events queue and flush as single SolidJS `batch()` call → one render frame. Prevents 100+ renders/second during streaming.

### Theme System

30+ built-in themes (catppuccin, dracula, tokyonight, nord, etc.). Auto-detects dark/light via `ESC]11;?BEL`. Custom themes from `~/.config/opencode/themes/` or `.opencode/themes/`.

---

## 4. Agent / Tool System

### Agent Definitions

Agents combine: permission ruleset, optional system prompt, optional model override, mode (`primary`/`subagent`/`all`), temperature, color, hidden flag.

Built-in agents (`src/agent/agent.ts`):

| Agent | Mode | Purpose |
|---|---|---|
| `build` | primary | Default coding agent, all tools |
| `plan` | primary | Plan-only, edits restricted to plan file |
| `general` | subagent | Multi-step research and tasks |
| `explore` | subagent | Fast read-only exploration |
| `compaction` | primary (hidden) | Context compression |
| `title` | primary (hidden) | Session title generation |
| `summary` | primary (hidden) | Session summary generation |

### Tool System

Every tool implements `Tool.Info`:
```ts
interface Info {
  id: string
  init: (ctx?: InitContext) => Promise<Def>
}
interface Def {
  description: string
  parameters: ZodType
  execute(args, ctx: Tool.Context): Promise<{ title, metadata, output, attachments? }>
}
```

`Tool.define()` wraps `execute` to validate input and auto-truncate large output.

**Built-in tools** (`src/tool/registry.ts`):
- `bash` — shell command execution
- `read` — file read with line range
- `write` — full file write
- `edit` — search-and-replace edits (non-GPT-5 models)
- `apply_patch` — unified diff (GPT-5+ models)
- `glob` — file pattern matching
- `grep` — ripgrep-powered search
- `list` (ls) — directory listing
- `webfetch` — HTTP fetch with HTML→markdown
- `websearch` — Exa web search
- `codesearch` — Exa code search
- `task` — spawn subagent (key multi-agent mechanism)
- `todowrite` — structured todo list
- `skill` — inject skill document
- `question` — ask user a blocking question
- `lsp` — LSP diagnostics query (experimental)
- `batch` — parallel tool execution (experimental)
- `plan_exit` — signal plan completion

**Tool selection is model-aware**: GPT-5+ gets `apply_patch` instead of `edit`/`write`.

### Permission System

Ruleset of `{ permission, pattern, action }` triples. Actions: `allow | deny | ask`. Pattern matching via glob against tool arguments.

When `ctx.ask(...)` fires, `Permission.Service` evaluates the ruleset. If `"ask"`, a `permission.asked` bus event emits and the fiber blocks until user replies via API endpoint.

Key permission types: `doom_loop`, `external_directory`, `read`, `edit`, `write`, `bash`, `question`, `plan_enter`, `plan_exit`.

---

## 5. Model Integration

### Provider Abstraction

Wraps **Vercel AI SDK** (`ai` v6.0.138) providing unified `LanguageModelV3` interface.

Supported providers (20+):
- Anthropic, OpenAI (Responses API for GPT-5+), Google Gemini, Vertex, Bedrock, Azure OpenAI
- GitHub Copilot (custom adapter), OpenRouter, xAI Grok, Mistral, Groq
- DeepInfra, Cerebras, Cohere, Together AI, Perplexity
- Vercel AI Gateway, GitLab, Poe
- Any OpenAI-compatible endpoint

Provider definitions from **models.dev** — JSON model registry fetched at startup, cached in `~/.cache/opencode/models.json`.

### Streaming

`LLM.stream()` calls `streamText()` from AI SDK, returns `fullStream` async iterable. Effect-based wrapper uses `Queue.unbounded` to bridge to Effect stream model.

Stream events: `start`, `reasoning-{start,delta,end}`, `tool-input-{start,delta,end}`, `tool-{call,result,error}`, `text-{start,delta,end}`, `step-finish`, `finish`, `error`.

### Provider-Specific Transforms

`ProviderTransform` normalizes per provider:
- **Anthropic**: removes empty content blocks, scrubs non-alphanumeric from tool IDs, enables prompt caching
- **OpenAI GPT-5+**: Responses API instead of Chat Completions
- **Copilot**: system prompt in `instructions` field, disables token counting
- **LiteLLM proxies**: adds dummy tool for validation quirk

### Retry Logic

`SessionRetry`: exponential backoff, base 2s, doubling, capped at 30s. Respects `Retry-After` headers. Non-retryable: context overflow, non-retryable API errors.

---

## 6. Session / Conversation Management

### Data Model

SQLite via Drizzle ORM. Key tables: `session`, `message`, `part`, `permission`.

`MessageV2` distinguishes:
- `User` — parts: text, file, agent references
- `Assistant` — parts: text, reasoning, tool, step, subtask
- `ToolPart` — state machine: `pending → running → completed | error`

### The Prompt Loop

`SessionPrompt.prompt()` flow:
1. Assert session not busy
2. Create/persist user message
3. Enqueue via `Runner.make()` (single-threaded per session)
4. **Core loop**:
   a. Build system context (env, instructions, config, LSP diagnostics, MCP resources)
   b. Assemble message history via `MessageV2.toModelMessages()`
   c. Resolve tools for agent + model
   d. Call `LLM.stream()` via AI SDK
   e. `SessionProcessor.process()` handles each stream event
   f. Check result: `"continue"` (loop), `"stop"`, or `"compact"` (context overflow)
5. After loop: generate title, summary, compact if needed

### Context Compaction

When tokens approach limit:
1. **Prune**: Remove old tool call/result pairs while protecting recent ones (min 20k freed, 40k protected)
2. If insufficient: run `compaction` agent to summarize history
3. Compacted summary replaces older messages

### Session Forking

`session.fork()` creates child sharing parent's history up to fork point. Preserves file system snapshot via `Snapshot` service for revert.

---

## 7. Configuration

### Config Hierarchy (highest → lowest priority)

1. **Managed** (`/etc/opencode/`) — enterprise/admin
2. **Global user** (`~/.config/opencode/opencode.json`)
3. **Project** (`.opencode/opencode.json` — traversed upward)
4. **Local project** (`.opencode/opencode.local.json` — gitignored)

Array fields (`plugin`, `instructions`) concatenated across levels.

### Key Config Schema

- `provider` — provider options and API keys
- `model` — default model as `"providerID/modelID"`
- `agent` — agent overrides and custom agents
- `mcp` — MCP server configs
- `lsp` — LSP server configs
- `keybinds` — keybind overrides
- `instructions` — additional prompt files
- `permission` — global permission ruleset
- `experimental` — feature flags

---

## 8. LSP Integration

Manages multiple language server processes per detected language.

- `LSPClient` — JSON-RPC via `vscode-jsonrpc`
- Auto-detected: TypeScript (tsserver), Python (pylsp), Go (gopls), Rust (rust-analyzer)
- Capabilities: diagnostics, go-to-definition, hover, workspace symbols, document symbols
- 150ms debounce on diagnostics
- File watching via `@parcel/watcher`

---

## 9. Database and Storage

### Two Parallel Systems

**SQLite (Primary)**: Drizzle ORM + Bun native SQLite. Path: `~/.local/share/opencode/opencode.db`
```sql
PRAGMA journal_mode = WAL
PRAGMA synchronous = NORMAL
PRAGMA busy_timeout = 5000
PRAGMA cache_size = -64000
PRAGMA foreign_keys = ON
```

**JSON File Store (Secondary)**: Key-value at `~/.local/share/opencode/storage/` for large diffs, snapshots. Uses `TxReentrantLock` for concurrent-safe access.

---

## 10. MCP Integration

Full MCP support via `@modelcontextprotocol/sdk`.

Three transports: `stdio`, `sse`, `streamable-http`. OAuth support for HTTP-based servers.

MCP tools dynamically resolved and bridged as AI SDK `dynamicTool`. Handles `ToolListChanged` notifications.

---

## 11. Plugin System

### Server Plugins

npm packages implementing `Plugin` interface from `@opencode-ai/plugin`. Installed into `.opencode/node_modules/`.

Hooks: `chat.system.transform`, `chat.params`, `chat.headers`, `tool.definition`, per-tool execution hooks.

### TUI Plugins

TypeScript/TSX files adding: custom routes, commands, keybinds, UI slot replacements, dialog prompts.

Built-in feature plugins: LSP diagnostics sidebar, modified files sidebar, MCP status, todo list, rotating tips.

---

## 12. Concurrency Model

### Runner: Per-Session Serialized

`Runner<T>` ensures single LLM generation per session. Wraps computation in Effect `Scope` with `cancel`, `busy` flag, lifecycle callbacks.

### Effect Fibers

Heavy computation runs as Effect fibers. `Effect.forkScoped` auto-cancels on parent scope close. Tool calls sequential within a step (AI SDK enforces). `task` tool spawns independent child sessions in separate Runners for parallel subagent execution.

### Event Bus

`Bus` service uses Effect's `PubSub`. Per-instance channels. `GlobalBus` bridges to HTTP SSE stream.

---

## 13. Key Design Decisions

### Strengths

1. **Client-Server Separation**: Clean HTTP API + TUI client. TUI replaceable with web/desktop/CLI. Remote operation works natively.
2. **Effect-ts**: Structured concurrency, typed errors, DI, per-project isolation via `InstanceState/ScopedCache`.
3. **Plugin System**: Two-level (server + TUI), npm-based distribution, simple hook pattern.
4. **Provider Abstraction**: 20+ providers via Vercel AI SDK, model-aware tool selection, per-provider transforms.
5. **SSE Event Batching**: 16ms debounce prevents render flooding during streaming.

### Weaknesses

1. **No Reactive Server State**: TUI maintains client-side replica via SSE — briefly stale.
2. **SQLite Contention**: Single-file with WAL, `busy_timeout = 5000`. Heavy concurrent subagents could contend.
3. **Bun Lock-in**: Native SQLite, PTY, and bundling depend on Bun-specific APIs.
