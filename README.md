# altcode

A minimal, blazing-fast Go CLI/TUI for AI-assisted coding.

**5ms startup. 8MB binary. Full agent loop with tool dispatch.**

## Features

- **Multi-turn agent loop** — model calls tools, gets results, continues until done (50-iteration cap)
- **Multi-provider** — Anthropic, OpenAI/Codex, Ollama, LMStudio, any OpenAI-compatible API
- **7 built-in tools** — read, glob, grep, ls, bash, edit, write
- **MCP client** — connect to external tool servers via JSON-RPC 2.0 over stdio
- **Hooks system** — PreToolUse/PostToolUse/Stop hooks with command handlers (Claude Code compatible)
- **Slash commands** — markdown files with frontmatter, `!backtick` expansion, `$ARGUMENTS`
- **Plugin system** — discover and load plugins with commands, hooks, and MCP configs
- **Exec mode** — `altcode "prompt"` for headless use, `--json` for JSONL events
- **Session persistence** — SQLite storage with `--last` resume and `sessions` subcommand
- **Permission system** — 4 modes with glob rules, doom loop detection
- **Streaming TUI** — Bubbletea-based with markdown rendering, themes, status bar
- **Claude Code compatible** — loads CLAUDE.md instructions, plugins, hooks, and commands natively
- **Rich system prompt** — behavioral instructions borrowed from Claude Code's prompt engineering

## Quickstart

```bash
# Build
make build

# Run (requires ANTHROPIC_API_KEY)
export ANTHROPIC_API_KEY=sk-...
./dist/altcode

# Or with model override
./dist/altcode --model anthropic/claude-sonnet-4-20250514

# Use with OpenAI/Codex
export OPENAI_API_KEY=sk-...
./dist/altcode --model openai/gpt-4

# Use with local Ollama
./dist/altcode --model ollama/llama3

# Exec mode (headless)
./dist/altcode "explain this error"
./dist/altcode --json "list files"  # JSONL output

# Resume last session
./dist/altcode --last
```

## Architecture

```
cmd/altcode/         Entry point (Cobra CLI)
internal/
├── engine/          Agent loop with tool dispatch + session persistence
├── provider/        Provider interface + Anthropic SSE streaming
├── tool/            Tool interface, registry, concurrent dispatch
├── permission/      Permission evaluator with modes and rules
├── store/           SQLite storage (sessions + messages)
├── config/          JSONC config with env expansion + instruction cascade
├── compact/         Context compaction (budget + microcompact)
├── hooks/           Hook system (PreToolUse/PostToolUse/Stop)
├── mcp/             MCP client (JSON-RPC 2.0 over stdio)
├── command/         Slash commands (markdown with frontmatter)
├── plugin/          Plugin discovery and loading
├── exec/            Headless execution mode
├── tui/             Bubbletea TUI (markdown, header, status, palette)
├── sysctl/          System prompt assembly
└── event/           Event types for engine ↔ TUI communication
```

## Configuration

Config files in JSONC (JSON with comments):
- `~/.config/altcode/config.json` — user config
- `.altcode/config.json` — project config
- CLI flags override both

```jsonc
{
  "model": "anthropic/claude-sonnet-4-20250514",
  "provider": {
    "anthropic": {
      "apiKey": "$ANTHROPIC_API_KEY"
    }
  },
  "theme": "catppuccin-mocha"
}
```

## Instructions

Instruction files loaded in cascade order:
1. `~/.config/altcode/instructions.md`
2. `CLAUDE.md`
3. `AGENTS.md`
4. `ALTCODE.md`
5. `.altcode/rules/*.md`

## Roadmap

All core phases complete. See `docs/plans/` for gap analyses.

- **Phase A** (complete): ContentPart messages, agent loop, session persistence
- **Phase B** (complete): Exec mode, session resume
- **Phase C** (complete): Hooks, slash commands, plugins
- **Phase D** (complete): Multi-provider (OpenAI, Ollama, LMStudio), MCP client
- **Phase E** (planned): Subagent system, skill loading, output capping, sandbox

## Development

```bash
make build     # Build binary
make test      # Run tests with race detector
make lint      # Run go vet
make clean     # Remove build artifacts
```

## License

See [LICENSE](LICENSE).
