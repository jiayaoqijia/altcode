# Altcode Project Guide

## Project: altcode CLI

A Go CLI/TUI for AI-assisted coding. Architecture:

```
cmd/altcode/main.go         → Cobra CLI entry point
internal/engine/             → Agent loop (tool dispatch, session persistence, cost tracking)
internal/provider/           → Provider interface (Anthropic SSE + OpenAI SSE + 5 Chinese providers)
internal/workflow/           → Optional workflow mode (interview, plan, ralph)
internal/tool/               → Tool interface, registry, concurrent dispatch
internal/permission/         → Permission evaluator (4 modes, doom loop)
internal/hooks/              → Hook system (13 events, command + prompt hooks, conditional if)
internal/agent/              → Subagent definitions, spawn, registry, team orchestration
internal/mcp/                → MCP client (stdio + SSE, tools + resources)
internal/command/             → Slash commands (markdown + frontmatter)
internal/plugin/             → Plugin discovery, loading, marketplace
internal/memory/             → Persistent cross-session memory
internal/cost/               → Per-turn token/USD cost tracking
internal/history/            → File operation journaling with diffs
internal/auth/               → Auto-detect Claude Code + Codex CLI credentials
internal/store/              → SQLite sessions + messages
internal/config/             → JSONC config, env expansion, instruction cascade
internal/compact/            → Context compaction (budget + micro)
internal/exec/               → Headless execution mode
internal/tui/                → Bubbletea TUI (15 slash commands, thinking indicator)
internal/event/              → Event types (engine ↔ TUI)
internal/sysctl/             → System prompt assembly
```

### Build & Test
```bash
make build          # Build to dist/altcode (uses -mod=mod)
make test           # Run all tests with race detector
make lint           # Run go vet
```

Note: Makefile sets `GOFLAGS=-mod=mod` automatically because vendor/ contains git submodules (codex, claude-code), not Go dependencies.

### Benchmark & Testing Rule (HARD RULE)

**ALL benchmarks and feature tests MUST run through the TUI, not headless commands.**

- Launch altcode TUI in tmux, send prompts via `tmux send-keys`, capture via `tmux capture-pane`
- This tests the REAL user experience: thinking indicator, tool tree, HUD, streaming, sidebar
- Headless mode (`altcode "prompt"`) skips TUI rendering and misses rendering bugs
- For CC comparison: capture CC's HUD via `claude-hud` plugin + Playwright browser capture

```bash
# Correct: TUI benchmark via tmux
tmux new-session -d -s bench -x 160 -y 45 "/tmp/altcode --config config.json"
tmux send-keys -t bench "Write a Fibonacci function in Go" Enter
sleep 20
tmux capture-pane -t bench -p  # capture rendered TUI output

# Wrong: headless benchmark (misses TUI bugs)
altcode "Write a Fibonacci function in Go"  # skips TUI entirely
```

### AltFix Daemon (implemented)

Complete daemon in `internal/daemon/` — 23 source files, 230 tests.
Design spec: `docs/superpowers/specs/2026-04-12-altfix-daemon-design.md` (v5).
Implementation plans: `docs/superpowers/plans/2026-04-12-daemon-plan-a-foundation.md`.
Spike audit: `docs/superpowers/specs/2026-04-12-daemon-spike-audit.md`.
E2E test plan: `docs/superpowers/specs/2026-04-12-e2e-test-plan.md` (v2, 62 tests).

The daemon is fully isolated: zero imports from internal/tui, internal/exec,
or internal/engine. Agents are spawned as subprocesses via os/exec.
Only touch point: `cmd/altcode/daemon.go` + 1 AddCommand line in main.go.

Key files: store.go (SQLite), subprocess.go (process groups), orchestrator.go
(phase loop), server.go (HTTP + auth), handlers.go (REST endpoints),
sse.go (streaming), github.go (PR lifecycle), webhooks.go (triggers),
budget.go (stall detection), modes.go (Solo/Pair/Team routing).

**Running the daemon:**
```bash
altcode daemon --port 9200 --auth-token $TOKEN --data-dir ~/.altcode/daemon
```

**Creating tasks:**
```bash
curl -X POST http://localhost:9200/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"https://github.com/you/repo","task":"fix tests","model":"altllm-basic"}'
```

**Model routing:** Models auto-detect provider by prefix (`altllm-basic` → altllm,
`deepseek-v3` → deepseek). No `provider/` slash needed for known providers.

**E2E verified pipeline (4 roles, real backends):**
- Lead (altcode + altllm-basic) — architecture planning
- Implementer (codex + gpt-5.4) — code generation
- Reviewer (claude code) — code review
- Tester (codex + gpt-5.4) — test generation

**Edge cases handled:** whitespace-only fields → 400, duplicate delivery_id → 409,
stop/steer nonexistent task → 404, stop/steer completed task → 409,
BudgetController thread-safe (sync.Mutex), subprocess stdout buffered (no pipe race).

