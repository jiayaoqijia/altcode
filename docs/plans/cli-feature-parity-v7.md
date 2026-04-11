# altcode CLI Feature Parity Spec (v7)

## Goal
Make `altcode` CLI cover ~90% of altcode/CC/Codex TUI features so a user
who lives in vim/tmux/scripts gets the full agentic coding experience
without an interactive PTY.

## What changed from v6
v6 got **PASS** from CC and **ITERATE** from Codex over one real bug:
`os.Exit(64)` inside `runExec` skips deferred `mcpCleanup()`, leaking
subprocess-backed MCP servers. Plus a CC nit: `payload []byte` needs
a `json.RawMessage(payload)` cast. v7 fixes:

1. **No `os.Exit` inside `runExec`** — return a typed error
   (`ErrPromptToolNotFound` or equivalent `usageError` wrapper) and
   let the top-level command print + exit AFTER the deferred
   `mcpCleanup()` runs. Also makes the path testable.
2. **`json.RawMessage` cast** — `json.Marshal` returns `[]byte`,
   `Tool.Execute` takes `json.RawMessage`. Explicit cast in the
   drain wrapper code sample.

## What changed from v5 (preserved from v6)
v5 got **PASS** from CC and **ITERATE** from Codex with one residual
P1 and two micro-gaps. v6 fixed:

1. **MCP bootstrap failure path** (Codex P1): `connectMCPWithCtx` at
   `cmd/altcode/main.go:393` returns no error, and `mcp.Manager.RegisterAll`
   at `internal/mcp/manager.go:76` only accumulates errors internally.
   So "fail fast if `--permission-prompt-tool` can't start" is not
   enforced just by unconditional MCP startup. v6 requires an
   **explicit post-connect validation**: after `connectMCPWithCtx`,
   call `eng.Registry().Get(ep.PermissionPromptTool)` and fail-fast
   at exit code 64 (EX_USAGE) if the tool is not in the registry.
2. **Drain signature change** (CC micro-gap): `drainText` and
   `drainJSON` need `ctx context.Context` and `ep *exec.Params`
   threaded through — v5 showed the new case clause without
   spec'ing the signature change. Documented explicitly.
3. **`runExec` caller overclaim** (CC micro-gap): v5 claimed
   "workflow/workspace subcommands" call `runExec` — grep confirms
   only `run()` in main.go:277 calls it. Workflow has its own drain
   at `internal/workflow/runner.go:196`. The signature change is a
   **1-site edit**, not a multi-caller refactor. Downgrades risk.

## What changed from v4 (preserved from v5)
v4 closed the v3 findings but both reviewers caught 4 residual items.
v5 fixed:
1. **`mcp.Manager.CallTool` is fiction** (v3 repeated in v4) — v4
   invented a `mcpMgr *mcp.Manager` type that doesn't exist.
   v5 routes the permission-prompt-tool call through the existing
   `tool.Registry` which already holds MCP-backed tools as
   `mcp__server__tool` (see `internal/mcp/tools.go:132`). No new
   manager type needed.
2. **`runExec` signature gap** — the existing `runExec` takes
   `engine.EngineParams`, not `exec.Params`, so threading
   `PermissionPromptTool` through it is non-trivial. v5 spec'ifies
   the signature change explicitly.
3. **`CostBudget` subagent propagation** — `internal/agent/spawn.go:94`
   inherits only `TokenBudget`. Without matching propagation of
   `CostBudget`, subagents bypass the USD cap. v5 requires the
   sibling field.
4. **`event.BudgetExceeded` not handled in drain** — v4 adds the
   event type but `drainText`/`drainJSON` at `exec.go:131,162`
   never print anything for it, so budget-triggered exits are silent.
   v5 requires handler cases in both drain functions.
5. **Multi-content MCP response edge case** — `CallTool` returns
   `Content[0].Text` (the first text block). If the prompt tool
   emits multiple content blocks, blocks 2..N are silently dropped.
   Documented as "tool must return single text block".

## What changed from v3 (preserved from v4)
v3 passed most checks but two reviewers caught new P1s and one
overcautious deferral. v4 fixed:

1. **`mcp.Client.CallTool` contract mismatch** — v3 spec'd
   `result.Allow` but the real signature is `(string, error)`
   (`internal/mcp/tools.go:76`). v4 defines the string-as-JSON
   response format explicitly.
