# Claude Code Gap Analysis: Features altcode Needs

> Generated 2026-04-01 from deep analysis of anthropics/claude-code

## Executive Summary

Claude Code is the most feature-rich AI coding CLI. While its core source is closed,
the plugin system, hooks API, settings format, and command architecture are fully
documented through the open repo. **Claude Code's key advantage is its extensibility
model** — hooks + plugins + commands + agents + skills create a composable platform
that third parties can extend without modifying core code.

**Top insights for altcode:**

| Priority | Feature | Why Claude Code got it right |
|----------|---------|------------------------------|
| P0 | Hooks system | Enables policy enforcement without core changes |
| P0 | Slash commands (markdown) | Zero-code extensibility — just write a .md file |
| P1 | Plugin system | Third-party ecosystem without core modification |
| P1 | Subagent spawning | Complex workflows delegate to specialized agents |
| P1 | `allowed-tools` per command | Fine-grained tool restriction per task |
| P2 | Prompt-based hooks | LLM evaluates policy — more flexible than regex |
| P2 | Progressive skill loading | Only load knowledge when triggered |

---

## P0: Must Have

### 1. Hooks System

**What Claude Code has**: Event-driven hooks that fire on 10+ events:
- `PreToolUse` — approve/deny/modify tool calls before execution
- `PostToolUse` — react to tool results (run linter after edit, etc.)
- `Stop` — validate task completion before agent stops
- `UserPromptSubmit` — inject context into user prompts
- `SessionStart` / `SessionEnd` — load/save context
- `PreCompact` — preserve critical info during compaction
- Plus: `SubagentStop`, `Notification`, `CwdChanged`, `FileChanged`, `TaskCreated`

Two hook types:
- **Command hooks**: Run a bash script, get JSON on stdin, return JSON on stdout
- **Prompt hooks**: LLM evaluates a prompt with `$TOOL_INPUT` / `$TOOL_RESULT` vars

**What altcode has**: Nothing. Permission system is static rules only.

**Why this matters**: Hooks are the #1 reason Claude Code has a plugin ecosystem.
Every plugin uses hooks to extend behavior. Without hooks, users can't enforce
policies, add context, or automate workflows.

**Design for altcode**:
```go
// internal/hooks/hooks.go
type Event string
const (
    PreToolUse      Event = "PreToolUse"
    PostToolUse     Event = "PostToolUse"
    Stop            Event = "Stop"
    UserPromptSubmit Event = "UserPromptSubmit"
    SessionStart    Event = "SessionStart"
    SessionEnd      Event = "SessionEnd"
)

type HookConfig struct {
    Matcher string      `json:"matcher"`   // tool name glob: "Write|Edit"
    Hooks   []HookEntry `json:"hooks"`
}

type HookEntry struct {
    Type    string `json:"type"`    // "command" or "prompt"
    Command string `json:"command"` // shell command to exec
    Prompt  string `json:"prompt"`  // LLM prompt (for prompt hooks)
    Timeout int    `json:"timeout"` // seconds
}

type HookResult struct {
    Continue         bool            `json:"continue"`
    SystemMessage    string          `json:"systemMessage,omitempty"`
    PermissionDecision string        `json:"permissionDecision,omitempty"` // allow/deny/ask
    UpdatedInput     json.RawMessage `json:"updatedInput,omitempty"`
}

// Execute runs all matching hooks for an event in parallel
func Execute(ctx context.Context, event Event, input HookInput) ([]HookResult, error)
```

Config in `settings.json`:
```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "python3 validate.py" }]
      }
    ]
  }
}
```

**Effort**: ~300 LoC | human: 3 days | AI: 30 min

---

### 2. Slash Commands (Markdown-based)

**What Claude Code has**: Commands are `.md` files in `commands/` with optional YAML frontmatter:
```markdown
---
allowed-tools: Bash(git *), Read, Grep
description: Review code changes
argument-hint: Optional file path
---

Current changes: !`git diff HEAD`

Review the code for bugs, security issues, and style problems.
```

Features:
- `!backtick` syntax executes bash inline and injects output
- `$ARGUMENTS` placeholder for user-supplied args
- `allowed-tools` restricts which tools the command can use
- Auto-discovered from `~/.claude/commands/` and `.claude/commands/`

**What altcode has**: Nothing. No extensibility mechanism.

**Why this matters**: Commands let users create reusable workflows with zero code.
The `allowed-tools` restriction is critical for security — a `/commit` command
should only use git, not `rm -rf`.

