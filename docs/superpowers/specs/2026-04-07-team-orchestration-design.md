# Team Orchestration v2: Hybrid Automated Workflows with External CLI Backends

## Problem

Altcode's team/workflow features use its internal engine for agent execution. Users want it to orchestrate real CLI tools (codex, claude, opencode) as execution backends — like multica does — but with automated phased workflows that multica lacks. The TUI should show each agent's live output in split panes with manual override capability.

## Core Idea

Altcode becomes a **workflow orchestrator** that:
1. Reads declarative workflow definitions (phases, roles, backends)
2. Spawns real CLI tools (codex/claude/opencode) as agents
3. Chains phases automatically based on dependencies and conditions
4. Shows live split-pane TUI with per-agent streaming output
5. Allows manual override (pause, inject context, skip, abort) at phase boundaries

**What makes it better than multica**: Automated phase-gated workflows (multica is manual assignment).
**What it borrows from multica**: Real CLI backends with structured streaming, filesystem context injection.

## Architecture

Two new packages + modifications to existing TUI:

```
internal/wfdef/          NEW — workflow definition types + YAML parser
internal/orchestra/      NEW — phase engine, stream parser, context injection
internal/tui/            MOD — workflow header, phase transitions, override keys
internal/agent/          MOD — add parser hook to SpawnExternal
```

## 1. Workflow Definition Format

Markdown files with YAML frontmatter in `.altcode/workflows/`. Matches existing agent/skill format.

### Example: `.altcode/workflows/ship-feature.md`

```markdown
---
name: ship-feature
description: Design, implement, and review a feature
phases:
  - name: design
    agents:
      - role: architect
        backend: claude
        prompt: |
          Read the codebase. Design the feature: {{.Task}}
          Output a concrete implementation plan.
    timeout: 10m
    required: true

  - name: implement
    depends_on: [design]
    agents:
      - role: implementer
        backend: codex
        prompt: |
          Implement the feature based on this plan:
          {{.PhaseOutput "design"}}
    timeout: 20m
    required: true

  - name: review
    depends_on: [implement]
    parallel: true
    agents:
      - role: reviewer
        backend: claude
        prompt: "Review for bugs and security issues."
      - role: challenger
        backend: codex
        prompt: "Find race conditions, edge cases, and missing tests."
    on_failure: human
---

End-to-end feature development with design, implementation, and adversarial review.
```

### Go Types — `internal/wfdef/wfdef.go`

```go
type WorkflowDef struct {
    Name        string     `yaml:"name"`
    Description string     `yaml:"description"`
    Phases      []PhaseDef `yaml:"phases"`
    Path        string     `yaml:"-"` // source file
}

type PhaseDef struct {
    Name      string            `yaml:"name"`
    DependsOn []string          `yaml:"depends_on"`
    Parallel  bool              `yaml:"parallel"`
    Agents    []AgentAssignment `yaml:"agents"`
    Timeout   Duration          `yaml:"timeout"`
    Required  bool              `yaml:"required"`  // default true
    OnFailure FailurePolicy     `yaml:"on_failure"` // retry|skip|abort|human
    Condition string            `yaml:"condition"`  // Go template, optional
}

type AgentAssignment struct {
    Role    string `yaml:"role"`
    Backend string `yaml:"backend"` // codex|claude|opencode|auto
    Model   string `yaml:"model"`   // optional override
    Prompt  string `yaml:"prompt"`  // Go template: {{.Task}}, {{.PhaseOutput "name"}}
}

type FailurePolicy string
const (
    FailureRetry FailurePolicy = "retry"  // re-run phase up to 3x
    FailureSkip  FailurePolicy = "skip"   // mark failed, continue
    FailureAbort FailurePolicy = "abort"  // cancel workflow
    FailureHuman FailurePolicy = "human"  // pause for user input
)
```

Functions: `ParseFile(path) (*WorkflowDef, error)`, `Discover(dirs ...string) ([]*WorkflowDef, error)`.

## 2. Phase Engine — `internal/orchestra/`

### Core Types — `orchestra.go`