2. **MCP startup gated by prompt keywords** — `cmd/altcode/main.go:330`
   only starts MCP servers when `needsMCP(prompt)` matches. v4
   unconditionally starts them when `--permission-prompt-tool` is set.
3. **`--max-cost` partially undeferred** — v3 dropped it entirely.
   Post-turn enforcement is a 30-minute change using the existing
   `TokenBudget` primitive at `engine.go:58`, extended to USD.
   v4 keeps mid-turn USD deferred but ships post-turn.
4. **`--print-tree` subagent nesting** — v3 was silent. v4 picks
   flat rendering with `[role]` prefix for subagent tools.
5. **`--commit` dirty-tree snapshot point** — v3 was underspecified.
   v4 captures `git status --porcelain` at `exec.Run` entry.
6. **`--permission-mode bypass` + `--permission-prompt-tool`** is
   value-dependent, moved from Cobra-enforced to `Validate()`.
7. **Parallel SQLite writes** — v4 notes WAL mode + per-worker
   session IDs for the batch runner.
8. **`PermResponse.Persistent` ignored** — documented explicitly
   as a v3-vs-CC gap.

## What changed from v1 → v2 → v3
- **v2**: rewritten after CC + Codex first round flagged 4 P0s
  (`--mode` collision, `--resume` parser trap, headless permission
  deadlock, `exec.Params` shape)