**Design for altcode**:
```go
// internal/command/command.go
type Command struct {
    Name         string   // from filename
    Description  string   // from frontmatter
    ArgumentHint string   // from frontmatter
    AllowedTools []string // from frontmatter
    Body         string   // markdown body (after frontmatter)
}

// Load discovers commands from config dirs
func Load(dirs ...string) ([]Command, error)

// Expand replaces !`cmd` with output and $ARGUMENTS with args
func (c *Command) Expand(args string) (string, error)
```

Discovery paths:
1. `~/.config/altcode/commands/`
2. `.altcode/commands/`
3. Plugin `commands/` directories

**Effort**: ~200 LoC | human: 2 days | AI: 20 min

---

## P1: Important

### 3. Plugin System

**What Claude Code has**: Plugins are directories with a `.claude-plugin/plugin.json` manifest:
```json
{
  "name": "my-plugin",
  "version": "1.0.0",
  "description": "Does things",
  "commands": "./commands",
  "agents": ["./agents"],
  "hooks": "./hooks/hooks.json",
  "mcpServers": "./.mcp.json"
}
```

Auto-discovered components:
- `commands/*.md` → slash commands
- `agents/*.md` → subagent definitions
- `skills/*/SKILL.md` → progressive knowledge
- `hooks/hooks.json` → event handlers
- `.mcp.json` → MCP server configs

**What altcode has**: No plugin system.

**Design for altcode**:
```go
// internal/plugin/plugin.go
type Plugin struct {
    Name        string
    Dir         string
    Commands    []command.Command
    Agents      []agent.Agent
    HookConfigs map[hooks.Event][]hooks.HookConfig
    MCPServers  map[string]config.MCPServerConfig
}

// Discover finds all plugins from standard locations
func Discover() ([]Plugin, error)
```

Plugin locations:
1. `~/.config/altcode/plugins/`
2. `.altcode/plugins/`

**Effort**: ~200 LoC | human: 2 days | AI: 20 min

---

### 4. Subagent System

**What Claude Code has**: Agents defined as `.md` files with YAML frontmatter:
```markdown
---
name: code-reviewer
description: Use when user asks for code review...
model: sonnet
color: blue
tools: ["Read", "Grep", "Bash"]
---

You are an expert code reviewer. Analyze the code for...
```

Features:
- Different model per agent (cheaper model for simple tasks)
- Tool restriction per agent
- Visual distinction (color in TUI)
- Trigger conditions via description examples
- `SubagentStop` hook to validate completion

**What altcode has**: Nothing. Single-agent only.

**Design for altcode**:
```go
// internal/agent/agent.go
type Agent struct {
    Name        string   `yaml:"name"`
    Description string   `yaml:"description"`
    Model       string   `yaml:"model"`       // "inherit", "sonnet", "opus"
    Tools       []string `yaml:"tools"`        // restricted tool list
    SystemPrompt string  // markdown body
}

// SpawnAgent creates a new engine with restricted tools and model
func SpawnAgent(parent *engine.Engine, agent Agent, input string) <-chan event.Event
```

**Effort**: ~250 LoC | human: 3 days | AI: 30 min

---

### 5. Tool Restriction per Context

**What Claude Code has**: `allowed-tools` in command frontmatter with pattern syntax:
```
allowed-tools: Bash(git *), Read, Grep, Edit
```

This means:
- `Bash` only allows commands matching `git *`
- `Read`, `Grep`, `Edit` allowed unconditionally
- All other tools blocked

**What altcode has**: Permission system is global, not per-command/per-agent.

**Design**: Extend tool.Registry with a `Subset` method (already exists) and
permission evaluator with a context-scoped restriction:

```go
// When executing a command or agent, create a scoped registry
scopedRegistry := registry.Subset(command.AllowedTools)

// For bash pattern restrictions like "Bash(git *)",
// add to permission evaluator as session rules
```

**Effort**: ~50 LoC on existing infrastructure

---

## P2: Nice to Have

### 6. Prompt-Based Hooks

**What Claude Code has**: Hooks where the LLM evaluates a prompt instead of running a script:
```json
{
  "type": "prompt",
  "prompt": "Check if this file write is safe. The tool input is: $TOOL_INPUT. Return 'approve' or 'deny' with reasoning."
}
```

**Why it's powerful**: Context-aware validation. A bash script can check if a file
is in `/etc`, but only an LLM can evaluate "is this edit consistent with the user's
request?"

**Design**: Add to hooks system — when `type: "prompt"`, send the expanded prompt
to the current model and parse the response.