```go
type RunParams struct {
    Def      *wfdef.WorkflowDef
    Task     string
    WorkDir  string
    Events   chan<- PhaseEvent   // consumed by TUI
    Override <-chan OverrideCmd  // from TUI
}

type PhaseEvent struct {
    Phase   string
    Role    string
    Kind    PhaseEventKind
    Text    string
    Elapsed time.Duration
}

type PhaseEventKind string
const (
    KindLine      PhaseEventKind = "line"
    KindToolStart PhaseEventKind = "tool_start"
    KindToolDone  PhaseEventKind = "tool_done"
    KindThinking  PhaseEventKind = "thinking"
    KindError     PhaseEventKind = "error"
    KindPhaseDone PhaseEventKind = "phase_done"
)

type PhaseResult struct {
    PhaseID string
    Verdict Verdict
    Outputs map[string]string // role -> output text
    Elapsed time.Duration
}

type Verdict int
const (
    VerdictPass Verdict = iota
    VerdictFail
    VerdictTimeout
    VerdictSkipped
)
```

### Execution Loop

```
Run(ctx, params):
  1. Topological sort phases by depends_on
  2. For each phase group:
     a. Check Override channel (pause/skip/abort)
     b. Evaluate Condition template against prior outputs
     c. InjectContext — write CLAUDE.md + prior phase outputs to workdir
     d. Render prompt templates ({{.Task}}, {{.PhaseOutput "design"}})
     e. SpawnExternal for each agent in phase
     f. Stream lines through parser → PhaseEvent → Events channel
     g. Collect results
     h. Evaluate verdict (all agents exit 0 = pass; any required fail = check OnFailure)
     i. Persist phase state via workflow.SaveState
  3. Emit KindPhaseDone with PhaseResult
  4. If all phases pass: emit workflow complete
```

### Stream Parser — `parser.go`

Bridge between raw CLI output and typed PhaseEvents:

```go
// ParseClaudeStreamJSON parses one line of claude --output-format stream-json.
// Maps {"type":"assistant","message":{"content":[{"type":"text","text":"..."}]}}
// to PhaseEvent{Kind: KindLine, Text: "..."}.
func ParseClaudeStreamJSON(line string) *PhaseEvent

// ParseCodexLine heuristically classifies a raw codex output line.
// [tool_name] → KindToolStart, ✓ → KindToolDone, etc.
func ParseCodexLine(line string) *PhaseEvent
```

### Context Injection — `context.go`

Before spawning each agent, inject context into the workdir:

```go
func InjectContext(workDir string, def wfdef.PhaseDef, task string, priorOutputs map[string]string) error
```

- For claude backend: appends to CLAUDE.md with task context + prior phase outputs
- For codex backend: writes to AGENTS.md (codex reads this natively)
- Prior outputs truncated to 32KB max to avoid context overflow

## 3. Manual Override — `control.go`

```go
type OverrideCmd struct {
    Op      OverrideOp
    Target  string // role name, "" = all
    Message string // for Inject op
}

type OverrideOp string
const (
    OpPause  OverrideOp = "pause"   // freeze after current agents finish
    OpResume OverrideOp = "resume"
    OpSkip   OverrideOp = "skip"    // skip current phase
    OpInject OverrideOp = "inject"  // append context to next phase
    OpAbort  OverrideOp = "abort"   // cancel everything
)
```

Override is cooperative — takes effect at phase boundaries, not mid-execution. The `Run` loop checks the `Override` channel before entering each phase via `select`.

When `OnFailure: human`, the orchestrator emits `KindError` and blocks on the Override channel. The TUI shows "press r to retry, s to skip, q to abort" and routes the keypress to the channel.

## 4. TUI Integration

### Workflow Header — `tui/workflow_header.go`

Single-line breadcrumb above split panes:

```
[design ✓] → [implement ⟳] → [review ·]
```

Each phase badge is colored: green ✓ pass, yellow ⟳ running, red ✗ failed, gray · pending.

### Phase Transitions

When a phase completes, `teamView.Start(nextPhaseRoles)` resets the split panes for the new phase's agents. The workflow header advances.

### Override Key Bindings (during workflow)

- `Ctrl+P` — pause workflow
- `Ctrl+R` — resume
- `Ctrl+S` — skip current phase
- `Ctrl+Q` — abort workflow
- When paused: input textarea activates for context injection (Enter to send)

### App Changes

```go
// internal/tui/app.go — new fields
type App struct {
    // ... existing ...
    orchestraRun   *orchestra.Run    // nil when not in workflow mode
    workflowHeader *workflowHeader
}
```

`View()` renders: header → workflow breadcrumb → split panes → HUD → input.
`Update()` handles `workflowPhaseTick` and `workflowDoneTick` tea.Msg types.

### Data Flow