- **v3**: rewritten after second round exposed several v2 claims as
  code-fiction (`sandbox.Config.ExtraRoots` doesn't exist, `exec.Params`
  was proposed to be replaced not extended, permission "ask channel"
  doesn't exist)
- **v4**: surgical edits on v3 after third round caught the MCP
  contract mismatch, keyword-gated MCP startup, and overly broad
  `--max-cost` deferral

### Dropped (infeasible without bigger projects)
- **`--add-dir`** — v2 spec'd it via `sandbox.Config.ExtraRoots`, but
  `internal/sandbox/sandbox.go:18-23` is only a bash-command filter
  (`policy`/`blockedCmds`/`allowedCmds`). Worse, the filesystem tools
  bypass sandbox entirely: `internal/tool/read.go:40`, `write.go:39`,
  `edit.go:41` call `os.ReadFile`/`os.WriteFile` directly. Real
  `--add-dir` requires a new `internal/fsguard/` layer wired into
  every fs tool — a separate project, not a CLI flag. **Deferred.**
- **`--rewind <n>`** — session store at `internal/store/message.go:47`
  is flat ordered messages with no turn checkpoints. Tool-use batching
  breaks any naive rewind. **Deferred to a schema migration.**
- **Mid-turn `--max-cost` enforcement** — engine records cost post-turn;
  no mid-stream abort hook. **Deferred.** Post-turn enforcement ships
  in v4 (see Phase 8).

### Fixed from v2
- **`--resume <id>` Cobra trap**: instead of inventing new flag names,
  keep existing `--last` and `--session <id>` (which already work
  correctly) and add `--continue` as a CC-compatibility alias for
  `--last`. No picker flag — users can run `altcode sessions` to
  list, then `altcode --session <id>`.
- **`exec.Params` rewrite**: v2 proposed dropping `EngineParams`,
  `Engine`, `Writer`, `Model`, `Auth` from the struct. That would
  have broken `cmd/altcode/main.go:316 runExec`. v3 **extends** the
  existing 8-field struct (see `internal/exec/exec.go:17-26`) rather
  than replacing it.
- **`--permission-prompt-tool` event flow**: v2 referenced a
  nonexistent "permission evaluator ask channel". The real mechanism
  is `event.PermissionRequest` carrying `*event.PermReq` with a
  `Response chan PermResponse` field (see `internal/event/event.go:64-68`
  and `engine.go:895`). v3 specifies the exec.go consumer explicitly.
- **`ModePlan`**: already exists at `permission.go:12`. v2 hedged
  "(new or derived)". Not new, just use it.
- **Value-dependent mutex rules**: `cobra.MarkFlagsMutuallyExclusive`
  only handles presence, not values. `--prompt-file -` vs `--image -`
  must be validated at runtime in `Params.Validate()`.
- **Hook recursion guard**: must be wired into `hooks/exec.go:14` where
  the child process is spawned via `sh -c` with inherited env.
  `ALTCODE_HOOK_DEPTH` needs explicit `cmd.Env = append(...)` plus a
  startup check in `cmd/altcode/main.go`.

---

## Current altcode CLI surface (preserved, every existing flag kept)
- `altcode "prompt"` — one-shot exec
- `altcode --json "prompt"` — JSONL event stream
- `altcode --last "prompt"` — resume most recent session
- `altcode --session <id> "prompt"` — resume by id
- `altcode --model <m>` — model override
- `altcode --debug` — events to stderr
- `altcode --theme <t>` — TUI theme
- `altcode --config <path>` — config override
- subcommands: `sessions`, `team`, `workflow`, `workspace`, `login`, `logout`

---

## Proposed CLI additions

### 1. Output / observability
- `--output-format text|json|stream-json|diff` — pick output shape
  - `text`: human-readable (default)
  - `json`: one final JSON object with full turn result
  - `stream-json`: JSONL events (alias for existing `--json`)
  - `diff`: only print final unified diffs of edited files
- `--print-cost` — final cost summary to stderr
- `--print-tools` — log each tool call to stderr as it happens
- `--print-tree` — end-of-run ASCII tool tree on stderr. **Flat
  rendering in v4**: subagent tool calls are rendered at the top
  level with a `[role]` prefix (e.g. `[explorer] grep "foo"`), not
  nested under a parent "Spawn" call. True nesting needs a `parent_id`
  or depth field on `event.ToolCall` which would be a wider change
  — deferred.
- `--quiet` — suppress everything except final answer
- `--verbose` — include tool args + thinking blocks
- `--show-system` — print system prompt to stderr at start

Implementation: `exec/format.go` consumes events from the existing
`eng.Run()` channel at `exec.go:49`. `--print-tree` needs an in-memory
event accumulator (list of `ToolStart`/`ToolResultEvent` pairs) rendered
via `exec/tree.go` at `event.Done`.

### 2. Permission / mode
- `--permission-mode plan|auto|default|bypass` — renamed from v1 `--mode`
  to avoid collision with `workflowCmd --mode` at `main.go:125`.
  - `plan` — `permission.ModePlan` (already exists at `permission.go:12`).
    Read-only tools allowed; write/bash/patch denied. Prints final
    assistant text as "plan" and exits.
  - `auto` — `permission.ModeAuto`. Allowlist-only; unmatched denied
    (see `permission.go:107-108`). **Not** "ask for destructive" —
    v1 got that wrong.
  - `default` — `permission.ModeDefault`. Unknown tools return
    `ActionAsk` (`permission.go:110`). In headless, `ActionAsk`
    without a prompt-tool fails fast (see below).
  - `bypass` — `permission.ModeBypass`. All tools auto-allowed with
    a loud stderr warning at startup.
- `--permission-prompt-tool <mcp-tool-name>` — the headless escape
  hatch. See "Permission event flow" below.
- `--allow-tool <name>[:<pattern>]` — repeatable. Appends an
  `ActionAllow` rule via existing `Evaluator.AddSessionRule` at
  `permission.go:124`.
- `--deny-tool <name>[:<pattern>]` — repeatable. `ActionDeny`.
- `--dry-run` — for write tools, logs `[DRY-RUN]` to stderr and
  skips execution. Read tools still run so the agent can reason.
- `--max-turns <n>` — hard cap on agent loop iterations. Engine
  must gain a `MaxTurns int` field on `EngineParams` and a new
  `event.BudgetExceeded` event type (not currently in `event.go:9-23`).
  Default 50.
- `--max-cost <usd>` — **post-turn enforcement only**. Extends the
  existing `TokenBudget` at `engine.go:58-95` with a `CostBudget`
  sibling. Between-turn check: if accumulated USD > limit, engine
  emits `BudgetExceeded` and stops before the next provider call.
  No mid-turn abort — that requires provider usage checkpoints and
  is deferred. Default unlimited.
  **Subagent propagation required**: `internal/agent/spawn.go:94-99`
  currently inherits only `TokenBudget`. `CostBudget` must be
  propagated alongside it or subagents bypass the USD cap. The
  change is mechanical — one extra field on `engine.EngineParams`
  and one extra line in `spawn.go` construction.

### 3. Session / history
- `--continue` — alias for existing `--last`. CC-compatible spelling.
- `--list-sessions` — alias for `altcode sessions` subcommand.
- `--session-dir <path>` — override `~/.altcode/sessions.db` location.
- `--fork-session <id>` — copy an existing session's messages into
  a new session row, then start a new run. Uses existing
  `store.DB.ListMessages` + `AddMessage` — no new SQL.

Not added: `--resume`, `--resume-pick`. The existing `--session <id>`
already works, and adding a second optional-arg flag creates the
Cobra parser trap that both reviews caught. For picking interactively,
run `altcode sessions` to list, then paste the id into `--session`.

### 4. Input
- `--image <path>` — repeatable. Attach image(s) for multimodal
  providers. Path `-` reads from stdin (mutex with other stdin consumers).
- `--file <path>` — repeatable. Inject file contents as a user message
  before the prompt.
- `--prompt-file <path>` — read prompt body from file. `-` = stdin.
- `--system <text>` — append to system prompt.
- `--system-file <path>` — read system-prompt additions from file.

Precedence when multiple prompt sources present: positional arg >
`--prompt-file` > piped stdin. Warned on stderr if more than one set.

### 5. Hook / extension
- `--hook <event>:<cmd>` — repeatable. Uses `:` separator (not `=`)
  to avoid ambiguity with commands containing `=`. Event names must
  match constants in `internal/hooks/hooks.go` (e.g. `PreToolUse`,
  `PostToolUse`, `UserPromptSubmit`, `Stop`, `SessionStart`,
  `SessionEnd`, `Notification`). Invalid names fail at flag parse.
- `--mcp <name>:<cmd>` — repeatable. Attach an MCP server for this
  run only, merged with `.mcp.json` entries.
- `--skill <name>` — preload a skill into the system prompt.

**Hook recursion guard**: `internal/hooks/exec.go:14` runs
`sh -c` with inherited env. Change to:
```go
cmd := exec.CommandContext(ctx, "sh", "-c", hook.Command)
depth, _ := strconv.Atoi(os.Getenv("ALTCODE_HOOK_DEPTH"))
cmd.Env = append(os.Environ(), fmt.Sprintf("ALTCODE_HOOK_DEPTH=%d", depth+1))
```
At `cmd/altcode/main.go` startup, read `ALTCODE_HOOK_DEPTH`; if
> 0, disable hook firing for this process (refuse to register any
hooks on the engine). Depth cap = 3.

This only needs to be wired into `runCommandHook` (`hooks/exec.go`) —
`runPromptHook` at `hooks/prompt.go:13` calls the LLM API via
`provider.Provider.Stream`, not a subprocess, so it cannot recurse
back into `altcode`.

### 6. Artifact
- `--save-transcript <path>` — full JSONL transcript to file
- `--save-cost <path>` — cost report as JSON
- `--save-diff <path>` — final unified diff
- `--commit` — run `git commit` at end with an auto-generated message.
  Refuses if working tree was dirty pre-run, `--dry-run`, or
  `--permission-mode plan` (validated in `Params.Validate()`).
  **Dirty-tree snapshot point**: `exec.Run` captures
  `git status --porcelain` output at function entry (before
  `eng.Run`) into `Params.preRunDirty []string`. After the run,
  the commit step asserts the pre-run snapshot is empty (unless
  `--commit-dirty`). `--porcelain` format includes untracked
  files by default, matching the check already used in
  `internal/tui/hud.go:389`.
- `--commit-dirty` — bypass the clean-tree requirement. Mixes
  human changes with agent changes in the commit. Loud stderr
  warning.

### 7. Workflow / batch
- `--run-workflow <name>` — run a named workflow def from
  `.altcode/workflows/<name>.yaml`. **Renamed from v1 `--workflow`**
  to avoid collision with the `workflow` subcommand.
- `--prompt-each <file>` — run prompt template against each line of
  file; each line substituted into `{{input}}`.
- `--parallel <n>` — run `--prompt-each` N lines at a time. Each
  worker gets its own session ID; they share the same
  `~/.altcode/sessions.db` handle. The store already runs in WAL
  mode, so concurrent writes are safe, but the batch runner
  serializes `SaveSession` calls through a per-run mutex to avoid
  lock contention under high `--parallel`.
- `--retry <n>` — retry a failed prompt up to N times.
- `--bail` — stop batch on first failure.

### 8. Inspection
- `--print-config` — effective config + exit
- `--print-tools-list` — registered tools + exit
- `--print-skills` — discovered skills + exit
- `--print-mcp` — configured MCP servers + exit
- `--doctor` — health check + exit

---

## Permission event flow (the real P0 fix)

### Today's behavior (verified in code)
1. Engine calls `e.checkPermission(t, tc)` at `engine.go:890` which
   returns `ActionAllow`/`ActionDeny`/`ActionAsk`.
2. On `ActionAsk`, engine calls `e.askPermission(ctx, tc, out)` at
   `engine.go:895`. This emits `event.PermissionRequest` carrying
   a `*event.PermReq` with a `Response chan PermResponse` field
   (`event.go:64-68`), then blocks on `<-respCh`.
3. The block is **already ctx-cancellable** (`engine.go:919`). So
   "deadlock forever" in v2 was the wrong framing — Ctrl-C unblocks.
4. **The real bug**: `internal/exec/exec.go:131-159` (`drainText`
   /`drainJSON`) has no `case event.PermissionRequest`. The event
   goes into the channel unread; the engine blocks waiting for
   `respCh`; user waits indefinitely until signal.

### v4 fix
**Contract correction**: `mcp.Client.CallTool` returns `(string, error)`,
not a structured result (verified `internal/mcp/tools.go:76`). The
spec's v3 `result.Allow` was fiction. v4 expects the MCP prompt tool
to return a JSON string which we parse:

```json
{"allow": true}           // minimal allow
{"allow": false, "reason": "not in allowlist"}  // minimal deny
```

Any parse error is treated as a deny with a diagnostic.

**Unconditional MCP startup**: `cmd/altcode/main.go:330` (exact line
verified) starts MCP servers only when `needsMCP(prompt)` matches
keywords like `"mcp__"`, `"playwright"`, etc. This means
`--permission-prompt-tool` would fail when the user's prompt doesn't
happen to mention MCP.

v5 changes the gating: if `--permission-prompt-tool` is set, MCP
servers are started unconditionally regardless of prompt content.
The fix is in `runExec` after the new signature change above:

```go
if ep.PermissionPromptTool != "" || needsMCP(ep.Prompt) {
    mcpCleanup = connectMCPWithCtx(ctx, ep.EngineParams.Config, eng)
}
```

Edge case: misconfigured MCP server now fails exec mode even for
prompts that don't use MCP. This is intentional — if the user
asked for `--permission-prompt-tool` and we can't start it, we must
fail fast rather than silently auto-deny everything.

**Post-connect validation (v7 refined)**: `connectMCPWithCtx` at
`main.go:393` has no error return (verified), and `mcp.Manager.RegisterAll`
at `manager.go:83` accumulates errors internally without surfacing.
So unconditional MCP startup alone doesn't guarantee fail-fast.
After MCP connection, `runExec` must explicitly validate that the
requested prompt tool is in the registry — but it **must not call
`os.Exit`** inside `runExec` because that skips the deferred
`mcpCleanup()` and leaks subprocess-backed MCP servers.

v7 fix: return a typed error. `runExec` signals the failure, the
deferred cleanup runs, and the top-level command (`run()`) translates
the error into `os.Exit(64)` on the way out.

```go
// exec/errors.go
type UsageError struct {
    Msg      string
    ExitCode int
}
func (e *UsageError) Error() string { return e.Msg }

// main.go runExec — validation happens after MCP startup, and
// the deferred cleanup runs on any return path
func runExec(ep exec.Params) (err error) {
    eng, cleanup, err := buildEngineAndMCP(ctx, ep)
    if err != nil {
        return err
    }
    defer cleanup() // runs on every return path, including the
                    // typed-error return below

    if ep.PermissionPromptTool != "" {
        if _, ok := eng.Registry().Get(ep.PermissionPromptTool); !ok {
            return &exec.UsageError{
                Msg: fmt.Sprintf(
                    "--permission-prompt-tool %q not registered "+
                    "(check MCP server config; run `altcode --print-mcp`)",
                    ep.PermissionPromptTool),
                ExitCode: 64, // EX_USAGE
            }
        }
    }
    return exec.Run(ctx, ep, eng)
}

// Top-level cobra command maps UsageError to the process exit code
// AFTER runExec has returned and all defers have run:
func (root *cobra.Command) RunE(...) error {
    err := runExec(ep)
    if uerr := new(exec.UsageError); errors.As(err, &uerr) {
        fmt.Fprintln(os.Stderr, "altcode:", uerr.Msg)
        os.Exit(uerr.ExitCode)
    }
    return err
}
```

This shape is also testable: `runExec` becomes a pure function
returning an error, and the exit-code translation is one line
in the cobra wrapper.

**Routing through the existing `tool.Registry`**: no new `mcp.Manager`
type. MCP tools are registered into the engine's `tool.Registry` at
`internal/mcp/tools.go:113-130` as `mcp__<server>__<tool>` entries.
The existing registry already implements `tool.Tool` interface, so
the drain handler can call `registry.Get(promptTool).Execute(ctx, payload)`
directly. No refactor of `mcp.Client`, no new manager abstraction.

**The drain wrapper** in `internal/exec/permission.go`:

```go
// promptToolResponse is the JSON shape we expect back from a
// --permission-prompt-tool call. Matches the minimal CC schema.
// The prompt tool MUST return a single text content block; if it
// emits multiple blocks, only the first is parsed (mcp.Client.CallTool
// at internal/mcp/tools.go:86-95 returns Content[0].Text only).
type promptToolResponse struct {
    Allow  bool   `json:"allow"`
    Reason string `json:"reason,omitempty"`
}

func handlePermissionRequest(
    ctx context.Context,
    ev event.Event,
    promptTool string,
    registry *tool.Registry,
) {
    if promptTool == "" {
        // Fail-fast: headless default mode without prompt tool = deny.
        select {
        case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
        case <-ctx.Done():
        }
        fmt.Fprintf(os.Stderr,
            "altcode: permission request for %s denied "+
            "(headless mode requires --permission-prompt-tool)\n",
            ev.Permission.ToolName)
        return
    }
    // Look up the MCP-backed tool in the registry. Validated at
    // flag parse to start with "mcp__" prefix, so we know where to
    // route; any registry miss is a config error, not a user error.
    t, ok := registry.Get(promptTool)
    if !ok {
        select {
        case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
        case <-ctx.Done():
        }
        fmt.Fprintf(os.Stderr,
            "altcode: --permission-prompt-tool %q not found in registry\n",
            promptTool)
        return
    }
    go func() {
        // Bound by our own timeout, not just engine ctx, so an
        // unresponsive prompt tool doesn't wedge the whole run.
        callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()

        payload, _ := json.Marshal(map[string]any{
            "tool_name": ev.Permission.ToolName,
            "pattern":   ev.Permission.Pattern,
        })
        // Tool.Execute wants json.RawMessage, not []byte. Explicit
        // cast avoids compile friction at the call site.
        result, err := t.Execute(callCtx, json.RawMessage(payload))
        action := event.Deny
        if err == nil && result != nil && result.Error == nil {
            var resp promptToolResponse
            if jerr := json.Unmarshal([]byte(result.Output), &resp); jerr == nil && resp.Allow {
                action = event.Allow
            }
        }
        select {
        case ev.Permission.Response <- event.PermResponse{Action: action}:
        case <-ctx.Done():
        }
    }()
}
```

**Drain signature change**: current `drainText(ch, w)` and
`drainJSON(ch, w)` at `exec.go:131,162` must accept `ctx` and `*Params`
too, so the permission handler can route through the registry:

```go
func drainText(ctx context.Context, ch <-chan event.Event, w io.Writer, ep *Params) error
func drainJSON(ctx context.Context, ch <-chan event.Event, w io.Writer, ep *Params) error
```

Both drain functions gain new cases:
```go
case event.PermissionRequest:
    handlePermissionRequest(ctx, ev, ep.PermissionPromptTool, ep.Registry)
case event.BudgetExceeded:
    fmt.Fprintf(os.Stderr,
        "altcode: budget exceeded (%s) — stopping\n", ev.Info)
    // Engine will close the channel; loop exits naturally.
```

**`exec.Params` gains**:
```go
Registry *tool.Registry  // for --permission-prompt-tool routing
```
Populated by `runExec` from the engine's existing registry after
unconditional MCP startup (see below).

**`runExec` signature change**: v4 glossed over this. Current
signature at `cmd/altcode/main.go:316` takes
`runExec(params engine.EngineParams, prompt string, jsonMode bool)`.
The new fields live on `exec.Params`, not `engine.EngineParams`.
v6 changes the signature to:
```go
func runExec(ep exec.Params) error
```
**Only caller**: `run()` at `main.go:277` is the sole caller
(verified — workflow/workspace/team subcommands have their own
drain paths; `internal/workflow/runner.go:196` doesn't go through
`runExec`). The signature change is a 1-site edit, not a
multi-caller refactor.

### Race analysis
- `respCh` is buffered (cap 1), so the goroutine's send never blocks.
- Engine selects on `<-respCh` and `<-ctx.Done()`. If ctx cancels
  after the goroutine sends but before the engine reads, the engine
  may return either the response or `ctx.Err()` — nondeterministic
  but safe (cancel path returns an error anyway, so no tool runs).
- No deadlock on unresponsive prompt tool because the 30s `callCtx`
  timeout forces the goroutine to fall through to the deny branch.

### v4 vs Claude Code gaps
CC's `--permission-prompt-tool` also supports:
- Structured allow/deny with `updatedInput` (tool args can be rewritten)
- Persistent permission updates (rule added to session via
  `PermResponse.Persistent`, which altcode already defines at
  `event.go:60` but the minimal v4 responder ignores)
- Pre-run MCP tool validation

v4 ships a minimal `{"allow": bool, "reason"?: string}` responder.
`updatedInput` and `Persistent` are documented as "future work" — not
blockers for v4. If the minimal version works, we layer richer
semantics in v5.

---

## `exec.Params` extension (preserve current shape)

Current struct at `internal/exec/exec.go:17-26`:
```go
type Params struct {
    EngineParams engine.EngineParams
    Engine       *engine.Engine
    Prompt       string
    JSON         bool
    Quiet        bool
    Model        string
    Auth         string
    Writer       io.Writer
}
```

v3 **adds** fields without reshaping:
```go
type Params struct {
    // existing (preserved)
    EngineParams engine.EngineParams
    Engine       *engine.Engine
    Prompt       string
    JSON         bool
    Quiet        bool
    Model        string
    Auth         string
    Writer       io.Writer

    // new: output
    OutputFormat   string  // "", "text", "json", "stream-json", "diff"
    Verbose        bool
    PrintCost      bool
    PrintTools     bool
    PrintTree      bool
    ShowSystem     bool
    SaveTranscript string
    SaveCost       string
    SaveDiff       string

    // new: permission
    PermissionMode       string
    PermissionPromptTool string
    DryRun               bool
    // allow/deny rules + max-turns are pushed onto EngineParams
    // before exec.Run is called (see runExec in main.go)

    // new: input
    Images     []string
    Files      []string
    PromptFile string
    System     string
    SystemFile string

    // new: artifact
    Commit      bool
    CommitDirty bool

    // new: routing (populated by runExec after engine construction)
    Registry *tool.Registry // for --permission-prompt-tool lookup
    preRunDirty []string    // snapshot from git status --porcelain
}

// Validate enforces mutual-exclusion rules that Cobra can't express.
func (p *Params) Validate() error { /* ... */ }
```

**Cobra** handles flag-presence exclusions (e.g. `--quiet` vs `--verbose`).
**`Params.Validate()`** handles value-dependent rules (e.g. two stdin
consumers).

---

## Mutual-exclusion matrix

### Cobra-enforced (flag presence)
| A                          | B                          |
|----------------------------|----------------------------|
| `--quiet`                  | `--verbose`                |
| `--quiet`                  | `--show-system`            |
| `--continue`               | `--session`                |
| `--continue`               | `--fork-session`           |
| `--session`                | `--fork-session`           |
| `--commit`                 | `--dry-run`                |

### Runtime-validated in `Params.Validate()`
Value-dependent rules that Cobra's `MarkFlagsMutuallyExclusive` can't
express (it only checks flag presence, not values):
- `--permission-mode bypass` + `--permission-prompt-tool` → prompt
  tool is meaningless when bypass allows everything (value-dependent
  because `--permission-mode` is a string flag, not a bool)
- `--commit` + `--permission-mode plan` → plan produces no changes
- `--prompt-file -` + positional prompt arg → pick one
- `--prompt-file -` + `--image -` (path `-`) → single stdin consumer
- `--prompt-each` + `--session`/`--continue` → batch is always fresh
- `--max-cost` value <= 0 → either drop flag or use a positive USD cap
- `--max-turns` value <= 0 → same
- `--permission-prompt-tool` value must start with `mcp__<server>__`
  prefix (MCP tools are registered at `internal/mcp/tools.go:132`
  with that shape; unprefixed names won't route)

---

## Signal handling

Current exec mode at `cmd/altcode/main.go:316-317` traps `SIGINT` only.
v3 adds `SIGTERM` to the same `signal.Notify` call so containers and
batch runners get clean shutdown:

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
```

On signal: cancel the engine `ctx`, which unblocks `askPermission`'s
ctx.Done() path at `engine.go:919` and triggers existing engine
cleanup.

---

## What CANNOT be in CLI (unchanged)
- Live tool tree updates (CLI is line-oriented; `--print-tree` is end-of-run only)
- Visual HUD with real-time cursor positioning
- Mid-turn interactive cancel with visible feedback (Ctrl-C still works)
- Mid-turn model switch
- Image paste from clipboard (use `--image <path>`)
- Interactive REPL palette / vim mode (use `$EDITOR` + `--prompt-file`)
- Interactive permission prompt with y/n/always (use
  `--permission-prompt-tool` with a real MCP tool, or `--permission-mode bypass`)

---

## Phased implementation

| Phase | Scope                                                                                              | Day |
|-------|----------------------------------------------------------------------------------------------------|-----|
| 1     | Extend `exec.Params`, add `--output-format`, `--quiet`/`--verbose`, `--print-cost`/`--print-tools` | 1   |
| 2     | `--permission-mode` (rename), `--allow-tool`/`--deny-tool`, `--dry-run`                            | 1   |
| 3     | `--permission-prompt-tool` + exec drain handler + MCP round-trip                                   | 1.5 |
| 4     | `--continue`, `--list-sessions`, `--fork-session`, `--session-dir`                                 | 1   |
| 5     | Input: `--image`, `--file`, `--prompt-file`, `--system`/`--system-file`                            | 1   |
| 6     | Hooks: `--hook`, `--mcp`, `--skill`, recursion guard, `ALTCODE_HOOK_DEPTH` in `hooks/exec.go`     | 1   |
| 7     | Artifacts: `--save-*`, `--commit`, `--commit-dirty` with dirty-tree check                          | 1   |
| 8     | Engine: `MaxTurns` on `EngineParams`, `event.BudgetExceeded`, `--max-turns` + `--max-cost` post-turn wiring via new `CostBudget` | 0.8 |
| 9     | Workflow/batch: `--run-workflow`, `--prompt-each`, `--parallel`, `--retry`, `--bail`               | 1.5 |
| 10    | Inspection: `--print-config`/`--print-tools-list`/`--print-skills`/`--print-mcp`/`--doctor`        | 0.5 |
| 11    | Signal handling: add `SIGTERM` at `main.go:316`                                                    | 0.2 |
| 12    | `--print-tree` event accumulator + ASCII renderer                                                  | 0.8 |
| 13    | Tests: table-driven `Params.Validate()`, E2E smoke tests per flag                                  | 2   |
|       | **Total**                                                                                          | **~12 days** |

**Explicitly deferred to future work** (not in v4 scope):
- `--add-dir` — needs new `internal/fsguard/` layer across read/write/edit tools
- `--rewind` — needs turn checkpoints in session schema
- Mid-turn `--max-cost` abort — needs provider-side cost checkpoints
  (v4 ships post-turn only)
- Structured permission updates (`updatedInput`, persistent rules) —
  CC has them, v4 ships minimal `{"allow": bool}` responder
- True `--print-tree` nesting — needs `parent_id` on `event.ToolCall`

Revised from v2's 8d → v3's 12d → v4's ~12.3d (add `--max-cost`
post-turn + cost budget wiring, +0.3d). v4 is honest about:
- The permission-prompt-tool drain handler is real work (1.5d not 0.5d)
- The hook recursion guard needs source edits in `hooks/exec.go`
- `--print-tree` needs an event accumulator (flat only, subagent
  nesting deferred)
- Writing `Params.Validate()` tests is meaningful coverage
- MCP startup must be unconditional when prompt-tool set
- Cost budget is a sibling of TokenBudget, ~1 hour of engine work

---

## Open questions for v5
1. Should `--permission-prompt-tool` accept a shell command too, not
   just MCP tool names? (CC only takes MCP tools.)
2. Should `--prompt-each --parallel` share one session by default
   with serialized turns, or one session per worker? (Current spec:
   per worker.)
3. Should `--continue` be deprecated in favor of keeping `--last`
   as canonical? (Current spec: both work, `--continue` for CC muscle
   memory.)
4. Scope for the eventual `--add-dir`: filesystem confinement at the
   tool layer, or an in-process allowlist checked by a new
   `fsguard.Check(path, mode)` helper called from every fs tool?
5. Should the permission-prompt-tool response schema allow
   `updatedInput` rewrites (CC parity) or stay at minimal
   `{"allow": bool}` indefinitely? Trade-off: more power vs.
   more attack surface when a malicious MCP tool rewrites args.
6. For `--max-cost` post-turn: should the engine emit a warning
   at 80% budget used, or just fail closed at 100%? CC has no
   cost flag at all, Codex uses per-session USD cap.
