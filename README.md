# altcode

**[altcode.io](https://altcode.io)** · [Install](#install) · [Benchmarks](#benchmarks) · [Releases](https://github.com/jiayaoqijia/altcode/releases) · [Full docs](docs/README.full.md)

> **The harness is the product. The model is the engine. Pick the engine you want.**

**altcode** is the open-source coding **harness** that gives every model — DeepSeek, GPT, AltLLM, MiniMax, GLM, Kimi, Qwen, Llama, anything self-hosted — the same multi-turn agent loop, permission system, context compaction, and multi-agent orchestration that Claude Code popularised for one model family. **One binary. Thirteen providers. No vendor lock-in.**

```
5ms startup · 17MB binary · 13 providers · 100+ models · 11 tools · 53 slash commands
type-ahead /queue · runtime /model swap · OSC-8 hyperlinks · CC-parity permission modal
```

### If you only have 30 seconds

| | |
|---|---|
| **13** | providers · one binary · one configuration |
| **100+** | models reachable through one `--model` flag |
| **7 / 7** | backends ship working code on the same task ([benchmark](docs/benchmark-multi-model.md)) |
| **0** | vendor lock-in — swap engine with one flag |
| **AGPL-3.0** | open-source, owned by no model vendor |

> Claude Code is the polished coding CLI of the moment, on Anthropic's own SWE-bench Verified scores — but it only runs Claude. altcode runs the same agentic loop against 13 providers and 100+ models, including AltLLM, DeepSeek, GPT and local Llama/Qwen. On a small benchmark it shipped working code from every backend tested. `altcode --model altllm-basic "fix the failing tests"` is the AltLLM-paired one-liner; the same flag swaps any other provider in.

### Two stacks. One coupled, one open.

```
       Vendor-locked stack                 Open stack
       (Claude Code shape)                 (altcode shape)
       ───────────────────                 ──────────────
        Your application                  Your application
              │                                  │
              ▼                                  ▼
    Harness (closed, vendor-shipped)    Harness (open, vendor-neutral)
              │                                  │
              ▼                                  ▼
       One model family            Any of 100+ models · swap with one flag
```

The harness layer is doing real work. The question is **who owns it** and whether the model underneath is interchangeable.

> **Independent dual-reviewer scoring (round 5)** — altcode TUI vs [DeepSeek-TUI](https://github.com/Hmbown/DeepSeek-TUI):
> Features **9.15** vs 8.5 · UI **8.30** vs 8.5 · Stability **7.70** vs 7.0. *altcode leads 2 of 3 unanimously.*

Long-form positioning piece: **["Why DeepSeek and GPT need AltCode to beat Claude Code"](https://x.com/yq_acc/status/2052398685947126102?s=20)** (April 2026).

## Quick start

```bash
# Install (macOS / Linux)
curl -fsSL https://altcode.io/install.sh | bash

# Log in with your ChatGPT subscription (or set ANTHROPIC_API_KEY / OPENAI_API_KEY)
altcode login codex

# Start coding
altcode "fix the failing tests"
```

Zero config — altcode auto-detects Claude Code, Codex CLI, and any other installed agent credentials.

<details>
<summary><b>More CLI examples</b> — headless, multi-agent, batch, daemon</summary>

```bash
# CLI headless with output format control
altcode --output-format json "analyze this code"
altcode --output-format diff "refactor the auth module"

# Permission modes
altcode --permission-mode plan "what would you change?"   # read-only, no writes
altcode --dry-run "add rate limiting"                     # alias for plan mode
altcode --permission-mode bypass "fix all tests"          # auto-allow everything

# Context injection
altcode --file src/api.ts --system "Focus on error handling" "review this"
altcode --image screenshot.png "What's wrong with this UI?"  # Anthropic multimodal
altcode --prompt-file task.txt                               # read prompt from file

# Budget controls
altcode --max-turns 10 --max-cost 1.50 "implement feature"
altcode --print-cost --print-tree "add auth"               # cost + tool tree on stderr

# Multi-agent workspace — orchestrate claude + codex together
altcode workspace "build JWT auth" claude:architect codex:coder

# Batch mode
altcode --prompt-each prompts.txt --parallel 3 "Review: {{input}}"

# Run as HTTP daemon (AltFix)
altcode daemon --port 9200 --auth-token $TOKEN

# Use any provider
altcode --model minimax/MiniMax-M2.7 "add rate limiting"
altcode --model kimi/kimi-k2 "write tests"
altcode --model altllm/altllm-basic "review security"
```

</details>

## CLI vs harness: the engineering distinction

A coding CLI sends a prompt and prints a response. A coding **harness** gives the agent the surrounding scaffolding it needs to do useful work across many turns: token budgets, permission gates, automatic compaction, post-edit verification, multi-agent orchestration, persistent memory. The right column below is what a competent coding harness looks like in 2026 — Claude Code has it for one model family, Codex CLI has a leaner version for another. **altcode generalises it: the same surface, against thirteen providers, behind one binary.**

| | Coding CLI | altcode-class harness |
|---|---|---|
| Agent loop | Run once | Multi-turn with verification gates + 50-iter cap |
| Context | Send and hope | Token tracking, auto-compact at 90%, LLM summarization |
| Tools | Call and pray | Permission system, pre/post hooks, auto-verify (`go build`) |
| Agents | Single model | Multi-agent: cc+opus, codex+gpt, altcode+minimax/glm/kimi |
| Workflow | Ad-hoc | Interview → plan → persistent execution (ralph) |
| Quality | Trust output | Generator/evaluator loop, adversarial review |
| Memory | Session-only | Cross-session persistent memory with `MEMORY.md` index |
| Providers | One vendor | 13 providers, any model, existing subscriptions |

## The credential trick — one harness, many subscriptions

Anthropic's Claude Code usage policy restricts re-wrapping CC subscriptions through third-party tooling — read as a practical bar on a single CLI proxying many users' Claude sessions. altcode's design sidesteps that question entirely: **each subagent uses its own credentials against its own provider on its own subscription, with no proxying**.

```
altcode (main: GPT / MiniMax / GLM / Kimi)
  ├── Agent(claude)     → uses Claude Code subscription (Opus)
  ├── Agent(codex)      → uses Codex subscription (GPT)
  ├── Agent(altcode, model=minimax/MiniMax-M2.7) → uses MiniMax API key
  ├── Agent(altcode, model=kimi/kimi-k2)         → uses Kimi coding plan
  └── Agent(altcode, model=zhipu/glm-4.7)        → uses GLM coding plan
```

A team that wants Claude for the architect role, GPT for the implementer, Kimi for the reviewer, and DeepSeek for cost-sensitive batch tasks runs all four through one binary, with one configuration, one permission model, one log stream. Each agent maintains its own paid relationship to the underlying provider. **If the harness layer stays a vendor privilege, model choice becomes a vendor privilege too. That's the whole argument.**

## Benchmarks

**7-model parity** (identical coding task, all run through altcode's harness):

| Model | Tests passing | Bugs fixed | Methods added |
|-------|:------------:|:----------:|:-------------:|
| Claude Code | **11** | 3/3 | 5/5 |
| altcode + GPT | 8 | 3/3 | 5/5 |
| altcode + altllm | 9 | 3/3 | 5/5 |
| altcode + Kimi | 8 | 3/3 | 5/5 |
| altcode + Qwen | 8 | 3/3 | 5/5 |
| altcode + DeepSeek | 6 | 3/3 | 5/5 |
| Codex CLI | 7 | 3/3 | 5/5 |

All 7 produce correct, race-safe Go code. See [full benchmarks](docs/benchmark-multi-model.md) and the [3-way comparison](docs/benchmark-three-way.md).

## Install

**Recommended (macOS / Linux):**

```bash
curl -fsSL https://altcode.io/install.sh | bash
```

Defaults to `~/.local/bin` (no sudo). Pre-built binaries from GitHub Pages; falls back to GitHub Releases, then `go build` from source. OS-version preflight warns on glibc < 2.17 / macOS < 10.15 (or 11 for arm64).

<details>
<summary><b>Other install paths</b> — go install, build from source, manual download</summary>

```bash
# Go install
go install github.com/jiayaoqijia/altcode/cmd/altcode@latest

# Build from source
git clone https://github.com/jiayaoqijia/altcode.git
cd altcode && make build
sudo cp dist/altcode /usr/local/bin/
```

Pre-built binaries from [Releases](https://github.com/jiayaoqijia/altcode/releases):

| Platform | Binary | Size |
|----------|--------|:----:|
| macOS Apple Silicon | `altcode-darwin-arm64` | 16MB |
| macOS Intel | `altcode-darwin-amd64` | 17MB |
| Linux x86_64 | `altcode-linux-amd64` | 17MB |
| Linux ARM64 | `altcode-linux-arm64` | 16MB |
| Windows x64 | `altcode-windows-amd64.exe` | 17MB |

```bash
curl -L https://github.com/jiayaoqijia/altcode/releases/latest/download/altcode-linux-amd64 -o altcode
chmod +x altcode && sudo mv altcode /usr/local/bin/
```

No runtime dependencies. No Node.js. No Python. Single binary.

</details>

## Provider support

| Provider | Prefix | Auth |
|----------|--------|------|
| **Claude** (Anthropic) | `anthropic/` | Claude Code sub, `ANTHROPIC_API_KEY` |
| **GPT** (OpenAI / Codex) | `openai/` | `altcode login codex`, Codex sub, `OPENAI_API_KEY` |
| **altllm** | `altllm/` | `ALTLLM` env var |
| **DeepSeek** | `deepseek/` | `DEEPSEEK_API_KEY` |
| **GLM** (Zhipu) | `zhipu/` | `ZHIPU_API_KEY` |
| **Kimi** (Moonshot) | `moonshot/` | `MOONSHOT_API_KEY` |
| **MiniMax** | `minimax/` | `MINIMAX_API_KEY` |
| **Qwen** (Alibaba) | `qwen/` | `DASHSCOPE_API_KEY` |
| **Ollama** / **LM Studio** | `ollama/` `lmstudio/` | local, no key |
| **OpenRouter** | `openai/` | `OPENROUTER` (100+ models) |

```bash
# Auto-detected prefix — no slash needed for known providers
altcode --model deepseek-v3 "fix this"
altcode --model kimi-k2 "write tests"
altcode --model altllm-basic "review security"
```

<details>
<summary><b>Coding-plan configs</b> — MiniMax / GLM / Kimi via Anthropic-compat endpoints</summary>

Chinese AI providers offer subscription-based coding plans with Anthropic-compatible APIs. altcode auto-detects the protocol from the configured `baseURL` (when it contains `/anthropic` or `/coding`):

```jsonc
// ~/.altcode/config.json — MiniMax coding plan
{
  "model": "minimax/MiniMax-M2.7",
  "provider": {
    "minimax": {
      "apiKey": "$MINIMAX_API_KEY",
      "baseURL": "https://api.minimax.io/anthropic"
    }
  }
}
```

```jsonc
// GLM (Zhipu) coding plan
{
  "model": "zhipu/glm-4.7",
  "provider": {
    "zhipu": { "apiKey": "$ZHIPU_API_KEY", "baseURL": "https://api.z.ai/api/anthropic" }
  }
}
```

```jsonc
// Kimi (Moonshot) coding plan
{
  "model": "kimi/kimi-k2",
  "provider": {
    "moonshot": { "apiKey": "$MOONSHOT_API_KEY", "baseURL": "https://api.kimi.com/coding/" }
  }
}
```

</details>

## Claude Code compatibility

altcode natively loads the entire Claude Code ecosystem — `CLAUDE.md`, `.mcp.json`, `.claude/settings.json`, `.claude/skills/` (47), `.agents/skills/`, plus `.claude-plugin/` directories in both altcode-native and CC marketplace formats. Plugins under `~/.claude/plugins/cache/<owner>/<repo>/` from the Claude Code marketplace are auto-discovered.

## Advanced

<details>
<summary><b>CLI feature parity</b> — 37 flags covering ~90% of TUI features for vim/tmux/scripts</summary>

| Category | Flags |
|----------|-------|
| **Output** | `--output-format text\|json\|stream-json\|diff`, `--verbose`, `--quiet`, `--print-cost`, `--print-tools`, `--print-tree`, `--show-system` |
| **Permission** | `--permission-mode plan\|auto\|default\|bypass`, `--allow-tool`, `--deny-tool`, `--dry-run`, `--permission-prompt-tool` |
| **Session** | `--continue`, `--fork-session`, `--session-db`, `--list-sessions` |
| **Input** | `--image`, `--file`, `--prompt-file`, `--system`, `--system-file` |
| **Hooks** | `--hook event:cmd`, `--mcp name:cmd`, `--skill name` |
| **Artifacts** | `--save-transcript`, `--save-cost`, `--save-diff`, `--commit`, `--commit-dirty` |
| **Budget** | `--max-turns`, `--max-cost` |
| **Batch** | `--prompt-each`, `--parallel`, `--retry`, `--bail` |
| **Inspect** | `--print-config`, `--print-tools-list`, `--print-skills`, `--print-mcp`, `--doctor` |

Validation errors exit with code 64 (`EX_USAGE`). Design spec reviewed across 7 rounds by CC + Codex.

</details>

<details>
<summary><b>Multi-agent workspace</b> — git worktrees, agent panes, PR lifecycle</summary>

```
altcode workspace "build auth system"
  ├── architect (claude)     ← designs the architecture
  ├── implementer (codex)    ← writes the code
  └── reviewer (claude)      ← reviews for bugs + security

Each agent gets:
  • Its own git worktree (isolated branch)
  • Live output streaming to split-pane TUI
  • Turn checkpoints for rollback
  • Automatic PR creation + CI tracking
```

```bash
altcode workspace "task"                 # start workspace
altcode workspace "task" --tmux          # full TUI mode in tmux panes
altcode workspace "task" --agents codex  # choose specific backends
altcode workspace status                 # show all agent states
altcode workspace list                   # list workspaces
altcode workspace resume                 # re-attach to live agents
altcode workspace spawn reviewer         # add an agent mid-run
```

12-state lifecycle: `spawning → working → pr_open → ci_checking → approved → mergeable → merged → cleanup → done`. CI failure routes the log back to the agent for repair (3 retries). PR review comments become follow-up tasks. Auto-merge when CI green and reviews approve.

</details>

<details>
<summary><b>AltFix daemon</b> — HTTP server for autonomous coding agents</summary>

```bash
altcode daemon --port 9200 --auth-token $TOKEN --data-dir ~/.altcode/daemon
```

REST endpoints (all require `Authorization: Bearer $TOKEN` except `/health`):

```
POST /tasks              Create a coding task (201/400/409)
GET  /tasks              List all tasks
GET  /tasks/:id          Get task + queue position
GET  /tasks/:id/sse      SSE event stream (Last-Event-ID replay)
GET  /tasks/:id/checkpoints  Named phase snapshots
POST /tasks/:id/steer    Inject guidance mid-execution
POST /tasks/:id/stop     Cancel a running task
POST /webhooks/github    GitHub webhook trigger (@altfix label/comment)
GET  /health             Liveness probe (no auth)
```

```bash
curl -X POST http://localhost:9200/tasks \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/you/repo",
    "task": "add rate limiting to the API",
    "model": "altllm-basic",
    "branch": "fix/rate-limit",
    "max_cost_usd": 2.00,
    "max_turns": 30
  }'
```

Pipeline: **Lead → Implement → Review → Test**. Spawns codex/claude/altcode as subprocesses, persists to SQLite, streams via SSE. See [full daemon docs](docs/README.full.md#altfix-daemon-altcode-daemon) for SSE event types, webhook payload formats, and stop/steer error semantics.

</details>

<details>
<summary><b>Workflow mode</b> — Socratic interview, consensus plan, persistent ralph</summary>

```bash
altcode workflow --mode interview "add rate limiting"      # Socratic clarification
altcode workflow --mode plan "redesign the config system"  # consensus plan, no execute
altcode workflow --mode ralph "implement the full auth"    # persistent loop until verified

# Auto-detect from keywords
altcode workflow "$ralph fix all failing tests"
altcode workflow "clarify what the hooks system does"
```

</details>

<details>
<summary><b>Three pillars</b> — context engineering, architectural constraints, entropy management</summary>

altcode is built on three harness engineering principles (inspired by [OpenHarness](https://github.com/HKUDS/OpenHarness)):

**1. Context engineering** — the codebase IS the context. `CLAUDE.md` for project conventions. Skills (47) for progressive disclosure. Memory for cross-session knowledge. MCP for external tools and resources.

**2. Architectural constraints** — permission system (4 modes with glob rules), 13 lifecycle hook events, auto-verify (`go build` after every `.go` edit), restricted tool sets for subagents.

**3. Entropy management** — LLM context compaction, pre-turn proactive compaction at 90% window, generator/evaluator loop with separate grader agent, workflow-mode interview-before-coding.

</details>

<details>
<summary><b>Configuration</b> — full <code>~/.altcode/config.json</code> example</summary>

```jsonc
{
  "model": "openai/gpt-5.4",
  "context_window": 1000000,    // override per-model (auto-detected from API)
  "compact_threshold": 700000,  // trigger auto-compact at this token count
  "provider": {
    "openai":    { "apiKey": "$OPENAI_API_KEY" },
    "anthropic": { "apiKey": "$ANTHROPIC_API_KEY" },
    "altllm":    { "apiKey": "$ALTLLM" },
    "deepseek":  { "apiKey": "$DEEPSEEK_API_KEY" }
  },
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash",
      "hooks": [{ "type": "http", "url": "https://hooks.example.com/pre-tool" }]
    }]
  }
}
```

</details>

<details>
<summary><b>Harness architecture</b> — what's under <code>internal/</code></summary>

```
cmd/altcode/         CLI entry point (login, workflow, workspace, team, sessions)
internal/
├── workspace/       Orchestration — session, store, worktrees, mailbox, eventlog
├── lifecycle/       12-state machine, CI auto-fix, review routing, auto-merge
├── scm/             GitHub REST API client — PR/CI/review lifecycle
├── engine/          Agent loop — tool dispatch, permissions, hooks, auto-compact
├── provider/        13 providers (Anthropic, OpenAI, altllm, DeepSeek, GLM, Kimi, …)
├── compact/         Context management — LLM summarization, pre-turn proactive
├── agent/           Multi-agent — mailbox IPC, history forking, team orchestration
├── workflow/        Interview / plan / ralph pipelines
├── tool/            15 built-in tools — Agent, Tasks, Read/Write/Edit, Bash, Patch, …
├── hooks/           13 events — command, prompt, HTTP webhook hooks
├── oauth/           OAuth PKCE + device-code flow for ChatGPT subscription
├── mcp/             MCP client — stdio + SSE, namespace isolation
├── command/         Skills loader — `.claude/skills/` + `.agents/skills/`
├── plugin/          `.altcode-plugin/` and `.claude-plugin/` formats
├── memory/          Persistent memory — `MEMORY.md` index
├── permission/      4 modes (default/auto/bypass/plan) with glob rules
├── tui/             Terminal UI — workspace dashboard, agent panes, HUD, tool tree
├── config/          JSONC config, env expansion, instruction cascade
├── store/           SQLite sessions with `--continue` resume
├── exec/            Headless mode with banner, progress, cost tracking
└── event/           Typed events (engine ↔ TUI ↔ exec)
```

</details>

<details>
<summary><b>What's new (May 2026)</b> — recent fixes and additions</summary>

- **Full-width chat body** — Files sidebar removed; the conversation now uses every column of the terminal.
- **HUD timer disambiguated** — `⟳ Ns` for turn-in-progress, `⏱ Ns` for session-idle. Glyph-prefixed so the two clocks are never confused.
- **Cost format unified** — adaptive precision via `formatUSD`; the HUD chip, the inline turn summary, and `/cost` now all show the same number at the same precision.
- **Assistant bullet inlined** — `┃ ●   answer` on one line instead of `┃ ●` over `┃   answer` over two lines (33% denser).
- **500ms double-submit debounce** — paste-and-Enter races no longer fire the same prompt twice.
- **No-sudo install** — defaults to `~/.local/bin`, sudos only if you opt into a system path interactively.
- **OS-version preflight** — installer warns on glibc < 2.17 / macOS < 10.15 (or 11 for arm64) before downloading the wrong binary.
- **macOS Terminal HUD fix** — auto-detects `Apple_Terminal` and skips the dark-mode background that rendered as solid black.
- **Mouse-selection escape** — `ALTCODE_NO_MOUSE=1` disables Bubbletea mouse capture so you can drag-select with the OS terminal.
- **Rich `--version`** — prints commit, build date, Go version, and platform.
- **`/queue` type-ahead** — type prompts while a turn is running; FIFO drain on completion. Visible `▶ N queued` chip in HUD.
- **Runtime `/model` switcher** — fuzzy substring match against the disk-cached model list. `/model haiku` → `anthropic/claude-haiku-4-5`.
- **`/anchor` facts** — survive compaction AND restart. Stored in `~/.altcode/anchors.json`.
- **`/share` to markdown** — exports session to `~/.altcode/shares/<ts>-<slug>.md`.
- **OSC-8 file:line hyperlinks** in tool output. Cmd/Ctrl-click `internal/foo.go:42` to jump to the source.
- **Cache-hit `%` chip** in HUD — surfaces prefix-cache effectiveness per turn.
- **OSC 9 desktop notification** when a turn finishes after 20s.
- **Permission modal default-on** (CC parity) — `y` / `n` / `a` (always) / `!` (allow all this tool). `ALTCODE_AUTO_APPROVE=1` to skip.
- **CC-style resume** — `-c`, `--continue`, `--resume`, `/resume last` all work. `/resume <id>` hot-swaps in-place.
- **LoopGuard** — blocks runaway tool repeats (3 same-input) and consecutive errors (8) so the agent can't burn cost on flaky paths.

</details>

## Where altcode still trails Claude Code

Honesty is the editorial test. The cases where altcode falls short are the ones a sophisticated buyer should look at first:

- **Native model fluency.** Claude Code is co-designed with Claude. The agent prompt, tool definitions, failure recovery and streaming behaviour are tuned together. altcode is a third-party harness; it inherits the model's behaviour, it doesn't shape it. On Claude itself, Claude Code will continue to lead.
- **Stretch-test coverage.** The 7-model benchmark above shows altcode trailing on the secondary test count even when correctness is matched. Real gap; will narrow as the harness matures and open-source models close the capability gap.
- **Polish + onboarding.** Claude Code is a productised commercial offering with polished installer, docs, error messages. altcode's gap is closing fast (curl-pipeable installer, OAuth login, OSC-8 hyperlinks, permission modal at parity in May 2026), but a non-technical buyer will still notice.
- **Single-vendor support relationship.** Anthropic is on the line for Claude Code uptime. altcode is a community project. For teams whose budget includes vendor support contracts, that asymmetry matters.
- **Subprocess + credential risk.** The harness spawns model-controlled CLIs as subprocesses with the user's environment. The permission system gates tool calls but doesn't isolate process scope. Operating altcode against an untrusted model or in CI requires sandboxing the harness itself — your responsibility today.
- **Provider quirks the harness can't hide.** Different providers handle tool-use, rate limits, streaming chunks and structured-output reliability differently. altcode's normalisation layer is good but not magical.

## The category is plural — peers worth comparing

altcode is not alone in the open-multi-provider quadrant, and we'd be dishonest to pretend otherwise. Evaluate two or three of these together before standardising:

- **[Aider](https://github.com/Aider-AI/aider)** — longest-running open-source coding-agent CLI, provider-flexible, extremely good for git-aware single-repo edits. No multi-agent workspace or daemon yet.
- **[OpenHands](https://github.com/All-Hands-AI/OpenHands)** (formerly OpenDevin) — closest spiritual peer. Open-source agent platform with multi-provider support and a strong research line. More ambitious in scope than altcode, currently less focused on terminal ergonomics.
- **[Codex CLI](https://github.com/openai/codex)** — OpenAI's first-party harness. Provider-locked to OpenAI, very polished.
- **[Continue](https://github.com/continuedev/continue), [Aichat](https://github.com/sigoden/aichat), [OpenCode](https://github.com/opencode-ai/opencode)** — adjacent slices of the surface.

altcode's distinctive choices: a single statically-linked Go binary, the [four-role daemon](#advanced) for self-hosting, deliberate parity with Claude Code's user-facing surface.

## Risk & disclaimer

altcode ships under [AGPL-3.0](LICENSE) **AS-IS, WITHOUT WARRANTY OF ANY KIND** (sections 15–17). These are not suggestions — read them before pointing altcode at code you care about:

| Risk | What it means | Mitigation |
|------|---------------|------------|
| **No vendor support** | No SLA, on-call, or support team. Community project. | Pay Anthropic for Claude Code if you need a support contract. |
| **Shell execution scope** | The permission system gates **tool calls**, not **process scope**. A compromised model can run destructive shell commands. | Sandbox the harness yourself for CI / untrusted prompts (containers, gVisor, restricted users, ephemeral worktrees). |
| **Credential inheritance** | Subagents inherit the parent process environment. | Review env + `~/.altcode/config.json` before running on untrusted input. |
| **Repository damage** | Edit / Write / Patch / Bash tools modify files and run shell. | `--permission-mode plan` or `--dry-run` first. Commit before agent runs. |
| **Provider quirks** | Tool-use, rate limits, streaming, structured-output reliability vary across providers. | Normalisation is good but not magical. Community-tracked failure modes change monthly. |
| **No data / PII guarantees** | Prompts and code go to whichever provider you configure. | Read each provider's terms. For on-prem, use `ollama/` or `lmstudio/`; do not configure a remote provider alongside. |
| **Pre-1.0 software** | Behaviour, flags, config schema and events still evolving. | Pin to a release (`@v0.10.5`) or commit hash for reproducible rebuilds. |

If any row is a deal-breaker: **Claude Code on Claude** for vendor support, or [self-hosted OpenHands](https://github.com/All-Hands-AI/OpenHands) for a containerised execution model.

## The verdict

The case for altcode is **not** that it beats Claude Code on Claude. The case is that the coding-harness category should not stay a vendor privilege. Every model that wants to be taken seriously as a coding partner needs the same multi-turn loop, permission system, context compaction, multi-agent orchestration and persistent memory that Claude Code popularised for one model. altcode is one open-source attempt at exactly that.

For a team that has standardised on Claude and is comfortable paying Anthropic, **Claude Code remains the right tool**. For a team that wants to keep its model choice open — for cost, sovereignty, on-prem inference, or simple optionality — **altcode is one publicly developed answer that doesn't require rebuilding the harness in-house**. It is not the only such project, and the race is not over.

> *In 2026 the buyer has real model choice for the first time. That changes which tooling layer matters most.*

## Development

```bash
make build          # Build binary (dist/altcode)
make test           # Run 400+ tests with race detector
make lint           # Run go vet
make verify         # Per-version pre-ship gate — walks every command in
                    # README + altcode.io and confirms it works against
                    # the binary you're about to ship. Run before every
                    # release. (53 checks across 7 sections.)
make verify-remote  # Same + also fetches https://altcode.io/bin/<asset>
                    # and diffs the SHA against docs/bin/ to catch
                    # "shipped a stale binary" regressions.
make release VERSION=vX.Y.Z
                    # Full release: verify → rebuild 4 platforms with
                    # ldflags → bump docs/index.html chip → print the
                    # 4 git/push steps you need to run by hand.
```

The `verify-instructions.sh` script is the single source of truth for "is this version fit to ship". When you change a documented command, add it there. When `make verify` fails, the README/webpage drifted from reality — fix the README, the webpage, or the binary, never the test.

## Links

- **Website**: [altcode.io](https://altcode.io)
- **GitHub**: [github.com/jiayaoqijia/altcode](https://github.com/jiayaoqijia/altcode)
- **Releases**: [Download binaries](https://github.com/jiayaoqijia/altcode/releases)
- **Full README** (every section unfolded): [docs/README.full.md](docs/README.full.md)
- **Benchmarks**: [7-model](docs/benchmark-multi-model.md) · [3-way](docs/benchmark-three-way.md) · [altllm](docs/benchmark-altllm.md)

## License

See [LICENSE](LICENSE).
