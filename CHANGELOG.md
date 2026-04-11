# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] — CLI feature parity

Brings the headless `altcode "prompt"` surface closer to the TUI for users
who work out of vim/tmux/scripts. Design doc lives at
`docs/plans/cli-feature-parity-v7.md` and was reviewed across 7 rounds
between Claude Code and Codex before implementation.

### Added (Phase 10: inspection flags)

- `--print-config` dumps the cascaded config as JSON with **all
  credentials redacted** — Provider.APIKey, Provider.BaseURL
  (if embedded credentials), MCPServerConfig.Env values, and
  TeamModel.APIKey / TeamModel.BaseURL. Safe to pipe into bug
  reports or jq. Live config is never mutated.
- `--print-tools-list` prints every tool registered in a default
  engine with a one-line description.
- `--print-skills` prints discovered skills AND agents (matching
  TUI `/skills`), sorted alphabetically with path + description.
- `--print-mcp` lists configured MCP servers without starting
  them (avoids the 1-5s startup latency per server).
- `--doctor` runs an environment health check: provider creds,
  tool count, MCP count, git repo presence, CLI agents on PATH
  (claude/codex/opencode), and config cascade inventory.

### Added (Phase 5: input flags)

- `--image <path>` attaches an image to the prompt as a multimodal
  content block. Path `-` reads from stdin. **Anthropic provider
  only** (`--model anthropic/...`); OpenAI/Chinese provider
  multimodal is future work. Non-Anthropic + `--image` fails fast
  with an EX_USAGE error. Max 20 MB per image, auto-detected MIME.
- `--file <path>` injects file contents as fenced-code context at
  the top of the prompt. Max 1 MB per file to keep the context
  window sane. Wrapper fence auto-extends to survive files that
  contain triple-backticks.
- `--prompt-file <path>` reads the prompt body from a file. `-`
  means stdin. Mutually exclusive with a positional prompt arg.
- `--system <text>` and `--system-file <path>` append to the
  system prompt. Both use distinct synthetic paths so any future
  dedupe-by-path cascade logic won't silently drop one.
- New `engine.EngineParams.PendingInputParts` gets consumed on the
  first `Run()` call and merged into the first user message
  alongside the text prompt.
- `provider.NewImagePartFromFile` + `NewImagePartFromBytes` helpers
  (Anthropic shape; OpenAI multimodal translator is future work).
- Image-only runs (no text prompt or --file) default the text
  part to "Describe what you see in the attached image(s)." so
  the model has a cue to respond to.
- Windows `--prompt-file`/`--file`/`--system-file` inputs have
  trailing `\r\n` trimmed (not just `\n`).

### Added (Phase 4: session / history)

- `--continue` resumes the most recent session (CC-compat alias
  for `--last`).
- `--fork-session <id>` branches an existing session into a new
  one without mutating the source. Useful for experimenting from
  a checkpoint. Runs in a single SQLite transaction so a crash or
  cancel can't leave half-forked state.
- `--session-db <path>` overrides the SQLite file used for
  session storage (default: XDG/platform data dir). Named
  `--session-db` (not `--session-dir`) because `store.Open` takes
  a file path, not a directory.
- `--list-sessions` root-flag shortcut prints sessions and exits
  (same as the existing `altcode sessions` subcommand, but
  accessible from any invocation).
- New `store.DB.ForkSession` helper does the message copy inside
  a `BEGIN/COMMIT` — 10k-message forks go from seconds to tens
  of ms because we no longer fsync per row.
- `altcode sessions` subcommand now honors `--session-db` so the
  subcommand and root-flag shortcut read from the same database.

### Added (Phase 2: permission / mode)

- `--permission-mode plan|auto|default|bypass` picks the permission
  evaluator mode. Named `--permission-mode` (not `--mode`) to avoid
  collision with the existing `workflow --mode` subcommand flag.
- `--allow-tool name[:pattern]` adds session-scoped allow rules
  (repeatable). Patterns split on the FIRST colon only, so
  `bash:echo hi:bye` yields name=bash, pattern=`echo hi:bye`.
