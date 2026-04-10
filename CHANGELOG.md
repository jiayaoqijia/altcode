# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.7.0] — 2026-04-07

The last release before the workspace-driven orchestration pivot. Everything below works with the internal engine + external CLI backends.

### Added

#### Team Orchestration (new)
- **External CLI agent backends** — spawn codex/claude/opencode as child processes with typed streaming events (`internal/agent/external.go`)
- **Workflow definitions** — YAML frontmatter files in `.altcode/workflows/` with phased execution, dependency DAG, parallel agents (`internal/wfdef/`)
- **Phase engine** — topo-sorted phase execution with context injection, override control, failure policies (`internal/orchestra/`)
- **Claude stream-json parser** — parses `--output-format stream-json` for typed events (text, thinking, tool_use, tool_result)
- **Claude control_request auto-approval** — responds to permission prompts via stdin (multica pattern)
- **Split-pane team TUI** — each agent gets a bordered pane with role badge, status icon, live output scrolling (`internal/tui/team_view.go`)
- **Workflow header breadcrumb** — `[design ✓] → [implement ⟳] → [review ·]` phase progress display
- **`/workflow <name> <task>`** — discovers and runs workflow definitions from `.altcode/workflows/`
- **`/team run <task>`** — auto-detects available CLIs and runs them in parallel with split panes
- **3 default workflows** — `ship-feature` (design→implement→review), `review` (parallel), `fix` (diagnose→fix→verify)
- **Override keys** — Ctrl+P pause, Ctrl+Q abort during workflow execution
- **Provider-aware context injection** — writes CLAUDE.md for claude, AGENTS.md for codex/opencode

#### Engine Improvements
- **Intent router** — classifies prompts into TaskClass (QA/fix/refactor/feature/architecture) with risk levels
- **Verification ladder** — build→vet post-edit verification replacing simple goAutoVerify
- **Turn summary** — emits `[feature implementation | risk:medium] 3 file(s) changed` at turn end
- **Compaction thrash detection** — stops after 3 consecutive compactions (matches Claude Code)
- **Compaction audit logging** — logs method, messages before/after, tokens before/after
- **Tool result truncation** — caps at 30KB to prevent context bloat
- **Non-blocking trySend** — prevents goroutine blockage when TUI is slow

#### TUI Improvements
- **Slash command tab completion** — type `/co` + Tab → `/compact`
- **`/search <query>`** — find text across all messages
- **Thinking block display** — collapsed 💭 messages when thinking ends
- **Tool error state** — ✗ icon for failed tools (was defined but never triggered)
- **Adaptive HUD collapse** — drops segments on narrow terminals (<60 cols)
- **Accurate token tracking** — uses API-reported tokens instead of crude estimate

### Fixed
- Context leak in SpawnExternal (cancel never called)
- Stderr silently dropped from external agents
- Parallel context injection race (concurrent file appends)
- `{{.PhaseOutput "design"}}` template key mismatch
- OpPause consumed but no blocking effect
- busy flag stuck after workflow completes
- waitForWfEvent cmd chain never started
- Double-verify bug in appendToolResults (called twice for Anthropic)

### Infrastructure
- 33 packages passing with `-race`
- 6 reference submodules (claude-code, codex, multica, cline-kanban, agent-orchestrator, openhands)
- Business/pricing strategy doc (`docs/business/pricing-and-direction.md`)
- Technical design spec (`docs/superpowers/specs/2026-04-07-team-orchestration-design.md`)

## [Unreleased]

### Fixed
- **Misleading "timeout 2m" on every running tool** — the tool tree used to render `Running… (Ns · timeout 2m)` on every live tool line, which made long reviews look like timeouts were firing constantly. Running tools now show only elapsed time.
- **Long agent turns killed mid-stream by a 5-minute HTTP cap** — both OpenAI and Anthropic providers had `http.Client{Timeout: 5 * time.Minute}`. In Go that covers the entire request lifetime including streaming body reads, so any turn longer than five minutes (extended thinking + multi-step tool use + slow providers) got cut off with a confusing timeout error. Replaced with a `Transport` that has granular dial/TLS/header/idle timeouts and no stream-length cap; user cancel still flows through the request context.
- **Silent bash tool timeouts** — when a Bash command was killed by its own timeout, the agent saw only `exit code -1` with partial stdout. It now gets `[bash: command killed after <d> — pass a larger timeout (ms)]`, so retries can widen the window explicitly instead of guessing.