```
/workflow ship-feature "add auth"
  ↓
tui/commands.go: look up workflow def
  ↓
orchestra.Run(ctx, RunParams{Def, Task, WorkDir, Events, Override})
  ↓ goroutine per agent
agent.SpawnExternal → Lines chan
  ↓
orchestra/parser.go: ParseClaudeStreamJSON / ParseCodexLine
  ↓
Events chan → tui/workflow_run.go goroutine
  ↓
PhaseEvent{KindLine} → teamView.AppendLine(role, text)
PhaseEvent{KindPhaseDone} → workflowPhaseTick msg → advance header, reset panes
PhaseEvent{KindError} → show override prompt
```

## 5. Built-in Workflows

Ship 3 defaults in `.altcode/workflows/`:

**`ship-feature.md`** — design → implement → review (3 phases, 4 agents)
**`review.md`** — parallel review by 2-3 agents, synthesize findings (1 phase)  
**`fix.md`** — diagnose → fix → verify (3 phases, sequential single agent)

## 6. Error Handling

- **Agent crash**: check `PhaseDef.Required`. If required + `abort`: cancel siblings. If `retry`: re-run up to 3x. If `human`: block for override.
- **Timeout**: per-phase via `context.WithTimeout`. Treated as crash.
- **Parallel partial failure**: non-required agents can fail without aborting the phase.
- **Context overflow**: prior phase outputs truncated to 32KB in context injection.
- **Compaction thrash**: not applicable — each agent is a fresh subprocess, not sharing context.

## 7. Security

- Context injection writes task descriptions only — never API keys.
- Keys inherited from parent process environment.
- Agent workdir is either project root or a temp dir (configurable).
- Output files from agents are not auto-applied to project root — user reviews first.

## Files to Create

| File | Lines | Purpose |
|------|-------|---------|
| `internal/wfdef/wfdef.go` | ~120 | WorkflowDef types, YAML parser, Discover |
| `internal/wfdef/wfdef_test.go` | ~80 | Parse round-trip, validation |
| `internal/orchestra/orchestra.go` | ~200 | Phase engine, Run loop, topo sort |
| `internal/orchestra/parser.go` | ~100 | Claude stream-json + codex line parser |
| `internal/orchestra/context.go` | ~80 | CLAUDE.md/AGENTS.md injection |
| `internal/orchestra/control.go` | ~40 | OverrideCmd types |
| `internal/orchestra/orchestra_test.go` | ~120 | Phase sequencing with mock backends |
| `internal/tui/workflow_header.go` | ~60 | Phase breadcrumb renderer |
| `internal/tui/workflow_run.go` | ~100 | feedWorkflowRun goroutine, tea.Msg types |
| `.altcode/workflows/ship-feature.md` | ~40 | Default feature dev workflow |
| `.altcode/workflows/review.md` | ~20 | Default review workflow |
| `.altcode/workflows/fix.md` | ~25 | Default fix workflow |
| **Total new** | **~985** | |

## Files to Modify

| File | Changes |
|------|---------|
| `internal/agent/external.go` | Replace `Lines <-chan string` with `Events <-chan AgentEvent` (typed); add `trySend()`; add claude control_request handler |
| `internal/tui/app.go` | Add orchestraRun field, wire PhaseEvent in Update/View |
| `internal/tui/commands.go` | `/workflow <name> <task>` starts orchestra run |
| `internal/tui/team_view.go` | Add override key bindings during workflow |
| `internal/workflow/workflow.go` | Add ModeOrchestrate |
| `internal/workflow/runner.go` | Route ModeOrchestrate to orchestra.Run |
| `internal/config/config.go` | Add WorkflowDir field |

## Appendix: Patterns Borrowed from Multica

Based on deep investigation of vendor/multica (3 research agents, reading all source files in
pkg/agent/, internal/daemon/, internal/daemon/execenv/).

### A1. Typed Streaming Events (from pkg/agent/agent.go)

Multica's `Session.Messages <-chan Message` uses typed events instead of raw strings.
We adopt this pattern — replace `ExternalAgentStream.Lines <-chan string` with:

```go
type AgentEvent struct {
    Type    AgentEventType
    Content string
    Tool    string          // tool name for tool_use/tool_result
    CallID  string          // tool call ID
    Input   json.RawMessage // tool input (tool_use only)
    Status  string          // for status events
}

type AgentEventType string
const (
    EventText       AgentEventType = "text"
    EventThinking   AgentEventType = "thinking"
    EventToolUse    AgentEventType = "tool_use"
    EventToolResult AgentEventType = "tool_result"
    EventStatus     AgentEventType = "status"
    EventError      AgentEventType = "error"
)
```