### CLI Feature Parity (implemented)

Spec: `docs/plans/cli-feature-parity-v7.md` — 13 phases bringing the
headless `altcode "prompt"` surface to ~90% of TUI features. Design
was reviewed across 7 rounds between CC and Codex before starting.

**Landed:**
- **Phase 1**: `--output-format text|json|stream-json|diff`, `--verbose`,
  `--quiet`, `--print-cost`, `--print-tools`, `--show-system`, plus
  `--save-transcript/cost/diff` placeholders. `runExec` now takes
  `exec.Params` and translates typed `*exec.UsageError` to exit 64
  after deferred MCP cleanup runs (never `os.Exit` inside `runExec`).
- **Phase 2**: `--permission-mode`, `--allow-tool`, `--deny-tool`,
  `--dry-run`. Applied via `exec.ApplyPermissionOverrides` to the
  permission evaluator before the exec/TUI branch so both paths
  honor the flags. Config deny still shadows CLI allow — documented,
  not changed.
- **Phase 4**: `--continue` (alias), `--fork-session`, `--session-db`,
  `--list-sessions`. Fork copy runs in a single SQLite transaction
  via new `store.DB.ForkSession`. Named `--session-db` not
  `--session-dir` because `store.Open` takes a file path, not a
  directory (CC Phase 4 review caught this).
- **Phase 5**: `--image`, `--file`, `--prompt-file`, `--system`,
  `--system-file`. Image provider gate requires `anthropic/` prefix
  — non-Anthropic multimodal would silently drop via `toOpenAIMessages`
  (both reviewers caught this as a BLOCKER). `engine.EngineParams.PendingInputParts`
  carries the image `ContentPart`s into the first user message and is
  consumed once per engine. `--file` fence auto-extends to survive
  triple-backtick content.
- **Phase 10**: `--print-config`, `--print-tools-list`, `--print-skills`,
  `--print-mcp`, `--doctor`. All print-and-exit. `--print-config`
  deep-copies and redacts every credential-bearing field: Provider.APIKey,
  MCPServerConfig.Env, TeamModel.APIKey, and Provider/TeamModel BaseURLs
  that embed `user:pass@host` (both CC + Codex caught secret-leak
  BLOCKERs here).
- **Phase 8**: `--max-turns`, `--max-cost`. New `engine.CostBudget`
  (atomic int64 micro-cents, nil-safe, race-safe under parallel
  subagents). New `event.BudgetExceeded` type. Propagated through
  `agent/spawn.go` so subagent cost counts toward the parent
  budget. Each engine pushes its own per-turn USD delta so
  parent+child don't double-count. Mid-turn cost abort deferred
  (needs provider-side usage checkpoints).
- **Phase 7**: `--commit`, `--commit-dirty`, `--save-cost`,
  `--save-diff`. `--commit` does SCOPED staging (agent-touched
  paths only, not `git add -A`) based on delta between pre-run
  and post-run `git status --porcelain`. Fresh `context.Background`
  for the commit sub-commands so cancellation between `git add`
  and `git commit` can't wedge the index. `--save-transcript`
  errors out pointing to the stream-json workaround (needs Phase 12).
- **Phase 11**: `SIGTERM` folded into exec mode signal handling.

**Hard rule when extending `exec.Params`:**
1. Never replace existing fields — only extend. Later phases are
   additive struct fields + additive Cobra flag registration.
2. Never call `os.Exit` from inside `runExec`. Return a typed
   `*exec.UsageError{Msg, ExitCode}` and let the top-level cobra
   `RunE` wrapper translate it to exit code AFTER deferred cleanup.
   This is how MCP subprocess teardown stays correct on failure
   paths (spec v7 Codex round-3 finding).
3. Drain functions (`drainText`/`drainJSON`/`drainJSONFinal`/`drainDiff`)
   must ALWAYS respond to `PermissionRequest` events — even after
   an encode error on stdout. The auto-deny response path is
   independent from the stdout path; nesting it inside `encodeErr == nil`
   wedges the engine on a later permission-gated tool call.
4. Run the full review loop per phase: implement → build → test →
   CC review diff → Codex review diff → fix blockers → commit. Two
   reviewers catch different classes of bug (CC tends to find
   mechanical/API mismatches, Codex tends to find deadlock/leak paths).

### Pre-Push Gate (HARD RULE)

**NEVER push without passing these locally first:**

```bash
# 1. Clean model-generated files (benchmarks create junk)
rm -f internal/main.go internal/stringxor.go internal/reverse_test.go
rm -rf internal/lru internal/middleware internal/stack internal/ratelimit internal/datastructures stack/

# 2. Build
GOFLAGS=-mod=mod go build ./...

# 3. Vet (catches fmt.Println newline issues, unused vars, etc.)
GOFLAGS=-mod=mod go vet ./...

# 4. Test (at minimum the packages you changed)
GOFLAGS=-mod=mod go test ./... -race -count=1 -timeout=180s
```