- `--deny-tool name[:pattern]` adds session-scoped deny rules.
  Deny beats allow within the same tier.
- `--dry-run` aliases to `--permission-mode plan` (read tools still
  run so the agent can reason; writes are denied).
- Both headless and TUI paths honor the new flags — `altcode
  --dry-run` without a prompt starts the interactive session with
  plan mode enabled.
- `--permission-mode bypass + --deny-tool` is rejected at flag
  parse (bypass allows everything, deny would be silently dropped).

Known limitation: config-level deny rules shadow CLI
`--allow-tool` because `permission.Check` iterates all denies
before any allows. Plan/auto built-in denies also shadow session
allows (plan mode short-circuits writes before rule iteration).
Documented in `exec.ApplyPermissionOverrides` with regression
tests pinning the current behavior.

### Added (Phase 1: output format + observability)

- `--output-format text|json|stream-json|diff` picks the stdout shape.
  - `text` is the current human-readable default.
  - `json` emits a single final JSON object with text, tool calls,
    permissions, cost, and accumulated errors.
  - `stream-json` is JSONL (the legacy `--json` behavior, kept as an alias).
  - `diff` runs the turn and prints `git diff HEAD` for files touched by
    `write`/`edit`/`apply_patch` tool calls.
- `--verbose` surfaces tool args and thinking blocks in text mode.
- `--quiet` suppresses the banner, tool chatter, and trailing newline.
- `--print-cost` writes a cost + timing summary to stderr at end of run
  (works outside a TTY, unlike the existing banner).
- `--print-tools` forces tool-call chatter to stderr even when piping.
- `--show-system` dumps the assembled instruction set (CLAUDE.md cascade)
  to stderr at start for debugging.
- `--print-tree`, `--save-transcript`, `--save-cost`, `--save-diff`
  registered as placeholders; full implementation lands in later phases.
- `runExec` now takes `exec.Params` directly and translates typed
  `*exec.UsageError` returns into `os.Exit(64)` after deferred MCP
  cleanup runs. No `os.Exit` inside `runExec` — that would leak
  subprocess-backed MCP servers.
- Signal handling now traps `SIGTERM` alongside `SIGINT` in exec mode so
  container/batch runners get clean shutdown.

### Fixed

- `drainJSON` no longer deadlocks the engine if stdout breaks (EPIPE)
  before a permission request arrives: the auto-deny response now
  sends independently of encoder state.
- `drainJSONFinal` accumulates every `ErrorEvent` instead of keeping
  only the last one, and reconciles tool `Input` from `ToolResultEvent`
  (engine re-sends the full input there).
- `drainDiff` surfaces git failures as a `UsageError` instead of
  silently writing to stderr, so scripts can tell "diff requested but
  unavailable" apart from "diff produced nothing".
- `apply_patch` tool inputs are now parsed for `+++ b/<path>` headers
  so `--output-format diff` picks up multi-file patches.

## [v0.7.0] — 2026-04-07

The last release before the workspace-driven orchestration pivot. Everything below works with the internal engine + external CLI backends.

### Added

#### Team Orchestration (new)
- **External CLI agent backends** — spawn codex/claude/opencode as child processes with typed streaming events (`internal/agent/external.go`)
- **Workflow definitions** — YAML frontmatter files in `.altcode/workflows/` with phased execution, dependency DAG, parallel agents (`internal/wfdef/`)
- **Phase engine** — topo-sorted phase execution with context injection, override control, failure policies (`internal/orchestra/`)
- **Claude stream-json parser** — parses `--output-format stream-json` for typed events (text, thinking, tool_use, tool_result)
- **Claude control_request auto-approval** — responds to permission prompts via stdin (multica pattern)
- **Split-pane team TUI** — each agent gets a bordered pane with role badge, status icon, live output scrolling (`internal/tui/team_view.go`)
- **Workflow header breadcrumb** — `[design ✓] → [implement ⟳] → [review ·]` phase progress display
- **`/workflow <name> <task>`** — discovers and runs workflow definitions from `.altcode/workflows/`
- **`/team run <task>`** — auto-detects available CLIs and runs them in parallel with split panes
- **3 default workflows** — `ship-feature` (design→implement→review), `review` (parallel), `fix` (diagnose→fix→verify)
- **Override keys** — Ctrl+P pause, Ctrl+Q abort during workflow execution
- **Provider-aware context injection** — writes CLAUDE.md for claude, AGENTS.md for codex/opencode

