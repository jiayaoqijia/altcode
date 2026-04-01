# altcode

A minimal, blazing-fast Go CLI/TUI for AI-assisted coding.

**5ms startup. 8MB binary. Full agent loop with tool dispatch.**

## Features

- **Multi-turn agent loop** — model calls tools, gets results, continues until done
- **7 built-in tools** — read, glob, grep, ls, bash, edit, write
- **Concurrent tool dispatch** — read-only tools run in parallel
- **Permission system** — 4 modes (default/auto/bypass/plan) with glob rules and doom loop detection
- **SQLite session persistence** — conversations survive between invocations
- **Streaming TUI** — Bubbletea-based with markdown rendering, themes, status bar
- **Context compaction** — budget-based truncation and microcompaction
- **Anthropic provider** — SSE streaming with retry and exponential backoff

## Quickstart

```bash
# Build
make build

# Run (requires ANTHROPIC_API_KEY)
export ANTHROPIC_API_KEY=sk-...
./dist/altcode

# Or with model override
./dist/altcode --model anthropic/claude-sonnet-4-20250514
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
├── hooks/           Hook system (planned)
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

See `docs/plans/` for detailed gap analyses and implementation plans:

- **Phase A** (complete): ContentPart messages, agent loop, session persistence
- **Phase B** (planned): Exec mode, session resume
- **Phase C** (planned): Hooks, slash commands, plugins
- **Phase D** (planned): Multi-provider (OpenAI, Ollama), MCP client

## Development

```bash
make build     # Build binary
make test      # Run tests with race detector
make lint      # Run go vet
make clean     # Remove build artifacts
```

## License

See [LICENSE](LICENSE).