**If ANY step fails, fix it before committing.** Do not push broken code
and hope CI catches it — CI runs on 3 platforms and failures block everyone.

### TUI Testing Rule (HARD RULE)

**Every TUI change must be tested at THREE levels: view tests, tmux E2E, and headless CLI.**
Unit tests only verify data flow. View tests catch rendering bugs. tmux E2E catches
real terminal interactions. All three are required for workspace/agent TUI changes.

#### Level 1: View Tests with `teatest` (REQUIRED for all TUI changes)

Use `charmbracelet/x/exp/teatest` for Bubbletea integration tests and direct
render testing. These run in CI with `-race` and catch layout, content, and
concurrency bugs.

```go
// teatest integration — tests the full Bubbletea app lifecycle
func TestTUIView_StartupRender(t *testing.T) {
    m := testApp()
    tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))
    time.Sleep(500 * time.Millisecond)
    tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
    out := readOutput(t, tm)
    // Assert rendered terminal output contains expected elements
}

// Direct render testing — tests individual components
func TestTUIView_WorkspaceViewRender(t *testing.T) {
    wv := NewWorkspaceView(sess)
    wv.SetSize(120, 25)
    wv.AppendAgentOutput("architect", "Reading codebase...")
    output := wv.Render(DefaultTheme)
    plain := stripANSI(output)
    // Assert: roles, agent output, branch, CI, PR, footer keys
}
```

**Required view test coverage for workspace changes:**
- Workspace pane render: roles, output lines, branch name, CI status, PR number
- Phase breadcrumb: ✓ done, ⟳ running, · pending icons
- Attention colors: green/yellow/orange/red produce different ANSI output
- Narrow terminal: no overflow at 60 cols
- Empty state: "No agents" fallback message
- Concurrent access: 200 writes + 200 renders under `-race`
- Help text: all slash commands + workspace mode keys present

See `internal/tui/tui_view_test.go` for the reference test suite (8 tests).

#### Level 2: tmux E2E Testing (AUTOMATED + REQUIRED for agent/workspace changes)

tmux creates a real PTY so Bubbletea renders properly. This catches issues that
view tests miss: actual agent spawning, output streaming, key binding conflicts.

**Automated suite:** `internal/tui/tmux_pty_test.go` runs the core probes
(startup, /help, /doctor, real PageUp/PageDown) in CI on every push — builds
altcode in `t.TempDir`, starts a detached tmux session, polls for the TUI
header, and asserts content. Skipped when `tmux` is absent or
`ALTCODE_TMUX_TEST=0` is set.

```bash
# Run the automated tmux suite locally:
GOFLAGS=-mod=mod go test ./internal/tui/ -race -count=1 -run TestTmuxPTY -v

# Manual ad-hoc probes for scenarios the automated suite doesn't cover:
GOFLAGS=-mod=mod go build -o /tmp/altcode-test ./cmd/altcode/
tmux new-session -d -s altcode-tui -x 120 -y 30 "/tmp/altcode-test"
sleep 3
tmux send-keys -t altcode-tui "/help" Enter
sleep 2
tmux capture-pane -t altcode-tui -p  # verify help renders

# Test workspace with real agents
tmux send-keys -t altcode-tui "/workspace create a hello function" Enter
sleep 15
tmux capture-pane -t altcode-tui -p  # verify agent panes + streaming

# Clean up
tmux kill-session -t altcode-tui
```

**What to verify in tmux captures:**
- Header: `[workspace:ID] task status [phase ✓] → [phase ⟳]`
- Agent panes: bordered boxes with ROLE, backend, activity, branch
- Agent output: scrolling text from codex/claude in panes
- Footer: `Ctrl+Z pause Ctrl+Q abort Ctrl+S send Tab cycle`
- HUD: model, git branch, timer, context bar
- No line overflow beyond terminal width
- Attention colors visible (green borders for working agents)

#### Level 3: Headless CLI Testing (REQUIRED for workspace commands)

Tests all workspace subcommands without a terminal:

```bash
timeout 5 /tmp/altcode-test workspace "test" --dry-run    # shows plan
timeout 3 /tmp/altcode-test workspace list --json          # valid JSON []
timeout 3 /tmp/altcode-test workspace status               # no crash
timeout 3 /tmp/altcode-test workspace resume               # helpful error
timeout 3 /tmp/altcode-test workspace spawn --help         # shows usage
timeout 3 /tmp/altcode-test workspace send --help          # shows usage
timeout 3 /tmp/altcode-test workspace rollback --help      # shows usage
timeout 3 /tmp/altcode-test workspace init --help          # shows usage
```

#### Pre-Push Checklist for TUI Changes

