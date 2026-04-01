# Codex Gap Analysis: Features altcode Needs

> Generated 2026-04-01 from deep analysis of openai/codex (Rust + TS)

## Executive Summary

Codex is a **production-grade agentic CLI** with ~50K LoC in Rust. Altcode is currently
a **walking skeleton** at ~5K LoC. The core architecture (provider → engine → tool → TUI)
is sound, but Codex reveals 8 high-priority gaps that will block production use.

**Priority order by impact:**

| Priority | Feature | Why it blocks production |
|----------|---------|------------------------|
| P0 | Tool-call agent loop | Engine can't dispatch tools — just streams text |
| P0 | Exec (non-interactive) mode | Can't run in CI/CD, scripts, or SDK embedding |
| P0 | Session resume (`--last`) | Users lose context between invocations |
| P1 | Multi-provider support | Locked to Anthropic only |
| P1 | MCP client | Can't connect to external tool servers |
| P1 | Sandbox / command isolation | Tools run with full user privileges |
| P2 | Apply-patch tool | Edit tool is fragile; unified diff is standard |
| P2 | Structured output (JSON schema) | Can't use in pipelines without structured responses |

---

## P0: Must Have (blocks basic agentic use)

### 1. Tool-Call Agent Loop

**What Codex has**: Full agent loop that:
1. Sends request with tool schemas
2. Receives tool_use blocks from model
3. Dispatches to tool registry
4. Sends tool results back to model
5. Repeats until model produces final text (no more tool_use)

**What altcode has**: Engine streams text only. Tool schemas are sent to the API,
but the response is never parsed for tool_use blocks. The `loop()` function does
one round-trip and exits.

**Design**:
```
internal/engine/engine.go — modify loop():

for {
    stream := provider.Stream(ctx, req)
    text, toolCalls := collectStream(stream)

    if len(toolCalls) == 0 {
        // Final response — emit Done
        break
    }

    // Dispatch tools
    results := tool.Dispatch(ctx, toolCalls)

    // Append assistant message (with tool_use blocks) + tool results
    messages = append(messages, assistantMsg, toolResultMsgs...)
}
```

**Files to change**: `engine/engine.go`, `provider/message.go` (add ContentPart types)

**Effort**: ~200 LoC | human: 3 days | AI: 30 min

---

### 2. Exec (Non-Interactive) Mode

**What Codex has**: `codex exec "prompt"` runs headless, outputs JSONL events or
final text to stdout. Used by SDKs, CI/CD, scripts.

**What altcode has**: Only TUI mode. Can't pipe, script, or embed.

**Design**:
```
cmd/altcode/main.go:
  altcode "prompt"          → exec mode (prompt as positional arg)
  altcode --json "prompt"   → JSONL event stream
  altcode                   → TUI mode (no args)

internal/exec/exec.go:
  func Run(cfg, prompt string, jsonMode bool) error
    - creates engine
    - runs single turn (or multi-turn if tool calls)
    - prints final text to stdout (or JSONL to stdout)
```

**Files**: new `internal/exec/exec.go`, modify `cmd/altcode/main.go`

**Effort**: ~150 LoC | human: 2 days | AI: 20 min

---

### 3. Session Resume

**What Codex has**: `codex resume --last` reloads last session from SQLite,
rebuilds message history, continues conversation.

**What altcode has**: SQLite store exists with session/message CRUD, but engine
doesn't use it. Each run starts fresh.

**Design**:
```
cmd/altcode/main.go:
  altcode --last            → resume most recent session
  altcode --session ID      → resume specific session

engine/engine.go:
  func (e *Engine) LoadSession(db *store.DB, sessionID string) error
    - loads messages from store
    - rebuilds e.messages slice
  func (e *Engine) SaveTurn(db *store.DB, sessionID string, ...) error
    - persists new messages after each turn
```

**Files**: modify `engine/engine.go`, `cmd/altcode/main.go`

**Effort**: ~100 LoC | human: 1 day | AI: 15 min

---

## P1: Important (blocks multi-model and extensibility)

### 4. Multi-Provider Support

**What Codex has**: OpenAI, Azure, LMStudio, Ollama, any OpenAI-compatible API.
Provider detection from model string or explicit `--provider` flag.

**What altcode has**: Anthropic only. Hardcoded in `engine.New()`.

**Design**:
```
internal/provider/openai.go     — OpenAI-compatible provider (covers OpenAI, local)
internal/provider/registry.go   — provider registry (name → factory function)

Config:
  provider:
    openai:
      apiKey: $OPENAI_API_KEY
    ollama:
      baseURL: http://localhost:11434

Model string format: "provider/model" (existing parseModel works)
```

**Files**: new `provider/openai.go`, new `provider/registry.go`, modify `engine/engine.go`

**Effort**: ~250 LoC | human: 3 days | AI: 30 min

---

### 5. MCP Client

**What Codex has**: Full MCP client that connects to external servers via stdio,
discovers tools, routes tool calls, handles elicitation requests.

**What altcode has**: MCP config struct exists but no implementation.

