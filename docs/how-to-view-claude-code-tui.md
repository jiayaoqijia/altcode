# How to View Claude Code's TUI from Inside a Session

## Method 1: Direct claude-hud invocation (best for status line data)

Claude Code's status line is rendered by the `claude-hud` plugin. You can invoke it
directly by piping the same stdin JSON that Claude Code sends:

```bash
# Find your session transcript
TRANSCRIPT="$HOME/.claude/projects/<project-hash>/<session-id>.jsonl"

# Run claude-hud with session context
echo '{
  "transcript_path": "'$TRANSCRIPT'",
  "cwd": "'$(pwd)'",
  "model": {"id": "claude-opus-4-6", "display_name": "Opus 4.6"},
  "context_window": {
    "context_window_size": 1000000,
    "used_percentage": 45
  }
}' | bun --env-file /dev/null \
  ~/.claude/plugins/cache/claude-hud/claude-hud/*/src/index.ts 2>/dev/null

# Strip ANSI for plain text
... | sed 's/\x1b\[[0-9;]*m//g'
```

### What it shows
- Line 1: `[Model] │ project git:(branch*) │ session-slug │ ⏱️ duration`
- Line 2: `Context █████░░░░░ 45%`
- Line 3: `N CLAUDE.md | N MCPs` (config counts)
- Line 4: `◐ running-tool | ✓ tool ×N | ✓ tool ×N` (tool activity)
- Line 5: `◐ agent-type [model]: description (elapsed)` (agent activity)
- Line 6: `▸ active-task (completed/total)` (todo progress)

## Method 2: Playwright browser capture (best for full TUI visual)

When running in a web IDE (Coder, code-server, VS Code Web):

```bash
# 1. Find code-server port
ps aux | grep code-server  # look for --bind-addr port

# 2. Navigate Playwright to it
# Uses MCP playwright tools:
mcp__playwright__browser_navigate("http://127.0.0.1:13337")

# 3. Dismiss trust dialog if shown
mcp__playwright__browser_click("Yes, I trust the authors")

# 4. Open terminal panel
# Press Ctrl+` or use command palette:
mcp__playwright__browser_press_key("Control+`")

# 5. Switch to Claude Code terminal tab
# Use command palette: "Terminal: Select Active Terminal"
mcp__playwright__browser_press_key("Control+Shift+P")
# Type "Terminal: Select Active Terminal" and pick the right one

# 6. Take screenshot
mcp__playwright__browser_take_screenshot(filename="cc-tui.png")
```

## Method 3: Read the transcript JSONL directly

The transcript contains ALL events (tool calls, agent spawns, todos):

```bash
# Last 20 events
tail -20 ~/.claude/projects/<hash>/<session>.jsonl | \
  jq -r '.message.content[0].type // .type'

# Tool calls this session
grep '"tool_use"' <transcript> | jq -r '.message.content[].name' | sort | uniq -c | sort -rn

# Active agents
grep '"Agent"' <transcript> | tail -5
```

## Method 4: Read the claude-hud source

The HUD plugin source reveals all data it displays:

```
~/.claude/plugins/cache/claude-hud/claude-hud/*/src/
├── index.ts          # Main entry: reads stdin, parses transcript, renders
├── types.ts          # StdinData, ToolEntry, AgentEntry, TodoItem, UsageData
├── transcript.ts     # Parses .jsonl → tools[], agents[], todos[]
├── stdin.ts          # Extracts context %, model, tokens from stdin
├── git.ts            # Git branch, dirty, ahead/behind, file stats
├── config.ts         # HUD config (colors, layout, thresholds)
├── config-reader.ts  # Counts CLAUDE.md, rules, MCPs, hooks
├── speed-tracker.ts  # Output tokens/second
├── memory.ts         # System memory usage
├── render/
│   ├── session-line.ts  # Model + context bar + git + config + usage + duration
│   ├── tools-line.ts    # Running tools (◐) + completed counts (✓ ×N)
│   ├── agents-line.ts   # Running/completed agents with elapsed time
│   ├── todos-line.ts    # Active todo + progress counter
│   └── lines/           # Identity, project, environment, usage, memory lines
```
