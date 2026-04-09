# altcode

**[altcode.io](https://altcode.io)** · [Install](#install) · [Docs](#harness-architecture) · [Benchmarks](#benchmarks) · [Releases](https://github.com/jiayaoqijia/altcode/releases)

**The universal agent harness for coding.** Orchestrate Claude Code, Codex, OpenCode, Aider, OpenClaw — or any CLI agent — from one terminal. Git-native coordination with worktrees, CI auto-fix, and split-pane TUI.

```
5ms startup · 17MB binary · 13 providers · 100+ models · 15 tools · 8 agent backends · 20+ commands · CC-parity TUI
```

## Why a harness, not just a CLI?

A coding CLI sends prompts and prints responses. A coding **harness** gives you the infrastructure to make AI agents reliable:

| | Coding CLI | altcode (Harness) |
|---|---|---|
| Agent loop | Run once | Multi-turn with verification gates + 50-iter cap |
| Context | Send and hope | Token tracking, auto-compact at 90%, LLM summarization |
| Tools | Call and pray | Permission system, pre/post hooks, auto-verify (go build) |
| Agents | Single model | Multi-agent: cc+opus, codex+gpt, altcode+minimax/glm/kimi |
| Tools | 10+ built-in | 15 tools incl. Agent (subagent spawn), TaskCreate/Update |
| Workflow | Ad-hoc | Interview → plan → persistent execution (ralph) |
| Quality | Trust output | Generator/evaluator loop, Codex adversarial review |
| Memory | Session-only | Cross-session persistent memory with MEMORY.md index |
| Providers | One vendor | 13 providers, any model, existing subscriptions |

## Quick start

```bash
# Install (macOS / Linux)
curl -fsSL https://altcode.io/install.sh | bash

# Log in with your ChatGPT subscription
altcode login codex

# Start coding — TUI with CC-parity display
altcode "fix the failing tests"

# Initialize project — auto-generate CLAUDE.md
altcode            # then type /init

# Multi-agent workspace — orchestrate claude + codex together
altcode workspace "build JWT auth" claude:architect codex:coder

# A/B test across models
altcode            # then type /compare review this code

# Full TUI mode — each agent in its own tmux pane
altcode workspace "implement payment flow" --tmux

# Use any Chinese AI coding plan
altcode --model minimax/MiniMax-M2.7 "add rate limiting"
altcode --model kimi/kimi-k2 "write tests"
altcode --model zhipu/glm-4.7 "review security"
```

**Zero config needed** — altcode auto-detects Claude Code, Codex CLI, and any other installed agents.

## Multi-Provider Agent Orchestration

altcode is the **universal orchestrator** — it calls any combination of agents, each using their own credentials:

```
altcode (main: GPT / MiniMax / GLM / Kimi)
  ├── Agent(claude)     → uses CC subscription (Opus)
  ├── Agent(codex)      → uses Codex subscription (GPT)
  ├── Agent(altcode, model=minimax/MiniMax-M2.7) → uses MiniMax API key
  ├── Agent(altcode, model=kimi/kimi-k2)         → uses Kimi coding plan
  └── Agent(altcode, model=zhipu/glm-4.7)        → uses GLM coding plan
```

Since Claude bans third-party agents from using CC's coding plan, altcode sidesteps this: **each subagent uses its own credentials**. The model decides when to spawn subagents based on task complexity — no manual setup needed.

```bash
# The model auto-spawns subagents for complex tasks
altcode "Review this code for security issues, test coverage, and performance"
# → spawns 3 Agent() calls in parallel, each with different backends

# Or pick agents explicitly in workspace mode
altcode workspace "build auth system" claude:architect codex:coder

# Add agents mid-run
/spawn reviewer altcode --model kimi/kimi-k2
```

**15 built-in tools**: Read, Write, Edit, Glob, Grep, Ls, Bash, Patch, WebFetch, WebSearch, Agent, TaskCreate, TaskUpdate, TaskList, TaskGet.

**20+ slash commands**: `/help` `/status` `/tools` `/init` `/doctor` `/compare` `/workspace` `/spawn` `/review` `/ship` `/evaluate` `/investigate` `/codex` `/cost` `/clear` `/compact` `/memory` `/diff` `/quit`

**12 keyboard shortcuts**: `Up/Down` history, `Ctrl+C` cancel/copy, `Ctrl+L` clear, `Ctrl+R` retry, `Ctrl+K` palette, `Ctrl+J` newline, `Ctrl+D` quit, `Tab` complete, `@` files, `Esc` cancel/vim, `PgUp/Down` scroll

## Multi-Agent Workspace

The workspace command orchestrates multiple agents working on the same codebase:

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

### Workspace commands

```bash
altcode workspace "task"                 # start workspace
altcode workspace "task" --tmux          # full TUI mode in tmux panes
altcode workspace "task" --agents codex  # choose specific backends
altcode workspace status                 # show all agent states
altcode workspace list                   # list workspaces
altcode workspace resume                 # re-attach to live agents
altcode workspace spawn reviewer         # add an agent mid-run
altcode workspace send architect "focus on JWT"  # message an agent
altcode workspace rollback --turn 3      # undo to turn 3
altcode workspace init                   # create .altcode/ config
```

### TUI mode (tmux panes)

The `--tmux` flag launches each agent in its own tmux pane with a real PTY, so full-TUI agents like Claude Code and Codex work with their native interfaces:

```bash
# Launch workspace with agents in tmux panes
altcode workspace "build auth" --tmux

# Mix headless and TUI agents
altcode workspace "build auth" --agents claude-code-tui,codex-exec --tmux
```

Two runtime modes:
- **processRuntime** (default) — headless pipe-based execution, stdout captured and streamed
- **TmuxRuntime** (`--tmux`) — each agent in a tmux split pane, real PTY, live observation

TUI agent definitions (`.altcode/agents/claude-code-tui.yaml`, `codex-tui.yaml`) set `tui: true` to signal they need a real terminal.

### Universal agent harness

Any CLI agent can be registered via YAML — no Go code needed:

```yaml
# .altcode/agents/my-agent.yaml
name: my-agent
binary: /path/to/my-agent
args: ["--headless", "--auto-approve"]
task_flag: "--task"
worktree: true
detect: "my-agent --version"
```

Ships with 8 agent definitions: `claude-code`, `codex-exec`, `codex-tui`, `claude-code-tui`, `altcode`, `opencode`, `openclaw`, `aider`.

### TUI dashboard

The workspace TUI shows each agent in its own pane with live output:

```
[workspace:01hv3k] build auth | working | pr:2 ci:pass | [architect ✓] → [implementer ⟳]
╭──── ARCHITECT (claude) [done] ────╮ ╭──── IMPLEMENTER (codex) [active] ──╮
│ turns:3 $0.15 PR#42 CI:pass       │ │ turns:7 $0.32 PR#43 CI:pass        │
│ ⎇ altcode/01hv/architect/auth      │ │ ⎇ altcode/01hv/implementer/auth     │
│ Designed JWT token flow...         │ │ Writing /api/login endpoint...      │
│ Created middleware spec            │ │ Running tests... ok                 │
╰────────────────────────────────────╯ ╰─────────────────────────────────────╯
  Ctrl+Z pause  Ctrl+Q abort  Ctrl+S send  Tab cycle
```

Keys: `Ctrl+Z` pause, `Ctrl+R` resume, `Ctrl+Q` abort, `Ctrl+S` send, `Tab` cycle, `Ctrl+1/2/3` focus, mouse click to focus.

### TUI features (Claude Code parity)

```
┃ ❯  Review internal/tui/ for bugs
├─ ✓ TaskCreate(TaskCreate) 2s
├─ ✓ glob(ls internal/tui (39 entries)) <1s
├─ ⟳ grep Running… (4s · timeout 2m)
├─ ⟳ read Running… (2s · timeout 2m)
└─ ⟳ bash
✶ Deliberating… (13s · ↓ 1.2K tokens · thinking with max effort)
  ⎿  Analyzing function lengths and race conditions...
  ⎿  Esc cancel · Ctrl+R retry · Ctrl+K commands

[gpt-5.4] │ altcode git:(main*) │ merry-singing-dawn │ thinking (14s) │ ✓ TaskCreate ×1 ✓ Ls ×1 │ 16s
[░░░░░░░░░░░░░░░░] 2% 1.2K/1.0M │ 2 CLAUDE.md | 4 MCPs │ $0.03
▸ Reviewing codebase (1/3)
```

| Feature | Description |
|---------|-------------|
| `[Model]` badge | Model name in brackets |
| `git:(branch*)` | Git branch with dirty indicator |
| Session slug | Random name per session (e.g. `merry-singing-dawn`) |
| `✶ Verb…` | Rotating thinking indicator with per-turn token count |
| Thinking preview | First line of model reasoning visible live |
| `Name(target) Ns` | CC-style tool display with smart basename truncation |
| `Running… (Ns · timeout 2m)` | Live elapsed + timeout on running tools |
| `⎿` output lines | Diff coloring (+green/-red), collapsed `… +N lines (ctrl+o to expand)` |
| `▸ task (n/m)` | Live task progress in HUD |
| `✓ Tool ×N` | Completed tool counts (sorted by frequency, top 4) |
| Config counts | `N CLAUDE.md \| N MCPs \| N hooks` |
| Turn summary | `✓ 2 files changed · 1 command · $0.03 · 1.2K tokens · 20s` |
| `Up/Down` | Input history — recall previous prompts |
| `Ctrl+C` | Cancel (busy) / copy last response (idle) |
| `Ctrl+L` | Clear screen |
| `Ctrl+R` | Retry last prompt |
| File sidebar | Tracks changed files with +/-N counts |
| Focused pane | `▸` marker + bright border on focused workspace pane |
| No flicker | Viewport stays stable during tool execution |

### Workflow definitions

Define multi-phase workflows in `.altcode/workflows/*.yaml`:

```yaml
# .altcode/workflows/ship-feature.yaml
name: ship-feature
phases:
  - name: design
    agents:
      - role: architect
        backend: claude
        prompt: "Design the implementation for: {{.Task}}"
  - name: implement
    depends_on: [design]
    agents:
      - role: implementer
        backend: codex
        prompt: "Implement the feature: {{.Task}}"
  - name: review
    depends_on: [implement]
    parallel: true
    agents:
      - role: reviewer
        backend: claude
      - role: challenger
        backend: codex
```

Ships with 3 workflows: `ship-feature` (design/implement/review), `review` (parallel), `fix` (diagnose/fix/verify).

### Agent-to-agent messaging

Agents communicate via a shared mailbox:

```bash
# Send a message to a specific agent
altcode workspace send architect "focus on JWT token flow"

# From TUI: Ctrl+S pre-fills the send command
/send implementer "use the architect's JWT design from PR #42"
```

Messages are targeted (role-specific) or broadcast. The mailbox persists to disk for resume support.

## Harness architecture

```
cmd/altcode/         CLI entry point (login, workflow, workspace, team, sessions)
internal/
├── workspace/       Workspace orchestration — session, store, worktrees, task list,
│   ├── backends/    8 agent backends (claude, codex, opencode, aider, openclaw,
│   │                altcode, universal YAML, TmuxRuntime for full-TUI agents)
│   ├── tasklist     Shared task list with dependencies + flock-guarded claiming
│   ├── mailbox      Agent-to-agent messaging (targeted + broadcast)
│   └── eventlog     Append-only JSONL event log (brain/hands/session decoupling)
├── lifecycle/       State machine — 12 states, CI auto-fix loop, review routing,
│                    stuck detection, turn checkpoints, auto-merge
├── scm/             GitHub integration — REST API with token, PR/CI/review lifecycle,
│                    rate-limit aware, cross-domain redirect protection
├── wsctl/           Context injection — provider-aware (CLAUDE.md/AGENTS.md),
│                    secret guard (7 patterns), 32KB context cap
├── engine/          Agent loop — tool dispatch, permission checks, hooks, auto-compact
├── provider/        13 providers — Anthropic, OpenAI, altllm, DeepSeek, GLM, Kimi,
│                    MiniMax, Qwen, Ollama, LMStudio, OpenRouter + any OpenAI-compat
├── compact/         Context management — LLM summarization, budget compaction,
│                    microcompact, pre-turn proactive compaction, retry with trimming
├── agent/           Multi-agent — mailbox IPC, history forking, roles, registry,
│                    depth limits, token budget sharing, team orchestration
├── workflow/        Structured workflows — interview (Socratic), plan (consensus),
│                    ralph (persistent execution), keyword routing, state lifecycle
├── tool/            15 built-in tools — Agent, TaskCreate/Update/List/Get, Read, Write,
│                    Edit, Glob, Grep, Ls, Bash, Patch, WebFetch, WebSearch
├── hooks/           13 events — command, prompt, and HTTP webhook hooks
├── oauth/           Native login — OAuth PKCE + device code flow for ChatGPT sub
├── mcp/             MCP client — stdio + SSE, auto-discovery, namespace isolation
├── command/         Skills — 47 discovered from .claude/skills/ + .agents/skills/
├── plugin/          Plugins — loads .altcode-plugin/ and .claude-plugin/ formats
├── memory/          Persistent memory — cross-session knowledge, MEMORY.md index
├── permission/      4 modes (default/auto/bypass/plan) with glob rules
├── tui/             Terminal UI — workspace dashboard, agent panes, HUD, tool tree,
│                    split panes, attention colors, phase breadcrumb, 6 themes
├── config/          JSONC config, env expansion, instruction cascade
├── store/           SQLite sessions with --last resume
├── exec/            Headless mode with banner, progress, cost tracking
└── event/           Typed events (engine ↔ TUI ↔ exec)
```

## Lifecycle state machine

Each agent progresses through a 12-state lifecycle with automatic transitions:

```
spawning → working → pr_open → ci_checking ──→ approved → mergeable → merged → cleanup → done
                                    │                ↑
                                    ↓                │
                               ci_failed ───→ changes_requested
                              (auto-fix ×3)    (review routing)
```

- **CI auto-fix** — on CI failure, the failure log is routed to the agent for repair (up to 3 retries)
- **Review routing** — PR review comments are extracted and sent to the agent as a follow-up task
- **Turn checkpoints** — git commit per agent turn with JSON metadata for rollback
- **Stuck detection** — agents idle >5 minutes get AttentionRed priority
- **Auto-merge** — when CI passes and reviews approve, the PR is merged automatically

## GitHub integration

altcode connects to GitHub via direct REST API — no `gh` CLI required:

```bash
# Token discovery (in order):
# 1. GITHUB_TOKEN env var
# 2. GH_TOKEN env var
# 3. gh auth token (if gh is installed)
```

Features: PR creation/merge, CI status polling, review feedback extraction, rate-limit aware requests, cross-domain redirect protection, HTTPS enforcement.

## The three pillars

altcode is built on three harness engineering principles (inspired by [OpenHarness](https://github.com/HKUDS/OpenHarness)):

### 1. Context engineering
The codebase IS the context. Everything the agent needs is discoverable:
- **CLAUDE.md** → project conventions, build commands, hard rules
- **Skills** → 47 slash commands with progressive disclosure (read SKILL.md on demand)
- **Memory** → persistent cross-session knowledge in markdown files
- **MCP** → tools and resources from external servers

### 2. Architectural constraints
Constraining the solution space makes agents more productive:
- **Permission system** — 4 modes with glob rules prevent destructive actions
- **Hooks** — 13 lifecycle events with command, prompt, and HTTP handlers
- **Auto-verify** — `go build` runs automatically after every `.go` edit
- **Tool restrictions** — subagents get a limited tool set

### 3. Entropy management
AI-generated code decays fast. The harness fights entropy:
- **Context compaction** — LLM summarization preserves key decisions, trims noise
- **Pre-turn compaction** — proactively compact at 90% context window before overflow
- **Generator/evaluator loop** — separate agent grades work against criteria
- **Workflow mode** — interview clarifies intent before coding starts

## Provider support

| Provider | Prefix | Auth |
|----------|--------|------|
| **Claude** (Anthropic) | `anthropic/` | Claude Code sub, `ANTHROPIC_API_KEY` |
| **GPT** (OpenAI/Codex) | `openai/` | `altcode login codex`, Codex sub, `OPENAI_API_KEY` |
| **altllm** | `altllm/` | `ALTLLM` env var |
| **DeepSeek** | `deepseek/` | `DEEPSEEK_API_KEY` |
| **GLM** (Zhipu) | `zhipu/` | `ZHIPU_API_KEY` |
| **Kimi** (Moonshot) | `moonshot/` | `MOONSHOT_API_KEY` |
| **MiniMax** | `minimax/` | `MINIMAX_API_KEY` |
| **Qwen** (Alibaba) | `qwen/` | `DASHSCOPE_API_KEY` |
| **Ollama** | `ollama/` | Local, no key |
| **LM Studio** | `lmstudio/` | Local, no key |
| **OpenRouter** | `openai/` | `OPENROUTER` (100+ models) |
| Any OpenAI-compat | any prefix | Falls back to configured `openai` provider |

```bash
altcode --model deepseek/deepseek-chat "fix this"
altcode --model altllm/altllm-basic "add tests"
altcode --model ollama/llama3 "explain this error"
```

### Coding plans (MiniMax / GLM / Kimi)

Chinese AI providers offer subscription-based coding plans with Anthropic-compatible APIs. altcode auto-detects the protocol from the configured `baseURL`:

```jsonc
// .altcode/config.json — MiniMax coding plan
{
  "model": "minimax/MiniMax-M2.7",
  "provider": {
    "minimax": {
      "apiKey": "$MINIMAX_API_KEY",
      "baseURL": "https://api.minimax.io/anthropic"  // Anthropic-compat
    }
  }
}
```

```jsonc
// GLM coding plan (Zhipu)
{
  "model": "zhipu/glm-4.7",
  "provider": {
    "zhipu": {
      "apiKey": "$ZHIPU_API_KEY",
      "baseURL": "https://api.z.ai/api/anthropic"
    }
  }
}
```

```jsonc
// Kimi coding plan (Moonshot)
{
  "model": "kimi/kimi-k2",
  "provider": {
    "moonshot": {
      "apiKey": "$MOONSHOT_API_KEY",
      "baseURL": "https://api.kimi.com/coding/"
    }
  }
}
```

When `baseURL` contains `/anthropic` or `/coding`, altcode automatically uses the Anthropic API protocol (tool use, streaming). Otherwise it uses the OpenAI-compatible endpoint. No extra flags needed.

## Workflow mode

Optional structured pipeline for complex tasks:

```bash
# Socratic clarification — asks questions before coding
altcode workflow --mode interview "add rate limiting"

# Consensus planning — produces a plan, doesn't execute
altcode workflow --mode plan "redesign the config system"

# Persistent execution — loops until verified complete
altcode workflow --mode ralph "implement the full auth flow"

# Auto-detect from keywords
altcode workflow "$ralph fix all failing tests"
altcode workflow "clarify what the hooks system does"
```

## Benchmarks

### 7-model benchmark (identical coding task)

| Model | Tests passing | Bugs fixed | Methods added |
|-------|:------------:|:----------:|:-------------:|
| Claude Code | **11** | 3/3 | 5/5 |
| altcode+GPT | 8 | 3/3 | 5/5 |
| altcode+altllm | 9 | 3/3 | 5/5 |
| altcode+DeepSeek | 6 | 3/3 | 5/5 |
| altcode+Kimi | 8 | 3/3 | 5/5 |
| altcode+Qwen | 8 | 3/3 | 5/5 |
| Codex CLI | 7 | 3/3 | 5/5 |

All 7 models produce correct, race-safe Go code through the same harness. See [full benchmark](docs/benchmark-multi-model.md).

### Three-way comparison

| Task | Claude Code | altcode+GPT | Codex CLI |
|------|:-:|:-:|:-:|
| HumanEval (6 tests) | 6/6 | 6/6 | 6/6 |
| ConnPool bugs + tests | 4 tests | 3 tests | 3 tests |
| wordcount CLI + tests | 9 tests | 4 tests | 5 tests |
| **Correctness** | 100% | 100% | 100% |

See [three-way benchmark](docs/benchmark-three-way.md).

## Install

**Quick install (macOS / Linux):**
```bash
curl -fsSL https://altcode.io/install.sh | bash
```

**Go install:**
```bash
go install github.com/altcode-ai/altcode/cmd/altcode@latest
```

**Build from source:**
```bash
git clone https://github.com/jiayaoqijia/altcode.git
cd altcode && make build
sudo cp dist/altcode /usr/local/bin/
```

**Pre-built binaries** from [Releases](https://github.com/jiayaoqijia/altcode/releases):

| Platform | Binary | Size |
|----------|--------|:----:|
| macOS Apple Silicon | `altcode-darwin-arm64` | 16MB |
| macOS Intel | `altcode-darwin-amd64` | 17MB |
| Linux x86_64 | `altcode-linux-amd64` | 17MB |
| Linux ARM64 | `altcode-linux-arm64` | 16MB |
| Windows x64 | `altcode-windows-amd64.exe` | 17MB |

```bash
# Manual download (Linux x86_64 example)
curl -L https://github.com/jiayaoqijia/altcode/releases/latest/download/altcode-linux-amd64 -o altcode
chmod +x altcode && sudo mv altcode /usr/local/bin/
```

No runtime dependencies. No Node.js. No Python. Single binary.

## Configuration

```jsonc
{
  "model": "openai/gpt-5.4",
  "context_window": 1000000,    // override per-model (auto-detected from API)
  "compact_threshold": 700000,  // trigger auto-compact at this token count
  "provider": {
    "openai": { "apiKey": "$OPENAI_API_KEY" },
    "anthropic": { "apiKey": "$ANTHROPIC_API_KEY" },
    "altllm": { "apiKey": "$ALTLLM" },
    "deepseek": { "apiKey": "$DEEPSEEK_API_KEY" }
  },
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash",
      "hooks": [{ "type": "http", "url": "https://hooks.example.com/pre-tool" }]
    }]
  }
}
```

## Claude Code compatibility

altcode natively loads the entire Claude Code ecosystem:
- **CLAUDE.md** — project instructions
- **.mcp.json** — MCP server configurations
- **.claude/settings.json** — permissions and hooks
- **.claude/skills/** — 47 skill definitions with YAML frontmatter
- **.agents/skills/** — agent skill definitions
- **.claude-plugin/** — plugin directories with commands, agents, hooks

## Development

```bash
make build     # Build binary (dist/altcode)
make test      # Run 400+ tests with race detector
make lint      # Run go vet

# TUI visual testing
./scripts/tui-e2e-test.sh ./dist/altcode

# Cross-compile
GOOS=darwin GOARCH=arm64 GOFLAGS=-mod=mod go build -o altcode-mac ./cmd/altcode
```

## Links

- **Website**: [altcode.io](https://altcode.io)
- **GitHub**: [github.com/jiayaoqijia/altcode](https://github.com/jiayaoqijia/altcode)
- **Releases**: [Download binaries](https://github.com/jiayaoqijia/altcode/releases)
- **Benchmarks**: [7-model](docs/benchmark-multi-model.md) · [3-way](docs/benchmark-three-way.md) · [altllm](docs/benchmark-altllm.md)

## License

See [LICENSE](LICENSE).
