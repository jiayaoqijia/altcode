# altcode

A minimal, blazing-fast Go CLI/TUI for AI-assisted coding.

**5ms startup. 8MB binary. 4 providers. 412 tests. Claude Code compatible.**

## Why altcode?

altcode is a **multi-provider** alternative to Claude Code CLI that runs the **same plugins, hooks, commands, and agents** across Anthropic, OpenAI/Codex, Ollama, and LMStudio. It's 40x faster to start, 6x smaller, and fully open source.

| | Claude Code CLI | altcode |
|---|:-:|:-:|
| Providers | Anthropic only | **4** (Anthropic, OpenAI, Ollama, LMStudio) |
| Startup | ~200ms | **5ms** (40x faster) |
| Binary | ~50MB | **8MB** (6x smaller) |
| Hooks | ~10 events | **13 events** |
| Plugins | Claude Code format | **Loads Claude Code plugins natively** |
| Tests | Closed source | **412 tests** (mock + live) |
| Source | Plugins only | **Fully open** |

## Features

- **Multi-turn agent loop** — model calls tools, gets results, continues until done (50-iteration cap)
- **Multi-provider** — Anthropic, OpenAI/Codex, Ollama, LMStudio, any OpenAI-compatible API
- **7 built-in tools** — read, glob, grep, ls, bash, edit, write
- **MCP client** — stdio + SSE/HTTP transports, auto-discovery, namespace isolation
- **Hooks system** — 13 events: PreToolUse, PostToolUse, Stop, SessionStart/End, UserPromptSubmit, SubagentStop, PreCompact, Notification, CwdChanged, FileChanged, TaskCreated, PermissionDenied
- **Slash commands** — markdown files with frontmatter, `!backtick` expansion, `$ARGUMENTS`, allowed-tools
- **Plugin system** — loads both `.altcode-plugin/` and `.claude-plugin/` formats, marketplace support
- **Subagent system** — spawn restricted child engines with tool subset, model override, depth limits
- **Persistent memory** — cross-session knowledge in markdown files with MEMORY.md index
- **Exec mode** — `altcode "prompt"` for headless use, `--json` for JSONL events
- **Session persistence** — SQLite storage with `--last` resume and `sessions` subcommand
- **Permission system** — 4 modes with glob rules, doom loop detection, 13 hook events
- **Streaming TUI** — Bubbletea-based with markdown rendering, themes, status bar
- **Claude Code compatible** — loads CLAUDE.md, plugins, hooks, commands, agents, and memory natively
- **Rich system prompt** — behavioral instructions borrowed from Claude Code's prompt engineering

## Install

**From source (recommended):**
```bash
curl -fsSL https://raw.githubusercontent.com/jiayaoqijia/altcode/main/scripts/install.sh | bash
```

**Manual build:**
```bash
git clone https://github.com/jiayaoqijia/altcode.git
cd altcode
make build
sudo cp dist/altcode /usr/local/bin/
```

**Go install:**
```bash
go install github.com/altcode-ai/altcode/cmd/altcode@latest
```

**Requirements:** Go 1.23+ (for building from source)

## Get Started

1. **Zero configuration** — altcode auto-detects your existing subscriptions:

    | If you have... | altcode uses it automatically |
    |----------------|------------------------------|
    | Claude Code CLI (`~/.claude/.credentials.json`) | Claude subscription (Max/Pro) |
    | Codex CLI (`~/.codex/auth.json`) | Codex subscription + relay URL |
    | `ANTHROPIC_API_KEY` env var | Anthropic API key |
    | `OPENAI_API_KEY` env var | OpenAI API key |

    **No setup needed if you already use Claude Code or Codex CLI.**

    For local models (no key needed):
    ```bash
    ollama serve &  # install from https://ollama.ai
    ```

2. Run altcode:

    ```bash
    # Interactive TUI
    altcode

    # With model override
    altcode --model openai/gpt-4
    altcode --model ollama/llama3

    # Headless (for scripts/CI)
    altcode "explain this error"
    altcode --json "list files"

    # Resume previous session
    altcode --last

    # List sessions
    altcode sessions
    ```

3. (Optional) Install Claude Code plugins:

    ```bash
    # Clone Claude Code plugins into your project
    git clone https://github.com/anthropics/claude-code.git /tmp/claude-code
    cp -r /tmp/claude-code/plugins ~/.config/altcode/plugins/
    ```

    Plugins are auto-discovered from `~/.config/altcode/plugins/` and `.altcode/plugins/`.

## Architecture

```
cmd/altcode/         Entry point (Cobra CLI)
internal/
├── engine/          Agent loop with tool dispatch + session persistence
├── provider/        Provider interface + Anthropic SSE + OpenAI SSE
├── tool/            Tool interface, registry, concurrent dispatch
├── permission/      Permission evaluator with modes and rules
├── hooks/           Hook system (13 events, command handlers)
├── mcp/             MCP client (stdio + SSE transports)
├── command/         Slash commands (markdown with frontmatter)
├── plugin/          Plugin discovery, loading, and marketplace
├── agent/           Subagent definitions, spawn, and registry
├── memory/          Persistent cross-session memory
├── store/           SQLite storage (sessions + messages)
├── config/          JSONC config, env expansion, instruction cascade
├── compact/         Context compaction (budget + microcompact)
├── exec/            Headless execution mode
├── tui/             Bubbletea TUI (markdown, header, status, palette)
├── sysctl/          System prompt assembly
└── event/           Event types for engine ↔ TUI communication
```

## Configuration

```jsonc
{
  "model": "anthropic/claude-sonnet-4-20250514",
  "provider": {
    "anthropic": { "apiKey": "$ANTHROPIC_API_KEY" },
    "openai": { "apiKey": "$OPENAI_API_KEY", "baseURL": "https://api.openai.com" }
  },
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "validate.sh" }] }
    ]
  },
  "theme": "catppuccin-mocha"
}
```

## Claude Code Compatibility

altcode natively loads Claude Code plugins from `.claude-plugin/` directories:

```bash
# All 12 official Claude Code plugins work
vendor/claude-code/plugins/
├── commit-commands/     # 3 commands ✓
├── feature-dev/         # 1 command + 3 agents ✓
├── pr-review-toolkit/   # 1 command + 6 agents ✓
├── security-guidance/   # 1 hook ✓
├── hookify/             # 4 commands + 1 agent + 4 hooks ✓
└── ...                  # 7 more plugins ✓
```

Instructions loaded from: `CLAUDE.md`, `AGENTS.md`, `ALTCODE.md`, `.altcode/rules/*.md`

## Development

```bash
make build     # Build binary
make test      # Run tests with race detector
make lint      # Run go vet
make clean     # Remove build artifacts

# Run with both providers
GOFLAGS=-mod=mod go test ./... -race -count=1 -timeout=120s -parallel=8
```

## License

See [LICENSE](LICENSE).