### A2. Non-Blocking Channel Send (from pkg/agent/claude.go:312-319)

```go
func trySend[T any](ch chan<- T, v T) {
    select {
    case ch <- v:
    default: // drop if consumer is slow — output is also accumulated in Builder
    }
}
```

Prevents goroutine blockage when TUI render loop can't keep up with fast output.

### A3. Claude CLI Flags (from pkg/agent/claude.go:36-53)

Full flag set we should use:
```
claude --output-format stream-json --verbose --permission-mode bypassPermissions \
  --model MODEL --max-turns N --append-system-prompt SYSTEM -p PROMPT \
  --resume SESSION_ID
```

Key additions over our current flags:
- `--max-turns N` — prevents runaway agent loops
- `--append-system-prompt` — injects role-specific instructions
- `--resume SESSION_ID` — continues prior conversation across phases

### A4. Claude Control Request Handling (from pkg/agent/claude.go:224-260)

Claude sends `{"type":"control_request"}` via stdout asking for permission.
Must respond via stdin with:
```json
{"type":"control_response","response":{"subtype":"success","request_id":"...","response":{"behavior":"allow","updatedInput":{}}}}
```

Without this, claude agents **hang** waiting for approval. Critical for automated workflows.

### A5. Codex JSON-RPC Protocol (from pkg/agent/codex.go:14-232)

Multica uses `codex app-server --listen stdio://` with JSON-RPC 2.0 over stdin/stdout:
1. Initialize handshake: `initialize({clientInfo, capabilities})`
2. `thread/start({model, cwd, sandbox:"workspace-write"})` → threadID
3. `turn/start({threadId, input:[{type:"text",text:prompt}]})`
4. Wait on turn completion via notifications
5. Auto-approve: respond to `item/commandExecution/requestApproval` with `{"decision":"accept"}`

This gives structured events (tool calls, turn progress) vs our current fire-and-forget `codex exec`.
**Phase 2 adoption**: Start with `codex exec` (simpler), upgrade to JSON-RPC later.

### A6. Execution Environment (from internal/daemon/execenv/)

Per-task isolated workdir with provider-aware context injection:

```
{root}/{task_id}/
├── workdir/                      # agent's CWD
│   ├── CLAUDE.md or AGENTS.md    # runtime config (provider-specific)
│   ├── .agent_context/
│   │   └── issue_context.md      # task context
│   ├── .claude/skills/           # claude-native skill path
│   └── .config/opencode/skills/  # opencode-native skill path
├── output/                       # preserved after cleanup
└── logs/                         # preserved after cleanup
```

Key function: `InjectRuntimeConfig(workDir, provider, ctx)` writes CLAUDE.md for claude,
AGENTS.md for codex/opencode. Per-agent instructions injected as "Agent Identity" section.

### A7. Session Resume (from pkg/agent/claude.go, opencode.go)

Pass session IDs between phases for context continuity:
- Claude: `--resume SESSION_ID`
- OpenCode: `--session SESSION_ID`
- Result includes `SessionID` for next phase to use

### A8. Message Batching with Timed Flush (from daemon.go:975-1098)

The daemon batches text/thinking deltas and flushes every 500ms:

```go
// Accumulate text chunks, flush every 500ms as a single message
ticker := time.NewTicker(500 * time.Millisecond)
for msg := range session.Messages {
    switch msg.Type {
    case MessageText:
        pendingText.WriteString(msg.Content)  // accumulate
    case MessageToolUse:
        batch = append(batch, ...)  // tool calls flush immediately
    }
}
// ticker fires → flush accumulated text as one message
```

**Why adopt**: Prevents flooding the TUI with per-character updates. Text accumulates
for 500ms then renders as one block — smoother display, fewer redraws.
Tool calls flush immediately (not batched) — they're structural events.

### A9. Per-Task CODEX_HOME Isolation (from execenv/codex_home.go)

Each task gets its own CODEX_HOME directory:
- `auth.json` → **symlinked** (shares auth tokens)
- `config.json`, `config.toml`, `instructions.md` → **copied** (isolated per task)
- Skills written to `{codexHome}/skills/`
- Env var `CODEX_HOME={codexHome}` passed to subprocess

**Why adopt**: Prevents skill/config pollution between concurrent agents.

### A10. Workdir Reuse with Session Resume (from daemon.go:883-901)

If a prior task worked on the same issue:
1. Reuse the existing workdir (keeps code changes, git state)
2. Refresh context files in-place
3. Pass `--resume SESSION_ID` to continue the conversation