```bash
# 1. View tests pass
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1 -run TestTUIView -v

# 2. Full TUI test suite passes
GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1

# 3. tmux E2E (automated — TestTmuxPTY runs on every `go test ./internal/tui/`;
#    the block below is for ad-hoc manual probes beyond the 4 automated subtests)
tmux new-session -d -s test -x 120 -y 30 "/tmp/altcode-test"
tmux send-keys -t test "/workspace test task" Enter
sleep 10
tmux capture-pane -t test -p  # inspect output
tmux kill-session -t test

# 4. Headless CLI commands all work
timeout 5 /tmp/altcode-test workspace "test" --dry-run
```

**Workspace E2E test checklist (run before every workspace PR):**
1. `altcode workspace "task" --dry-run` — shows plan
2. `altcode workspace "task" --agents codex` — agents spawn, output streams
3. `altcode workspace list` — shows workspace
4. `altcode workspace status <id>` — shows per-agent status
5. tmux TUI test — `/workspace` shows dashboard with live agent output

If a TUI change passes unit tests but breaks visual rendering, the unit tests
are insufficient. Add a tmux capture to the PR description.

Common CI failures and how to avoid them:
- `found packages internal (bench_test.go) and main (main.go)` → model-generated `main.go` in internal/. Delete it.
- `fmt.Println arg list ends with redundant newline` → remove `\n` from Println.
- `undefined: SomeFunc` → model-generated test references deleted code. Delete the test file.
- `TestPatchToolNewFile` fails on macOS → system `patch` behaves differently. Use fallback parser.

### Current Status
- Website: https://altcode.io
- Multi-AI orchestrator: calls Claude Code, Codex CLI, and API models as backends
- 26 packages: engine, orchestrator, provider, tool (10), hooks (13 events), agent (teams), mcp, command, plugin, memory, cost, history, sandbox, task, auth, exec, tui, config, compact, store, sysctl, event
- CI green on Linux + macOS + Windows
- 5ms startup, 10MB binary
- Providers: Anthropic, OpenAI/Codex, altllm, DeepSeek, Zhipu/GLM, Moonshot/Kimi, MiniMax, Qwen, Ollama, LMStudio, OpenRouter (100+ models)
- Auth: auto-detects Claude Code sub + Codex CLI sub + 8 provider API keys + OpenRouter
- Claude Code compatible: loads CLAUDE.md, .mcp.json, settings.json, hooks, skills, plugins, agents, memory natively
- Plugin marketplace compatible: accepts both altcode native (`"commands": "commands/"`) and Claude Code marketplace (`"commands": ["./commands/setup.md", ...]`) manifest formats; auto-discovers nested plugins under `~/.claude/plugins/cache/<owner>/<repo>/`
- MCP spec compliant: performs the JSON-RPC `initialize` + `notifications/initialized` handshake on connect; lenient on `-32601 method not found` for minimal servers
- Workflow mode: optional interview → plan → ralph pipeline (altcode workflow)
- Skills: 117 discovered from `.claude/skills/` + `.agents/skills/` + plugin contributions (Codex-style paths in system prompt)
- TUI inspection commands: `/skills`, `/mcp`, `/plugins` show what's actually loaded into the session (in addition to `/tools`, `/agents`, `/backends`, `/memory`, `/doctor`)
- Benchmarks: DeepSeek 96%, MiniMax 93%, Qwen 93%, Claude 90% (5 suites × 6 models)
- 7-model benchmark: all 7 models pass identical coding task (see docs/benchmark-multi-model.md)

### Key Patterns
- Engine emits `<-chan event.Event` consumed by TUI or exec mode
- Tool dispatch respects concurrency (read tools parallel, write tools sequential)
- Messages use ContentPart for tool_use/tool_result blocks
- Permission evaluator checks before every tool execution
- Session messages persisted as JSON in SQLite

### Multi-AI Development Process

**Use both Claude Code and Codex CLI during design, thinking, and evaluation phases.**

This is a hard rule — not optional. Two AI systems catch more bugs, produce better
designs, and prevent blind spots from single-model reasoning.

#### 4-Stage Review Pipeline (HARD RULE)

Every implementation task MUST go through 4 stages before marking complete:

```
Implementer → Spec Reviewer → Code Quality Reviewer → Test Verifier
```

1. **Implementer** — writes code + tests, commits. Reports DONE / DONE_WITH_CONCERNS / NEEDS_CONTEXT / BLOCKED.
2. **Spec Reviewer** — compares implementation against the plan/spec. Checks: all requirements met, nothing over-built, types/signatures match. Reports ✅ or ❌ with specific gaps.
3. **Code Quality Reviewer** — checks error handling, SQL injection, resource leaks, thread safety, naming, test quality. Reports Approved or Issues (with fixes needed).
4. **Test Verifier** — runs tests with `-race`, checks coverage, identifies missing edge-case tests (nonexistent IDs, empty inputs, concurrent access, error paths), writes them if missing.

**Do NOT skip stages.** Do NOT proceed to the next task until all 4 stages pass.
If any reviewer finds issues, the implementer fixes them and the reviewer re-reviews.

