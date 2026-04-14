# TUI Feature Test Plan — altcode vs Claude Code vs Codex CLI

## Feature Comparison

| Feature | CC | Codex | altcode | Test Status |
|---------|:--:|:-----:|:-------:|:-----------:|
| **Display** |
| Thinking indicator + token count | ✓ | ✓ | ✓ | tmux #1 |
| Tool tree (running/done/duration) | ✓ | ✓ | ✓ | tmux #5 |
| HUD bar (model, git, cost, ctx) | ✓ | ~ | ✓ | tmux #2 |
| File sidebar (+/- counts) | ✓ | ✗ | ✓ | Go unit |
| Diff coloring (+green/-red) | ✓ | ✓ | ✓ | Go unit |
| Narrow terminal (<60 cols) | ✓ | ✓ | ✓ | tmux #18,19 |
| **Input** |
| Prompt history (Up/Down) | ✓ | ✓ | ✓ | tmux #16 |
| Tab completion | ✓ | ✓ | ✓ | Go unit |
| Vim mode (Esc toggle) | ✓ | ~ | ✓ | tmux #17 |
| Multiline input (Ctrl+J) | ✓ | ✓ | ✓ | Go unit |
| @file completion | ✓ | ✗ | ✓ | tmux #20 |
| External editor (Ctrl+G) | ✓ | ✓ | ✓ | — |
| **Commands (15+)** |
| /help | ✓ | ✓ | ✓ | tmux #1 |
| /status | ✓ | ✓ | ✓ | tmux #2 |
| /tools | ✓ | ✓ | ✓ | tmux #3 |
| /doctor | ✓ | ✗ | ✓ | tmux #4 |
| /cost | ✓ | ✗ | ✓ | tmux #5 |
| /clear | ✓ | ✓ | ✓ | tmux #6 |
| /compact | ✓ | ✗ | ✓ | — |
| /memory | ✓ | ✗ | ✓ | tmux #7 |
| /diff | ✓ | ✗ | ✓ | tmux #8 |
| /skills | ✓ | ✗ | ✓ | tmux #9 |
| /mcp | ✓ | ✗ | ✓ | tmux #10 |
| /plugins | ✓ | ✗ | ✓ | tmux #11 |
| /backends | ✗ | ✗ | ✓ | tmux #12 |
| /workspace | ✗ | ✗ | ✓ | — |
| /compare | ✗ | ✗ | ✓ | — |
| **Navigation** |
| Session switcher (Ctrl+A) | ✓ | ✓ | ✓ | — |
| Command palette (Ctrl+K) | ✓ | ~ | ✓ | tmux #17 |
| Scroll (PgUp/PgDown) | ✓ | ✓ | ✓ | — |
| **Multi-Agent** |
| Multi-backend workspace | ✗ | ✗ | ✓ | Go unit |
| Agent focus (Ctrl+1/2/3) | ✗ | ✗ | ✓ | — |
| Pause/resume (Ctrl+Z/R) | ✗ | ✗ | ✓ | — |
| Task tracking | ✓ | ✗ | ✓ | — |
| **Config** |
| 6+ themes | ✗ | ✗ | ✓ | — |
| Dark mode | ✓ | ✓ | ✓ | — |
| **Integration** |
| MCP servers | ✓ | ✓ | ✓ | tmux #10 |
| Plugin system | ✓ | ✓ | ✓ | tmux #11 |
| Hook system (13 events) | ✓ | ~ | ✓ | — |
| **Output** |
| Markdown rendering | ✓ | ✓ | ✓ | Go unit |
| Image support | ✓ | ✓ | ✗ | MISSING |

## altcode Advantages (features CC/Codex DON'T have)

1. **Multi-backend workspace** — Claude + Codex + any CLI agent in parallel
2. **Agent focus switching** — Ctrl+1/2/3 to switch between agents
3. **Pause/resume workflows** — Ctrl+Z to pause, Ctrl+R to resume
4. **/backends command** — shows detected CLI backends
5. **/workspace command** — multi-agent orchestration
6. **/compare command** — side-by-side multi-model answers
7. **6 color themes** — Catppuccin, Dracula, Nord, Tokyo Night, Solarized, default
8. **13 providers** — any model from any vendor via single CLI
9. **8 agent backends** — claude, codex, aider, opencode, openclaw, altcode, universal YAML

## Missing Features (CC/Codex have, altcode doesn't)

1. **Image display in terminal** — CC supports inline image rendering
2. **JSON schema validation** — CC validates structured output

## Test Matrix

### Level 1: Go Unit Tests (74 passing)
- Template rendering, view components, key handlers
- Workspace view, agent pane, tooltree, markdown
- Concurrent render + update race tests
- Narrow/wide terminal edge cases

### Level 2: tmux E2E Tests (20 passing)
- All slash commands produce expected output
- HUD shows model + git branch
- Command palette opens with Ctrl+K
- Vim mode toggle with Esc
- Narrow terminal (60x15, 30x8) renders without crash
- /quit exits cleanly

### Level 3: Live Agent Tests (manual)
- Submit prompt → thinking indicator → tool calls → response
- Multi-tool concurrent execution → tree shows parallel progress
- Permission dialog → allow/deny → agent continues/stops
- Ctrl+C cancels mid-execution → spinner stops, input returns
- /compact mid-session → message count reduces
- Workspace mode → agents spawn → panes render

## Corner Cases Fixed (8 bugs across 2 rounds)

1. Width underflow (8 sites) — `max(10, width-N)` guards
2. ANSI truncation — `lipgloss.Width()` + `ansi.Truncate()`
3. Ctrl+L stale state — clears tool tree + thinking text
4. Height=1 guard — "terminal too small" fallback
5. ToolResult by ID — `findRunningByID()` matches tool call ID
6. /quit subprocess leak — `cancel()` before `tea.Quit` (5 sites)
7. Escape cancel — verified false positive
8. /compact race — verified false positive (single-threaded Update loop)