#### Engine Improvements
- **Intent router** — classifies prompts into TaskClass (QA/fix/refactor/feature/architecture) with risk levels
- **Verification ladder** — build→vet post-edit verification replacing simple goAutoVerify
- **Turn summary** — emits `[feature implementation | risk:medium] 3 file(s) changed` at turn end
- **Compaction thrash detection** — stops after 3 consecutive compactions (matches Claude Code)
- **Compaction audit logging** — logs method, messages before/after, tokens before/after
- **Tool result truncation** — caps at 30KB to prevent context bloat
- **Non-blocking trySend** — prevents goroutine blockage when TUI is slow

#### TUI Improvements
- **Slash command tab completion** — type `/co` + Tab → `/compact`
- **`/search <query>`** — find text across all messages
- **Thinking block display** — collapsed 💭 messages when thinking ends
- **Tool error state** — ✗ icon for failed tools (was defined but never triggered)
- **Adaptive HUD collapse** — drops segments on narrow terminals (<60 cols)
- **Accurate token tracking** — uses API-reported tokens instead of crude estimate

### Fixed
- Context leak in SpawnExternal (cancel never called)
- Stderr silently dropped from external agents
- Parallel context injection race (concurrent file appends)
- `{{.PhaseOutput "design"}}` template key mismatch
- OpPause consumed but no blocking effect
- busy flag stuck after workflow completes
- waitForWfEvent cmd chain never started
- Double-verify bug in appendToolResults (called twice for Anthropic)

### Infrastructure
- 33 packages passing with `-race`
- 6 reference submodules (claude-code, codex, multica, cline-kanban, agent-orchestrator, openhands)
- Business/pricing strategy doc (`docs/business/pricing-and-direction.md`)
- Technical design spec (`docs/superpowers/specs/2026-04-07-team-orchestration-design.md`)

## [Unreleased]

### Added
- **`/skills` command** — list every discovered skill with its description so you can see exactly what's wired into the session. Skills installed under `.claude/skills/`, `~/.claude/skills/`, `.agents/skills/`, or contributed by plugins all show up.
- **`/mcp` command** — list configured MCP servers, transport (stdio/sse), and a count of registered MCP tools so you can confirm a server actually loaded after it connected.
- **`/plugins` command** — show plugin warnings and search paths. Helps debug a plugin that loaded silently or that you expected to contribute commands.
- **Claude Code marketplace plugin support** — plugin manifests can now use the array form (`"commands": ["./commands/setup.md", ...]`) used by `claude-hud` and other marketplace plugins, alongside altcode's directory-string form. Discovery walks one level deeper into directories that don't have a manifest themselves so `~/.claude/plugins/cache/<owner>/<repo>/` plugins load correctly.
- **Plugin commands and agents actually install now** — `Plugin.Merge` previously only propagated hooks; commands and subagents loaded from a plugin manifest were silently dropped. They now fold into `/skills` and `/agents` alongside the filesystem cascade. After this change the local skill count went from 73 to 117 with no new files installed.
- **MCP `initialize` handshake** — altcode now performs the JSON-RPC `initialize` + `notifications/initialized` handshake on connect. Spec-compliant MCP servers (most `modelcontextprotocol/server-*` impls) used to refuse `tools/list` with `-32002 "Server not initialized"` and silently registered zero tools. Lenient on `-32601 "method not found"` so minimal servers still work.
- **Hermes-agent vendor reference** — `vendor/hermes-agent` submodule for cross-comparison.