**Design**:
```
internal/mcp/client.go     — JSON-RPC 2.0 over stdio
internal/mcp/transport.go  — spawn process, manage stdin/stdout
internal/mcp/tools.go      — tool discovery + schema translation

Flow:
  1. On startup, spawn configured MCP servers
  2. Call tools/list to discover tools
  3. Register discovered tools in tool.Registry
  4. Route tool calls through MCP client
```

**Files**: new `internal/mcp/` package (3 files)

**Effort**: ~400 LoC | human: 1 week | AI: 45 min

---

### 6. Sandbox / Command Isolation

**What Codex has**: Platform-specific sandboxing (Landlock/bwrap on Linux,
Seatbelt on macOS, restricted tokens on Windows). Network proxy.
Output capping (8KB default).

**What altcode has**: No sandboxing. Bash tool executes anything as current user.

**Design** (pragmatic approach — don't try to match Codex's full sandbox):
```
internal/sandbox/sandbox.go:
  type Sandbox interface {
      Exec(ctx, cmd string, args []string) (stdout, stderr []byte, err error)
  }

  // Phase 1: Output capping + timeout (already have timeout)
  // Phase 2: Linux namespace isolation via bwrap (if available)
  // Phase 3: macOS seatbelt (if on macOS)

internal/sandbox/capped.go:
  - Wraps exec with output size limit (default 512KB)
  - Kills process if output exceeds cap
```

**Files**: new `internal/sandbox/` package

**Effort**: Phase 1: ~100 LoC | Phase 2+3: ~300 LoC each

---

## P2: Nice to Have (improves quality)

### 7. Apply-Patch Tool (Unified Diff)

**What Codex has**: `apply_patch` tool that takes a unified diff and applies it.
More robust than string replacement for multi-line edits.

**What altcode has**: `edit` tool with exact string match. Fails on ambiguous
matches and can't handle multi-file patches.

**Design**:
```
internal/tool/patch.go:
  - Accepts unified diff format
  - Uses go-diff library or exec `patch` command
  - Supports multi-file patches
  - Falls back gracefully on conflict
```

**Effort**: ~150 LoC | human: 2 days | AI: 20 min

---

### 8. Structured Output (JSON Schema)

**What Codex has**: `--output-schema` flag that constrains model output to a
JSON schema. Critical for pipeline integration.

**What altcode has**: Nothing. Output is always free-form text.

**Design**:
```
provider/provider.go — add ResponseFormat to Request:
  type ResponseFormat struct {
      Type   string          `json:"type"` // "json_schema"
      Schema json.RawMessage `json:"schema,omitempty"`
  }

cmd/altcode/main.go:
  --output-schema schema.json
```

**Effort**: ~50 LoC

---

## Features We Do NOT Need to Copy

| Codex Feature | Why Skip |
|--------------|----------|
| Realtime audio/video | Altcode is a coding CLI, not a voice assistant |
| Windows sandbox (restricted tokens) | Linux/macOS-first, defer Windows |
| JS REPL tool | Go ecosystem, not Node |
| Guardian risk scoring | Over-engineered for v1; permission system is sufficient |
| Thread forking | Session resume is sufficient for v1 |
| OAuth credential management | Env vars + config are sufficient for v1 |
| Multi-agent spawn/send | Single-agent is sufficient for v1 |
| Cloud-managed requirements | Enterprise feature, not needed |

---

## Codex Patterns Worth Adopting (Architecture)

### 1. ContentPart message format
Codex (and Anthropic) use multi-part messages:
```json
{"role": "assistant", "content": [
  {"type": "text", "text": "I'll read the file"},
  {"type": "tool_use", "id": "1", "name": "read", "input": {"file_path": "..."}}
]}
```
Our `provider.Message` only has `Content string`. Must support `[]ContentPart`.

### 2. Turn diff tracking
Codex snapshots file state before each turn and computes a unified diff after.
This enables rollback and shows users exactly what changed. Cheap to implement
with `os.ReadFile` before/after.

### 3. Event processor abstraction
Codex separates event processing from event generation. The engine emits events;
different processors handle them (TUI renderer, JSONL writer, human formatter).
Our engine should emit to `<-chan event.Event` (already does) and consumers should
be pluggable.

### 4. ExecPolicy text matching
Codex's `prefix_rule(["git", "commit"])` is more composable than our glob matching.
Consider adding prefix-based matching to permission rules.

---

## Recommended Implementation Order

```
Phase A (make it agentic):
  1. ContentPart message format  — 1 hour
  2. Tool-call agent loop        — 30 min
  3. Session persistence in engine — 15 min

Phase B (make it scriptable):
  4. Exec mode (non-interactive) — 20 min
  5. --last session resume       — 15 min

Phase C (make it extensible):
  6. OpenAI-compatible provider  — 30 min
  7. MCP client (basic)          — 45 min

Phase D (make it safe):
  8. Output capping in bash tool — 10 min
  9. Apply-patch tool            — 20 min
```

Total estimated AI-assisted time: ~3.5 hours for all phases.
