# altcode

**[altcode.io](https://altcode.io)** · [Install](#install) · [Docs](#harness-architecture) · [Benchmarks](#benchmarks) · [Releases](https://github.com/jiayaoqijia/altcode/releases)

**The open agent harness for coding.** One binary. Any model. Production-grade infrastructure for AI-assisted development.

```
5ms startup · 10MB binary · 13 providers · 100+ models · 400+ tests
```

## Why a harness, not just a CLI?

A coding CLI sends prompts and prints responses. A coding **harness** gives you the infrastructure to make AI agents reliable:

| | Coding CLI | altcode (Harness) |
|---|---|---|
| Agent loop | Run once | Multi-turn with verification gates + 50-iter cap |
| Context | Send and hope | Token tracking, auto-compact at 90%, LLM summarization |
| Tools | Call and pray | Permission system, pre/post hooks, auto-verify (go build) |
| Agents | Single model | Multi-agent with mailbox IPC, roles, depth limits, history forking |
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

# Start coding
altcode "fix the failing tests"

# Or use workflow mode for complex tasks
altcode workflow --mode ralph "implement the auth system"
```

**Zero config needed** if you already use Claude Code or Codex CLI — altcode auto-detects your credentials.

## Harness architecture

```
cmd/altcode/         CLI entry point (login, workflow, team, sessions)
internal/
├── engine/          Agent loop — tool dispatch, permission checks, hooks, auto-compact
├── provider/        13 providers — Anthropic, OpenAI, altllm, DeepSeek, GLM, Kimi,
│                    MiniMax, Qwen, Ollama, LMStudio, OpenRouter + any OpenAI-compat
├── compact/         Context management — LLM summarization, budget compaction,
│                    microcompact, pre-turn proactive compaction, retry with trimming
├── agent/           Multi-agent — mailbox IPC, history forking, roles, registry,
│                    depth limits, token budget sharing, team orchestration
├── workflow/        Structured workflows — interview (Socratic), plan (consensus),
│                    ralph (persistent execution), keyword routing, state lifecycle
├── tool/            10 built-in tools — concurrent dispatch, auto-verify after edits
├── hooks/           13 events — command, prompt, and HTTP webhook hooks
├── oauth/           Native login — OAuth PKCE + device code flow for ChatGPT sub
├── mcp/             MCP client — stdio + SSE, auto-discovery, namespace isolation
├── command/         Skills — 47 discovered from .claude/skills/ + .agents/skills/
├── plugin/          Plugins — loads .altcode-plugin/ and .claude-plugin/ formats
├── memory/          Persistent memory — cross-session knowledge, MEMORY.md index
├── permission/      4 modes (default/auto/bypass/plan) with glob rules
├── tui/             Terminal UI — HUD, tool tree, split panes, colored messages,
│                    vim keys, command palette, inline diffs, 6 themes
├── config/          JSONC config, env expansion, instruction cascade
├── store/           SQLite sessions with --last resume
├── exec/            Headless mode with banner, progress, cost tracking
└── event/           Typed events (engine ↔ TUI ↔ exec)
```

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

**macOS / Linux:**
```bash
curl -fsSL https://altcode.io/install.sh | bash
```

**Build from source:**
```bash
git clone git@github.com:jiayaoqijia/altcode.git
cd altcode && make build
sudo cp dist/altcode /usr/local/bin/
```

**Pre-built binaries** from [Releases](https://github.com/jiayaoqijia/altcode/releases):

| Platform | Size |
|----------|:----:|
| macOS Apple Silicon | 16MB |
| macOS Intel | 17MB |
| Linux x86_64 | 17MB |
| Linux ARM64 | 16MB |
| Windows x64 | 17MB |

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