```go
if task.PriorWorkDir != "" {
    env = execenv.Reuse(task.PriorWorkDir, provider, taskCtx, logger)
}
if env == nil {
    env, _ = execenv.Prepare(params, logger)
}
```

**Why adopt**: Phase continuity — the review phase can resume in the implementer's workdir
with all code changes present. No need to re-clone or re-checkout.

### A11. Tool Result Truncation at 8KB (from daemon.go:1052)

```go
output := msg.Output
if len(output) > 8192 {
    output = output[:8192]
}
```

Applied when forwarding tool results to the server. Prevents huge file reads from
bloating the event stream. We should apply the same cap when streaming to TUI.

### A12. Cancellation Polling (from daemon.go:792-810)

Every 5 seconds, daemon checks if the task was cancelled server-side:
```go
ticker := time.NewTicker(5 * time.Second)
for {
    select {
    case <-ticker.C:
        if status, _ := client.GetTaskStatus(ctx, task.ID); status == "cancelled" {
            runCancel()  // cancel agent context
            return
        }
    }
}
```

**Adaptation for altcode**: Instead of polling a server, check a local cancel channel
from the TUI override system. Same cooperative pattern, different signal source.

### A13. Git Worktree per Agent (from execenv/git.go:70-84)

Each agent can get an isolated git worktree:
```go
func setupGitWorktree(gitRoot, worktreePath, branchName, baseRef string) error {
    err := runGitWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
    if err != nil && strings.Contains(err.Error(), "already exists") {
        branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
        err = runGitWorktreeAdd(...)
    }
    return err
}
```

**Why adopt**: Parallel agents editing the same repo need isolation. Without worktrees,
the implementer and reviewer would stomp on each other's changes. Timestamp-based
branch collision handling is pragmatic.

### A14. Event Bus Decoupling (from events/bus.go:8-82)

Synchronous pub/sub with panic recovery per handler:
```go
bus.Subscribe("task:completed", func(e Event) { ... })
bus.Publish(Event{Type: "task:completed", Payload: result})
```

**Adaptation for altcode**: The orchestra package should emit typed events to a channel.
The TUI consumes them. The orchestra never imports `tui` — events flow one direction.
This is already in our spec design (`PhaseEvent` channel), confirming the approach.

### A15. Full Execution Flow Summary (from daemon.go:763-1128)

The complete multica task flow that our orchestra should mirror:

```
1. handleTask(task):
   a. StartTask(taskID)                          → mark "executing"
   b. ReportProgress("Launching claude", 1, 2)   → TUI shows step 1/2
   c. Start cancellation polling (5s ticker)
   d. runTask(task):
      i.   execenv.Prepare() or Reuse()          → isolated workdir
      ii.  InjectRuntimeConfig(CLAUDE.md)         → context injection
      iii. backend.Execute(prompt, opts)          → spawn CLI
      iv.  Drain session.Messages in goroutine:
           - Text/thinking: accumulate, flush every 500ms
           - Tool calls: batch immediately with seq counter
           - Tool results: truncate at 8KB
           - Errors: batch immediately
      v.   <-session.Result                       → final outcome
   e. CompleteTask(output, sessionID, workDir)    → persist for reuse
      OR FailTask(error)
```

**This is the reference architecture our orchestra.Run() should follow.**

### A16. Patterns NOT Borrowed

- **Daemon poll loop** — multica polls a server. Altcode is local-first, no server.
- **Database task queue** — multica uses PostgreSQL. Altcode uses in-memory + file state.
- **Web kanban UI** — multica is web-based. Altcode stays terminal.
- **Manual task assignment** — multica requires human assignment. Altcode automates via workflows.
- **Codex JSON-RPC** (deferred) — complex bidirectional protocol. Use `codex exec` first, upgrade later.

## Build Sequence

1. `wfdef/` — types + parser + tests
2. `orchestra/parser.go` — claude stream-json parser + codex line parser + tests
3. `orchestra/context.go` — provider-aware context injection (multica execenv pattern)
4. `orchestra/control.go` — override types
5. Upgrade `agent/external.go` — typed events, trySend, claude control_request, session resume
6. `orchestra/orchestra.go` — phase engine + tests
7. `tui/workflow_header.go` — breadcrumb display
8. `tui/workflow_run.go` — goroutine + tea.Msg wiring
9. TUI modifications (app.go, commands.go, team_view.go)
10. Default workflow files
11. Integration test + visual TUI test