### Fixed
- **Misleading "timeout 2m" on every running tool** — the tool tree used to render `Running… (Ns · timeout 2m)` on every live tool line, which made long reviews look like timeouts were firing constantly. Running tools now show only elapsed time.
- **Long agent turns killed mid-stream by a 5-minute HTTP cap** — both OpenAI and Anthropic providers had `http.Client{Timeout: 5 * time.Minute}`. In Go that covers the entire request lifetime including streaming body reads, so any turn longer than five minutes (extended thinking + multi-step tool use + slow providers) got cut off with a confusing timeout error. Replaced with a `Transport` that has granular dial/TLS/header/idle timeouts and no stream-length cap; user cancel still flows through the request context.
- **Silent bash tool timeouts** — when a Bash command was killed by its own timeout, the agent saw only `exit code -1` with partial stdout. It now gets `[bash: command killed after <d> — pass a larger timeout (ms)]`, so retries can widen the window explicitly instead of guessing.
- **`web_fetch` SSRF DNS rebinding** — the SSRF guard resolved the host once and then handed the URL to `http.Client`, which re-resolved through its own dialer. A short-TTL DNS rebinding attacker could return a public IP on the first lookup and `169.254.169.254` / `10.0.0.1` on the second, bypassing the guard. The transport now re-validates the resolved IP inside `DialContext` and pins it into the dial target.
- **Slash command shell injection via `$ARGUMENTS`** — `$ARGUMENTS` was substituted as raw text *before* `` !`...` `` backtick expansion ran `sh -c`. A skill body like `` !`grep "$ARGUMENTS" file` `` invoked with args `abc"; rm -rf ~; #` would happily execute the deletion. `$ARGUMENTS` is now single-quote-escaped before backtick exec.
- **Plugin manifest path traversal in `hooks` field** — `loadHooks` joined the manifest-supplied path without checking it stayed inside the plugin directory. A malicious plugin could set `"hooks": "../../../etc/passwd"` and we'd happily try to read it. Now goes through `safeJoin` like commands and agents already did.
- **Memory store concurrent-write corruption** — `Store.Save` and the `MEMORY.md` index rewrite had no mutex and used `os.WriteFile` directly, so two sessions saving memories at once could truncate one write or leave a stale index. Now serialized through `sync.Mutex` and written via temp-file + `os.Rename` so a crash mid-write never leaves a partial file.
- **Compaction split assistant `tool_use` from its `tool_result`** — the cutoff index landed on an arbitrary point in the message list. When it fell between an assistant message that emitted a `tool_use` block and the matching `tool_result`, providers rejected the next request with "tool_use ids not found in prior message", corrupting the session on every compact. Cutoff is now walked backward until it doesn't split a tool call from its result.
- **Compaction budget undercounted Anthropic tool results** — `BudgetCompactor` summed only flat `Content` on `role==tool` messages, but Anthropic puts tool result text in `Parts[].Content/Text`. The budget loop saw nothing on the Anthropic path and returned the bloated message list unchanged. Now counts and truncates Parts content too.
- **`UserPromptSubmit` hook never received the prompt text** — `fireUserPromptSubmit` passed only `Event` + `SessionID`, so command hooks reading the JSON payload saw an empty value and prompt hooks expanding `$USER_PROMPT` got `""`. Added a `UserPrompt` field to `hooks.Input`, populated at the call site, with `$USER_PROMPT` rebound to it. Adds `$TOOL_OUTPUT` as a separate placeholder for the original use.
- **MCP server subprocess leak on shutdown** — `connectMCP` used `context.Background()` and `Close` only closed stdin then waited forever. Servers that ignored EOF hung indefinitely and survived Ctrl+C. Now uses the signal-cancellable ctx, waits with a 5-second deadline, then SIGKILLs the entire process group.
- **MCP tool name collisions silently overwrote each other** — registration order depended on map iteration randomness, so the "winner" of a collision could change between restarts. Iteration is now sorted by server name and `Get` is checked before each `Register` to skip duplicates.
- **Subagent process group leak on timeout** — `internal/tool/agent.go` used `exec.CommandContext` with a 5-minute timeout but never configured a process group, so claude/codex/altcode subagents that forked their own helpers (LSPs, MCP servers) leaked them when the timeout fired.
- **Hook subprocess grandchildren leak on timeout** — same problem in `internal/hooks/exec.go`. Now sets `Setpgid` and SIGKILLs the whole group via `cmd.Cancel`, build-tagged for unix/windows.
- **HTTP hook silently allowed on network errors and non-2xx** — security-gate webhooks would `Decision: "allow"` on connection refused, 5xx, or malformed JSON, bypassing the gate on a network blip. Now fails closed with a descriptive deny message for every error path. Empty 2xx body is still treated as `"allow"`.
- **`Team.WaitAll` goroutine leak on timeout** — agents marked `"timeout"` had their child engines kept running because `Team` never owned a cancel func. Now stores per-agent `context.CancelFunc` and invokes it on the timeout branch so the spawn drains promptly.
- **Plugin loader noisy warnings on Claude Code's metadata dirs** — `~/.claude/plugins/cache/`, `data/`, `marketplaces/` are not plugin roots; the loader treated them as plugins that failed parse. Now silently skips dirs without `plugin.json` and walks one level deeper looking for nested plugins.
- **Glob `**/*.go` always returned zero matches** — the matcher used `filepath.Match` against the basename only, but `filepath.Match` has no concept of `**`. Replaced with a small handler that splits on `**` and matches segments. Honors `ctx.Cancel` so a long walk can be interrupted.
- **`write`/`patch`/`web_search` silent error swallow** — failure paths constructed `Result` with an `Output: "Error..."` string but no `Error` field, so the dispatcher saw success and the agent assumed the file was written or patch applied. Now sets `Result.Error` like every other tool.
- **Palette descriptions wrapped mid-word** — long skill descriptions broke at character boundaries inside the rounded border, producing "production-/grade" and "application/s". Truncates to the available content width and collapses embedded newlines from skill frontmatter.
- **`/wf-cancel` always claimed it cleared workflow state** — even when there was nothing to clear. Now reports the actual count.
- **`@file` completion lost trailing text** — typing `look at @ma<TAB> for the bug` dropped ` for the bug` because the splice used the `@` index alone. Now finds the end of the query and splices around the whole token.
- **Workspace `installPathWrappers` failed on Windows / stripped-env contexts** — used `os.Getenv("HOME")` which returns empty on Windows. Now uses `os.UserHomeDir()`.
- **Workflow ralph runner silently dropped state errors** — `SaveState` errors at every call site were thrown away, iteration errors weren't persisted, and the final iteration's output wasn't recorded. Now surfaces `SaveState` errors via the writer and persists iteration error + output.
- **Claude `control_request` numeric `request_id`** — claude-cli emits `request_id` as either a string or a number; the previous unmarshal targeted a string field so numeric ids became `""` and the response targeted the empty id. claude then waited forever for the real approval and the agent timed out with no explanation. Decoded through `json.RawMessage`, accepts both shapes.
- **`SpawnOptions.ForkTurns < 0` panic** — `make([]Message, negative)` panicked with `makeslice: len out of range`, killing the engine goroutine. Clamped at 0.
- **Tool registry data race** — `Register`/`Get`/`All`/`Schemas`/`Subset` now serialized with `sync.RWMutex`, with `All`/`Schemas` returning sorted output for deterministic system prompts.
- **Workflow header data race** — concurrent reads/writes from the bubbletea event loop and orchestra goroutines. Now `sync.RWMutex` protected.
- **Theme name lookup case-insensitive** — `Dracula` / `DRACULA` used to fall back to the default theme.
- **Sidebar accumulated empty paths** — tool results with a missing Title slipped through and produced blank entries.
- **Sidebar path truncation byte-split multi-byte chars** — CJK and emoji paths produced replacement glyphs.