When using subagents: dispatch a fresh subagent for each stage so context stays
clean. The implementer subagent is reused for fixes; reviewers get fresh context.

#### Review depth (HARD RULE)

When asking ANY AI (Claude Code, Codex, GPT, subagents) to review code, ALWAYS
require them to cover all three:

1. **Happy path** — does the normal-case input produce the right output?
2. **Unhappy path** — what happens on error, timeout, cancel, network failure,
   permission denied, partial response, broken pipe? Does the failure surface
   to the user, or get silently swallowed?
3. **Edge cases / corner cases** — empty input, nil pointer, max-length input,
   concurrent access, unicode/CJK/emoji, off-by-one boundaries, retry storms,
   resource exhaustion, malformed data, race windows.

A review that only covers the happy path is incomplete and must be re-run.
Surface this requirement explicitly in the review prompt: "Review happy path,
unhappy path, AND edge cases. Report any class of input the code mishandles."

#### When to use Codex

Use `codex --dangerously-bypass-approvals-and-sandbox` for:

1. **Design review** — before implementing a feature, ask Codex to review the plan:
   ```bash
   codex exec --dangerously-bypass-approvals-and-sandbox \
     "Read internal/engine/engine.go. Design [feature]. Show Go structs and functions."
   ```

2. **Adversarial challenge** — after implementing, ask Codex to break it:
   ```bash
   codex exec --dangerously-bypass-approvals-and-sandbox \
     "Read [files]. Find race conditions, security holes, edge cases. Be adversarial."
   ```

3. **Architecture scoring** — periodically ask Codex to score altcode vs competitors:
   ```bash
   codex exec --dangerously-bypass-approvals-and-sandbox \
     "Score altcode 1-10 on feature completeness, code quality, architecture. Compare to Claude Code CLI."
   ```

#### When to use Claude Code (this tool)

Use Claude Code for:
- Implementation (writing code, tests, docs)
- Deep codebase exploration and refactoring
- Running evaluator (`/evaluate`) with e2e tests
- Multi-file changes with dependency tracking

#### The generator/evaluator loop with both AIs

```
Claude Code (generate) → Codex (evaluate/challenge) → Claude Code (fix) → Codex (re-evaluate)
```

Every major feature should go through at least one Codex review before merging.
Codex findings become test cases in the evaluator suite.

#### Flags and configuration

- Codex requires `--dangerously-bypass-approvals-and-sandbox` in this environment
  (bwrap sandbox is blocked by the container runtime)
- Codex uses whatever model is configured in `~/.codex/config.toml` (currently GPT-5.4)
- Claude Code uses the model specified by `--model` or auto-detected credentials
- Both AIs have read access to the full codebase

---

# Project Development Guide

## Startup: sync skills from gstack

On every conversation start, check for gstack skill updates:

```bash
# Pull latest gstack to a temp dir and compare
GSTACK_TMP=$(mktemp -d)
git clone --depth 1 --quiet https://github.com/garrytan/gstack.git "$GSTACK_TMP" 2>/dev/null
```

For each skill directory in `$GSTACK_TMP` that has a `SKILL.md`, compare against
`.claude/skills/<skill>/SKILL.md`. If the upstream version is newer or different:

1. Copy the updated `SKILL.md` (and any resource files like checklists, templates)
2. Strip gstack-specific content: preamble bash blocks calling gstack binaries
   (`gstack-update-check`, `gstack-config`, `gstack-telemetry-log`), `~/.gstack/`
   paths, contributor mode sections, telemetry opt-in flows, `.tmpl` files
3. Replace `CC+gstack` with `AI-assisted` in effort tables
4. Preserve the YAML frontmatter and all actual skill logic

Skip these skills (not portable): `browse`, `gstack-upgrade`, `setup-browser-cookies`

```bash
rm -rf "$GSTACK_TMP"
```

Report briefly: "Skills synced — X updated" or "Skills up to date". Do not narrate
the diff unless asked.

## Commands

```bash
# Replace with your project's commands
npm install          # install dependencies
npm test             # run tests
npm run build        # build project
npm run lint         # lint code
npm run format       # format code
```

## Project structure

```
project/
├── src/             # Source code
├── tests/           # Test files
├── docs/            # Documentation
├── scripts/         # Build & utility scripts
├── .claude/         # Claude Code configuration
│   ├── settings.json
│   └── skills/      # Installed skills
├── .agents/         # Agent skills (Codex/Gemini/Cursor compatible)
│   └── skills/
├── .mcp.json        # MCP server configuration
├── CLAUDE.md        # This file — project config for Claude
└── package.json     # Project manifest
```

## Hard rules

### Commits
- **Never add Claude, AI, or any co-author attribution to commits.** No `Co-Authored-By`, no AI mentions in commit messages. Period.