**Effort**: ~80 LoC on top of hooks system

---

### 7. Progressive Skill Loading

**What Claude Code has**: Skills are directories with:
- `SKILL.md` — core instructions (loaded when triggered)
- `references/` — detailed docs (loaded on demand)
- `scripts/` — executable code (run, not loaded into context)
- `assets/` — templates (used in output)

Three-level loading:
1. **Always**: Name + description (~100 words) — determines when to load
2. **On trigger**: SKILL.md body (~5K words) — core knowledge
3. **On demand**: References, scripts, assets — as needed

**Design for altcode**:
```go
// internal/skill/skill.go
type Skill struct {
    Name        string
    Description string
    Body        string            // SKILL.md content
    References  map[string]string // filename → content
    Scripts     []string          // paths to scripts
}

// MatchTrigger checks if current context should load this skill
func (s *Skill) MatchTrigger(input string) bool
```

**Effort**: ~150 LoC | AI: 15 min

---

### 8. Stop Hook / Completion Validation

**What Claude Code has**: `Stop` hook fires when the agent tries to finish:
```json
{
  "Stop": [{
    "matcher": "*",
    "hooks": [{
      "type": "prompt",
      "prompt": "The agent is about to stop. Verify: 1) Tests were run if code was changed. 2) All requested features were implemented. 3) No TODOs were left. Return 'approve' or 'block' with reason."
    }]
  }]
}
```

If blocked, the agent continues with the reason as context.

**Why it matters**: Prevents the agent from declaring "done" prematurely — the
biggest failure mode of agentic coding.

**Design**: Natural extension of hooks system. When engine emits `event.Done`,
run Stop hooks. If any returns `block`, inject reason as a new user message
and continue the loop.

**Effort**: ~50 LoC on top of hooks system

---

## Features We Should NOT Copy

| Claude Code Feature | Why Skip for altcode |
|--------------------|---------------------|
| Plugin marketplace | Over-engineered for v1; local discovery is fine |
| Managed settings (enterprise) | Enterprise feature |
| PowerShell tool | Linux/macOS first |
| `$CLAUDE_ENV_FILE` | Complex; env vars in config is simpler |
| Prompt-based hooks (initially) | Start with command hooks; add prompt hooks later |
| Conditional `if` field for hooks | Premature optimization |

---

## Claude Code Patterns Worth Adopting

### 1. Command `!backtick` Syntax
Brilliant design. `!git status` in a command body is replaced with the output
at expansion time. Gives commands dynamic context without code.

### 2. Exit Code Protocol for Hooks
- Exit 0: Success, stdout shown in transcript
- Exit 2: Block, stderr fed back to agent as context
- Other: Non-blocking error

Simple, shell-native, easy to implement in any language.

### 3. Hook Input as JSON on stdin
All hooks receive the same JSON structure on stdin with `tool_name`, `tool_input`,
`session_id`, `cwd`, etc. Universal, language-agnostic.

### 4. `matcher` Pattern for Hook Targeting
`"matcher": "Write|Edit"` is concise and readable. Better than our glob approach
for tool matching.

### 5. Settings.json Permission Format
```json
{
  "permissions": {
    "ask": ["Bash"],
    "deny": ["WebSearch"]
  }
}
```
Cleaner than our `PermissionRule` array.

---

## Combined Roadmap (Codex + Claude Code)

Merging both gap analyses, here's the recommended implementation order:

### Phase A: Make it agentic (from Codex analysis)
1. ContentPart message format — support tool_use blocks
2. Tool-call agent loop — dispatch tools, loop until done
3. Session persistence — wire store.DB into engine

### Phase B: Make it scriptable (from Codex analysis)
4. Exec mode — `altcode "prompt"` headless mode
5. Session resume — `altcode --last`

### Phase C: Make it extensible (from Claude Code analysis)
6. Hooks system — PreToolUse, PostToolUse, Stop, SessionStart
7. Slash commands — markdown files with frontmatter
8. Plugin discovery — find and load plugins from standard dirs

### Phase D: Make it multi-model (from Codex analysis)
9. OpenAI-compatible provider
10. MCP client (basic stdio transport)

### Phase E: Make it safe (combined)
11. Output capping in bash tool
12. Sandbox (bwrap on Linux)
13. Tool restriction per command/agent

### Phase F: Advanced extensibility (from Claude Code)
14. Subagent system — spawn specialized agents
15. Skill loading — progressive knowledge disclosure
16. Stop hook — completion validation

**Total estimated AI-assisted time**: ~6 hours for all phases.