### Secrets — zero tolerance
- **Never commit secrets.** API keys, private keys, tokens, passwords, connection strings,
  service account JSON — none of these belong in code, config files, logs, or comments.
- **Use environment variables** for all secrets. Reference `.env.example` for the schema.
- **Before every commit, check for secrets.** Grep for: `password`, `secret`, `api_key`,
  `token`, `private_key`, `-----BEGIN`, `AKIA`, `sk-`, `ghp_`, `gho_`, `xoxb-`, `xoxp-`.
  If any match, stop and extract to env vars.
- **Never log secrets.** Not even in debug mode. Mask or redact in all output.
- **.gitignore covers**: `.env*`, `*.pem`, `*.key`, `*.p12`, `credentials.json`,
  `service-account*.json`, SSH keys, GPG keys, kubeconfig, Docker config, npm/pypi tokens.

### Code red lines (not guidelines — hard limits)
- **File length: 800 lines max.** If a file exceeds 800 lines, split it. No exceptions.
- **Function length: 30 lines max.** If a function exceeds 30 lines, extract helpers.
- **Nesting depth: 3 levels max.** If nesting exceeds 3, use early returns or extract logic.
- **Conditional branches: 3 max per block.** If you need more than 3 branches, use a map, strategy pattern, or polymorphism.

These are not "try to keep short." They are **must not exceed.** Be obsessively clean.

### Project organization
- **Root directory holds the global map.** The root README, CLAUDE.md, and directory structure describe the full project topology.
- **Module directories hold member manifests.** Each module/package directory has an index or barrel file that declares what it exports.
- **File headers declare dependencies.** Imports go at the top. No dynamic requires buried in function bodies. Dependencies are visible at a glance.

### Enforcement over repetition
- **One hook beats a hundred prompts.** Write standards as linter rules, pre-commit hooks, or CI checks — not as prose that gets ignored. Catching violations at write-time is infinitely more effective than asking nicely in documentation.
- **Plans go in the project directory.** Store plans, design docs, and architecture decisions in `docs/plans/` or `PLAN.md` — not in chat history, not in memory, not in your head.

### Response style
- **Keep responses minimal.** Every unnecessary line of AI output is noise. Lead with the action or answer. Skip preamble, summaries of what was just done, and filler. If the diff speaks for itself, don't narrate it.

## Harness engineering (three pillars)

The model is not the bottleneck. The harness is the architecture. When the agent
struggles, fix the environment — not the symptom.

### Pillar 1: Context engineering
The repo is the single source of truth. If the agent can't find it, it doesn't know it.
- **CLAUDE.md** = commands, structure, hard rules, skill design, thinking frameworks
- **AGENTS.md** = agent definitions, patterns, harness architecture
- **docs/plans/** = design decisions, architecture, specs (not chat, not memory)
- **Module indexes** = barrel files declaring exports
- **File headers** = imports at top, dependencies visible at a glance

### Pillar 2: Architectural constraints
Constraining the solution space makes agents more productive, not less.
- Every constraint must be **mechanically enforced** (hook/linter/CI/test)
- Prose-only constraints are tech debt — upgrade to automation
- Dependency layers flow in one direction (no circular imports)
- Code red lines (800 lines, 30-line functions, 3 nesting, 3 branches) = hard limits

### Pillar 3: Entropy management
AI-generated code decays fast. Schedule cleanup as part of the loop.
- **Doc sync**: After shipping, verify docs match code (`/document-release`)
- **Constraint sweep**: Run all linters and structural tests (`/harness`)
- **Dead code**: Find orphaned files, unused exports, dead branches
- **Pattern alignment**: Ensure new code follows established patterns

### The meta-rule
> Every eval failure is a harness signal. When the agent produces bad output,
> ask: what context was missing? What constraint wasn't enforced? What feedback
> loop was broken? Fix the harness, then re-run.

## Design thinking frameworks

Three reasoning frameworks applied to every design and implementation decision.

### Socratic Questioning — surface hidden assumptions
Challenge every decision. Never accept "that's how it's done" as justification.
- **Clarifying**: "What problem does this actually solve?"
- **Probing assumptions**: "Why this? What if the opposite were true?"
- **Evidence**: "What data supports this? Has it been tested with users?"
- **Alternatives**: "What would this look like with zero configuration?"
- **Consequences**: "If this scales 10x, does the design still hold?"

### First Principles — decompose to real constraints
1. Identify the **real constraint**, not the assumed solution
   ("users need context-preserving navigation" not "we need a sidebar")
2. List **cognitive truths** (Fitt's Law, Miller's Law, attention scarcity)
3. Rebuild from truths. The "5 Whys": keep asking until you hit bedrock.

| Convention | First-principles question | Insight |
|-----------|--------------------------|---------|
| Settings page | "What if nothing needed config?" | Sensible defaults eliminate 80% of settings |
| Confirmation dialog | "Is the action actually destructive?" | Undo beats "are you sure?" |
| Loading spinner | "What can we show immediately?" | Skeleton screens reduce perceived wait |
| Pagination | "Why can't we show everything?" | Virtual scroll or better search |

### Occam's Razor — simplest valid solution wins
- Can I remove an element without losing function? → Remove it
- Can I merge two elements? → Merge them
- Can I replace custom with platform convention? → Replace it
- Would a new user understand this in 5 seconds? → If not, simplify

**Apply in sequence**: Socratic (surface assumptions) → First Principles (decompose)
→ Occam's Razor (choose simplest solution).

## Generator/evaluator pattern

**Never let the generator grade its own work.** Self-evaluation bias is real — agents
confidently praise mediocre output. Every significant generation step needs an
independent evaluator.

### Architecture (from Anthropic's harness design research)

```
┌─────────┐    generates    ┌──────────┐    evaluates    ┌───────────┐
│ Planner │ ──────────────→ │Generator │ ──────────────→ │ Evaluator │
│ (scope) │                 │ (builds) │ ←────────────── │ (grades)  │
└─────────┘                 └──────────┘    feedback      └───────────┘
                                 ↕                             │
                            iterate (max 5)              PASS / ITERATE / FAIL
```

### When to apply

| After generating... | Evaluate with... |
|---------------------|-----------------|
| Feature code | `/evaluate` — run tests, screenshot UI, grade criteria |
| Plan or design doc | `/evaluate` — check feasibility, scope clarity, risks |
| PR diff | `/review` + `/codex` — structural review + adversarial challenge |
| UI/frontend | `/qa` or `/design-review` — visual + functional verification |
| Any major deliverable | Spawn evaluator agent in background worktree |

### Core principles

1. **Separate generation from evaluation.** Different agent, different context, fresh eyes.
2. **Explicit grading criteria.** Convert subjective "is this good" into measurable
   dimensions with weights and pass thresholds. Never evaluate against vibes.
3. **Structured handoffs.** File-based communication between agents. The evaluator
   reads the actual output, not the generator's description of what it did.
4. **Decompose without over-specifying.** High-level specs prevent cascading errors
   better than granular technical prescriptions.
5. **Iterate with a cap.** Max 5 generator/evaluator cycles. If still failing,
   escalate — the approach needs rethinking, not more iterations.
6. **Continuously test assumptions.** Every harness component encodes an assumption
   about what the model can't do alone. Re-examine as models improve.

### Verdict thresholds

- **PASS** (≥ 7.0 weighted, no blockers): Ship it
- **ITERATE** (5.0–6.9 or has blockers): Fix blockers, re-evaluate
- **FAIL** (< 5.0): Fundamental rethink needed

### Applying the pattern

- **After `/ship`**: Spawn evaluator agent to independently verify the PR
- **After feature impl**: Run `/evaluate` before showing the user
- **After plan writing**: Run `/plan-eng-review` + `/plan-ceo-review` as evaluators
- **Long-running builds**: Use planner → generator → evaluator loop with sprint contracts

## Platform-agnostic design

Skills must NEVER hardcode framework-specific commands, file patterns, or directory
structures. Instead:

1. **Read CLAUDE.md** for project-specific config (test commands, build commands, etc.)
2. **If missing, ask the user** — let them tell you
3. **Persist the answer to CLAUDE.md** so we never have to ask again

The project owns its config; skills read it.

## Commit style

**Always bisect commits.** Every commit should be a single logical change. When
you've made multiple changes (e.g., a rename + a rewrite + new tests), split them
into separate commits before pushing. Each commit should be independently
understandable and revertable.

Examples of good bisection:
- Rename/move separate from behavior changes
- Test infrastructure separate from test implementations
- Mechanical refactors separate from new features

## CHANGELOG style

CHANGELOG.md is **for users**, not contributors. Write it like product release notes:

- Lead with what the user can now **do** that they couldn't before
- Use plain language, not implementation details
- Every entry should make someone think "oh nice, I want to try that"

## Skill design principles

Skills are **folders**, not just markdown files. The entire file system is a form of
context engineering and progressive disclosure.

### Structure
- **Use `references/`, `scripts/`, `templates/` subdirs.** Tell Claude what files
  exist in the skill folder; it will read them at appropriate times. Split detailed
  API signatures into `references/api.md`, output templates into `templates/`.
- **Store scripts and libraries.** Give Claude code to compose, not reconstruct.
  Helper functions, query builders, assertion libraries — these let Claude spend
  turns on decisions, not boilerplate.

### Content
- **Don't state the obvious.** Claude already knows how to code. Focus on what
  pushes it out of its defaults — your org's conventions, footguns, edge cases.
- **Build a Gotchas section.** The highest-signal content in any skill. Grow it
  over time from actual failure points Claude hits.
- **Avoid railroading.** Give Claude information and flexibility. Overly specific
  step-by-step instructions break when the situation doesn't match exactly.

### Configuration
- **The description field is for the model.** It's not a summary — it's a trigger
  spec. Write it as "use when X, Y, Z" so Claude can match requests to skills.
- **On-demand hooks > always-on hooks.** Skills like `/careful` and `/freeze`
  register hooks only when invoked, keeping other sessions clean.
- **Store setup in `config.json`.** If a skill needs user context (Slack channel,
  project ID), persist it in the skill directory so it's only asked once.

### Memory & data
- **Skills can store data.** Append-only logs, JSON state, SQLite — whatever fits.
  Use `${CLAUDE_PLUGIN_DATA}` for stable storage that survives skill upgrades.
- **Previous results improve future runs.** A standup skill that reads its own
  history can show deltas. A review skill that tracks past findings avoids repeats.

### Skill categories to consider
1. **Library & API reference** — internal libs, CLIs, SDKs with gotchas and examples
2. **Product verification** — test flows with Playwright/tmux, programmatic assertions
3. **Data fetching & analysis** — connect to monitoring, dashboards, event sources
4. **Business process automation** — standup posts, ticket creation, weekly recaps
5. **Code scaffolding & templates** — framework boilerplate with org conventions
6. **Code quality & review** — style enforcement, adversarial review, test practices
7. **CI/CD & deployment** — PR babysitting, deploy pipelines, cherry-pick workflows
8. **Runbooks** — symptom → investigation → structured report
9. **Infrastructure operations** — cleanup, dependency management, cost investigation

## Available skills

### Harness engineering
- `/harness` — Orchestrate full dev/test/eval flow, audit three pillars, entropy cleanup
- `/generate` — Elite product builder (30-year Google/Apple/Microsoft PM/CTO/designer)
- `/evaluate` — Elite evaluator (30-year Google/AWS test expert + Apple design master)

### Development workflow
- `/office-hours` — Brainstorm ideas, startup diagnostic, builder mode
- `/plan-ceo-review` — CEO/founder-mode plan review (scope expansion/reduction)
- `/plan-eng-review` — Engineering architecture review
- `/plan-design-review` — Design dimension audit
- `/design-consultation` — Create a design system from scratch

### Implementation
- `/investigate` — Systematic debugging with root cause analysis
- `/coding-agent` — Run Codex/Claude/agents in background with worktrees
- `/careful` — Safety warnings for destructive commands
- `/freeze` / `/unfreeze` — Lock edits to a specific directory
- `/guard` — Maximum safety mode (careful + freeze)

### Quality & evaluation
- `/evaluate` — Generator/evaluator quality gate (grade any output against criteria)
- `/qa` — Browser QA testing + automatic fix loop
- `/qa-only` — QA report only, no fixes
- `/review` — Pre-landing PR code review
- `/design-review` — Visual design audit + fix loop

### PR workflow
- `/review-pr` — Structured PR review (sections A-J)
- `/prepare-pr` — Rebase, fix review issues, run gates, push
- `/merge-pr` — Squash merge after prepare-pr

### Release
- `/ship` — Full ship workflow: merge base, test, review, PR
- `/document-release` — Post-ship documentation updates
- `/retro` — Weekly engineering retrospective

### Design (impeccable)
- `/polish` — Refine UI details, spacing, alignment
- `/audit` — Design audit with P0-P3 severity ratings
- `/critique` — Score against Nielsen's 10 usability heuristics
- `/typeset` — Typography improvements
- `/colorize` — Color system refinement
- `/arrange` — Layout and composition
- `/animate` — Motion and transitions
- `/bolder` / `/quieter` — Increase or decrease visual weight
- `/overdrive` — Maximum design intensity
- `/distill` — Simplify and reduce
- `/adapt` — Responsive design adjustments
- `/harden` — Accessibility and robustness
- `/clarify` — Improve readability and hierarchy
- `/normalize` — Consistency pass
- `/delight` — Add micro-interactions and polish
- `/optimize` — Performance-focused design
- `/extract` — Extract design tokens and patterns
- `/onboard` — First-run and onboarding UX
- `/frontend-design` — Enhanced frontend design skill
- `/teach-impeccable` — Gather design context for all skills

### Second opinions
- `/codex` — Get a second opinion from OpenAI Codex CLI

## AI effort compression

When estimating or discussing effort, show both human-team and AI-assisted time:

| Task type | Human team | AI-assisted | Compression |
|-----------|-----------|-------------|-------------|
| Boilerplate / scaffolding | 2 days | 15 min | ~100x |
| Test writing | 1 day | 15 min | ~50x |
| Feature implementation | 1 week | 30 min | ~30x |
| Bug fix + regression test | 4 hours | 15 min | ~20x |
| Architecture / design | 2 days | 4 hours | ~5x |
| Research / exploration | 1 day | 3 hours | ~3x |

Completeness is cheap. Don't recommend shortcuts when the complete implementation
is achievable. Implement the full solution when the scope is reasonable.
