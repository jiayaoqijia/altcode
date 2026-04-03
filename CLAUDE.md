# Altcode Project Guide

## Project: altcode CLI

A Go CLI/TUI for AI-assisted coding. Architecture:

```
cmd/altcode/main.go         → Cobra CLI entry point
internal/engine/             → Agent loop (tool dispatch, session persistence, cost tracking)
internal/provider/           → Provider interface (Anthropic SSE + OpenAI SSE)
internal/tool/               → Tool interface, registry, concurrent dispatch
internal/permission/         → Permission evaluator (4 modes, doom loop)
internal/hooks/              → Hook system (13 events, command + prompt hooks, conditional if)
internal/agent/              → Subagent definitions, spawn, registry, team orchestration
internal/mcp/                → MCP client (stdio + SSE, tools + resources)
internal/command/             → Slash commands (markdown + frontmatter)
internal/plugin/             → Plugin discovery, loading, marketplace
internal/memory/             → Persistent cross-session memory
internal/cost/               → Per-turn token/USD cost tracking
internal/history/            → File operation journaling with diffs
internal/auth/               → Auto-detect Claude Code + Codex CLI credentials
internal/store/              → SQLite sessions + messages
internal/config/             → JSONC config, env expansion, instruction cascade
internal/compact/            → Context compaction (budget + micro)
internal/exec/               → Headless execution mode
internal/tui/                → Bubbletea TUI (15 slash commands, thinking indicator)
internal/event/              → Event types (engine ↔ TUI)
internal/sysctl/             → System prompt assembly
```

### Build & Test
```bash
make build          # Build to dist/altcode (uses -mod=mod)
make test           # Run all tests with race detector
make lint           # Run go vet
```

Note: Makefile sets `GOFLAGS=-mod=mod` automatically because vendor/ contains git submodules (codex, claude-code), not Go dependencies.

### Pre-Push Gate (HARD RULE)

**NEVER push without passing these locally first:**

```bash
# 1. Clean model-generated files (benchmarks create junk)
rm -f internal/main.go internal/stringxor.go internal/reverse_test.go
rm -rf internal/lru internal/middleware internal/stack internal/ratelimit internal/datastructures stack/

# 2. Build
GOFLAGS=-mod=mod go build ./...

# 3. Vet (catches fmt.Println newline issues, unused vars, etc.)
GOFLAGS=-mod=mod go vet ./...

# 4. Test (at minimum the packages you changed)
GOFLAGS=-mod=mod go test ./... -race -count=1 -timeout=180s
```

**If ANY step fails, fix it before committing.** Do not push broken code
and hope CI catches it — CI runs on 3 platforms and failures block everyone.

Common CI failures and how to avoid them:
- `found packages internal (bench_test.go) and main (main.go)` → model-generated `main.go` in internal/. Delete it.
- `fmt.Println arg list ends with redundant newline` → remove `\n` from Println.
- `undefined: SomeFunc` → model-generated test references deleted code. Delete the test file.
- `TestPatchToolNewFile` fails on macOS → system `patch` behaves differently. Use fallback parser.

### Current Status
- Website: https://altcode.io
- Multi-AI orchestrator: calls Claude Code, Codex CLI, and API models as backends
- 26 packages: engine, orchestrator, provider, tool (10), hooks (13 events), agent (teams), mcp, command, plugin, memory, cost, history, sandbox, task, auth, exec, tui, config, compact, store, sysctl, event
- CI green on Linux + macOS + Windows
- 5ms startup, 10MB binary
- 5ms startup, 10MB binary
- Providers: Anthropic, OpenAI/Codex, Ollama, LMStudio, OpenRouter (100+ models)
- Auth: auto-detects Claude Code sub + Codex CLI sub + OpenRouter key
- Claude Code compatible: loads CLAUDE.md, hooks, commands, plugins, agents, memory natively
- Benchmarks: DeepSeek 96%, MiniMax 93%, Qwen 93%, Claude 90% (5 suites × 6 models)

### Key Patterns
- Engine emits `<-chan event.Event` consumed by TUI or exec mode
- Tool dispatch respects concurrency (read tools parallel, write tools sequential)
- Messages use ContentPart for tool_use/tool_result blocks
- Permission evaluator checks before every tool execution
- Session messages persisted as JSON in SQLite

### Multi-AI Development Process

**Use both Claude Code and Codex CLI during design, thinking, and evaluation phases.**

This is a hard rule — not optional. Two AI systems catch more bugs, produce better
designs, and prevent blind spots from single-model reasoning.

#### When to use Codex

Use `codex --dangerously-bypass-approvals-and-sandbox` for:

1. **Design review** — before implementing a feature, ask Codex to review the plan:
   ```bash
   codex exec --dangerously-bypass-approvals-and-sandbox \
     "Read internal/engine/engine.go. Design [feature]. Show Go structs and functions."
   ```

2. **Adversarial challenge** — after implementing, ask Codex to break it:
   ```bash
   codex exec --dangerously-bypass-approvals-and-sandbox \
     "Read [files]. Find race conditions, security holes, edge cases. Be adversarial."
   ```

3. **Architecture scoring** — periodically ask Codex to score altcode vs competitors:
   ```bash
   codex exec --dangerously-bypass-approvals-and-sandbox \
     "Score altcode 1-10 on feature completeness, code quality, architecture. Compare to Claude Code CLI."
   ```

#### When to use Claude Code (this tool)

Use Claude Code for:
- Implementation (writing code, tests, docs)
- Deep codebase exploration and refactoring
- Running evaluator (`/evaluate`) with e2e tests
- Multi-file changes with dependency tracking

#### The generator/evaluator loop with both AIs

```
Claude Code (generate) → Codex (evaluate/challenge) → Claude Code (fix) → Codex (re-evaluate)
```

Every major feature should go through at least one Codex review before merging.
Codex findings become test cases in the evaluator suite.

#### Flags and configuration

- Codex requires `--dangerously-bypass-approvals-and-sandbox` in this environment
  (bwrap sandbox is blocked by the container runtime)
- Codex uses whatever model is configured in `~/.codex/config.toml` (currently GPT-5.4)
- Claude Code uses the model specified by `--model` or auto-detected credentials
- Both AIs have read access to the full codebase

---

# Project Development Guide

## Startup: sync skills from gstack

On every conversation start, check for gstack skill updates:

```bash
# Pull latest gstack to a temp dir and compare
GSTACK_TMP=$(mktemp -d)
git clone --depth 1 --quiet https://github.com/garrytan/gstack.git "$GSTACK_TMP" 2>/dev/null
```

For each skill directory in `$GSTACK_TMP` that has a `SKILL.md`, compare against
`.claude/skills/<skill>/SKILL.md`. If the upstream version is newer or different:

1. Copy the updated `SKILL.md` (and any resource files like checklists, templates)
2. Strip gstack-specific content: preamble bash blocks calling gstack binaries
   (`gstack-update-check`, `gstack-config`, `gstack-telemetry-log`), `~/.gstack/`
   paths, contributor mode sections, telemetry opt-in flows, `.tmpl` files
3. Replace `CC+gstack` with `AI-assisted` in effort tables
4. Preserve the YAML frontmatter and all actual skill logic

Skip these skills (not portable): `browse`, `gstack-upgrade`, `setup-browser-cookies`

```bash
rm -rf "$GSTACK_TMP"
```

Report briefly: "Skills synced — X updated" or "Skills up to date". Do not narrate
the diff unless asked.

## Commands

```bash
# Replace with your project's commands
npm install          # install dependencies
npm test             # run tests
npm run build        # build project
npm run lint         # lint code
npm run format       # format code
```

## Project structure

```
project/
├── src/             # Source code
├── tests/           # Test files
├── docs/            # Documentation
├── scripts/         # Build & utility scripts
├── .claude/         # Claude Code configuration
│   ├── settings.json
│   └── skills/      # Installed skills
├── .agents/         # Agent skills (Codex/Gemini/Cursor compatible)
│   └── skills/
├── .mcp.json        # MCP server configuration
├── CLAUDE.md        # This file — project config for Claude
└── package.json     # Project manifest
```

## Hard rules

### Commits
- **Never add Claude, AI, or any co-author attribution to commits.** No `Co-Authored-By`, no AI mentions in commit messages. Period.

### Secrets — zero tolerance
- **Never commit secrets.** API keys, private keys, tokens, passwords, connection strings,
  service account JSON — none of these belong in code, config files, logs, or comments.
- **Use environment variables** for all secrets. Reference `.env.example` for the schema.
- **Before every commit, check for secrets.** Grep for: `password`, `secret`, `api_key`,
  `token`, `private_key`, `-----BEGIN`, `AKIA`, `sk-`, `ghp_`, `gho_`, `xoxb-`, `xoxp-`.
  If any match, stop and extract to env vars.
- **Never log secrets.** Not even in debug mode. Mask or redact in all output.
- **.gitignore covers**: `.env*`, `*.pem`, `*.key`, `*.p12`, `credentials.json`,
  `service-account*.json`, SSH keys, GPG keys, kubeconfig, Docker config, npm/pypi tokens.

### Code red lines (not guidelines — hard limits)
- **File length: 800 lines max.** If a file exceeds 800 lines, split it. No exceptions.
- **Function length: 30 lines max.** If a function exceeds 30 lines, extract helpers.
- **Nesting depth: 3 levels max.** If nesting exceeds 3, use early returns or extract logic.
- **Conditional branches: 3 max per block.** If you need more than 3 branches, use a map, strategy pattern, or polymorphism.

These are not "try to keep short." They are **must not exceed.** Be obsessively clean.

### Project organization
- **Root directory holds the global map.** The root README, CLAUDE.md, and directory structure describe the full project topology.
- **Module directories hold member manifests.** Each module/package directory has an index or barrel file that declares what it exports.
- **File headers declare dependencies.** Imports go at the top. No dynamic requires buried in function bodies. Dependencies are visible at a glance.

### Enforcement over repetition
- **One hook beats a hundred prompts.** Write standards as linter rules, pre-commit hooks, or CI checks — not as prose that gets ignored. Catching violations at write-time is infinitely more effective than asking nicely in documentation.
- **Plans go in the project directory.** Store plans, design docs, and architecture decisions in `docs/plans/` or `PLAN.md` — not in chat history, not in memory, not in your head.

### Response style
- **Keep responses minimal.** Every unnecessary line of AI output is noise. Lead with the action or answer. Skip preamble, summaries of what was just done, and filler. If the diff speaks for itself, don't narrate it.

## Harness engineering (three pillars)

The model is not the bottleneck. The harness is the architecture. When the agent
struggles, fix the environment — not the symptom.

### Pillar 1: Context engineering
The repo is the single source of truth. If the agent can't find it, it doesn't know it.
- **CLAUDE.md** = commands, structure, hard rules, skill design, thinking frameworks
- **AGENTS.md** = agent definitions, patterns, harness architecture
- **docs/plans/** = design decisions, architecture, specs (not chat, not memory)
- **Module indexes** = barrel files declaring exports
- **File headers** = imports at top, dependencies visible at a glance

### Pillar 2: Architectural constraints
Constraining the solution space makes agents more productive, not less.
- Every constraint must be **mechanically enforced** (hook/linter/CI/test)
- Prose-only constraints are tech debt — upgrade to automation
- Dependency layers flow in one direction (no circular imports)
- Code red lines (800 lines, 30-line functions, 3 nesting, 3 branches) = hard limits

### Pillar 3: Entropy management
AI-generated code decays fast. Schedule cleanup as part of the loop.
- **Doc sync**: After shipping, verify docs match code (`/document-release`)
- **Constraint sweep**: Run all linters and structural tests (`/harness`)
- **Dead code**: Find orphaned files, unused exports, dead branches
- **Pattern alignment**: Ensure new code follows established patterns

### The meta-rule
> Every eval failure is a harness signal. When the agent produces bad output,
> ask: what context was missing? What constraint wasn't enforced? What feedback
> loop was broken? Fix the harness, then re-run.

## Design thinking frameworks

Three reasoning frameworks applied to every design and implementation decision.

### Socratic Questioning — surface hidden assumptions
Challenge every decision. Never accept "that's how it's done" as justification.
- **Clarifying**: "What problem does this actually solve?"
- **Probing assumptions**: "Why this? What if the opposite were true?"
- **Evidence**: "What data supports this? Has it been tested with users?"
- **Alternatives**: "What would this look like with zero configuration?"
- **Consequences**: "If this scales 10x, does the design still hold?"

### First Principles — decompose to real constraints
1. Identify the **real constraint**, not the assumed solution
   ("users need context-preserving navigation" not "we need a sidebar")
2. List **cognitive truths** (Fitt's Law, Miller's Law, attention scarcity)
3. Rebuild from truths. The "5 Whys": keep asking until you hit bedrock.

| Convention | First-principles question | Insight |
|-----------|--------------------------|---------|
| Settings page | "What if nothing needed config?" | Sensible defaults eliminate 80% of settings |
| Confirmation dialog | "Is the action actually destructive?" | Undo beats "are you sure?" |
| Loading spinner | "What can we show immediately?" | Skeleton screens reduce perceived wait |
| Pagination | "Why can't we show everything?" | Virtual scroll or better search |

### Occam's Razor — simplest valid solution wins
- Can I remove an element without losing function? → Remove it
- Can I merge two elements? → Merge them
- Can I replace custom with platform convention? → Replace it
- Would a new user understand this in 5 seconds? → If not, simplify

**Apply in sequence**: Socratic (surface assumptions) → First Principles (decompose)
→ Occam's Razor (choose simplest solution).

## Generator/evaluator pattern

**Never let the generator grade its own work.** Self-evaluation bias is real — agents
confidently praise mediocre output. Every significant generation step needs an
independent evaluator.

### Architecture (from Anthropic's harness design research)

```
┌─────────┐    generates    ┌──────────┐    evaluates    ┌───────────┐
│ Planner │ ──────────────→ │Generator │ ──────────────→ │ Evaluator │
│ (scope) │                 │ (builds) │ ←────────────── │ (grades)  │
└─────────┘                 └──────────┘    feedback      └───────────┘
                                 ↕                             │
                            iterate (max 5)              PASS / ITERATE / FAIL
```

### When to apply

| After generating... | Evaluate with... |
|---------------------|-----------------|
| Feature code | `/evaluate` — run tests, screenshot UI, grade criteria |
| Plan or design doc | `/evaluate` — check feasibility, scope clarity, risks |
| PR diff | `/review` + `/codex` — structural review + adversarial challenge |
| UI/frontend | `/qa` or `/design-review` — visual + functional verification |
| Any major deliverable | Spawn evaluator agent in background worktree |

### Core principles

1. **Separate generation from evaluation.** Different agent, different context, fresh eyes.
2. **Explicit grading criteria.** Convert subjective "is this good" into measurable
   dimensions with weights and pass thresholds. Never evaluate against vibes.
3. **Structured handoffs.** File-based communication between agents. The evaluator
   reads the actual output, not the generator's description of what it did.
4. **Decompose without over-specifying.** High-level specs prevent cascading errors
   better than granular technical prescriptions.
5. **Iterate with a cap.** Max 5 generator/evaluator cycles. If still failing,
   escalate — the approach needs rethinking, not more iterations.
6. **Continuously test assumptions.** Every harness component encodes an assumption
   about what the model can't do alone. Re-examine as models improve.

### Verdict thresholds

- **PASS** (≥ 7.0 weighted, no blockers): Ship it
- **ITERATE** (5.0–6.9 or has blockers): Fix blockers, re-evaluate
- **FAIL** (< 5.0): Fundamental rethink needed

### Applying the pattern

- **After `/ship`**: Spawn evaluator agent to independently verify the PR
- **After feature impl**: Run `/evaluate` before showing the user
- **After plan writing**: Run `/plan-eng-review` + `/plan-ceo-review` as evaluators
- **Long-running builds**: Use planner → generator → evaluator loop with sprint contracts

## Platform-agnostic design

Skills must NEVER hardcode framework-specific commands, file patterns, or directory
structures. Instead:

1. **Read CLAUDE.md** for project-specific config (test commands, build commands, etc.)
2. **If missing, ask the user** — let them tell you
3. **Persist the answer to CLAUDE.md** so we never have to ask again

The project owns its config; skills read it.

## Commit style

**Always bisect commits.** Every commit should be a single logical change. When
you've made multiple changes (e.g., a rename + a rewrite + new tests), split them
into separate commits before pushing. Each commit should be independently
understandable and revertable.

Examples of good bisection:
- Rename/move separate from behavior changes
- Test infrastructure separate from test implementations
- Mechanical refactors separate from new features

## CHANGELOG style

CHANGELOG.md is **for users**, not contributors. Write it like product release notes:

- Lead with what the user can now **do** that they couldn't before
- Use plain language, not implementation details
- Every entry should make someone think "oh nice, I want to try that"

## Skill design principles

Skills are **folders**, not just markdown files. The entire file system is a form of
context engineering and progressive disclosure.

### Structure
- **Use `references/`, `scripts/`, `templates/` subdirs.** Tell Claude what files
  exist in the skill folder; it will read them at appropriate times. Split detailed
  API signatures into `references/api.md`, output templates into `templates/`.
- **Store scripts and libraries.** Give Claude code to compose, not reconstruct.
  Helper functions, query builders, assertion libraries — these let Claude spend
  turns on decisions, not boilerplate.

### Content
- **Don't state the obvious.** Claude already knows how to code. Focus on what
  pushes it out of its defaults — your org's conventions, footguns, edge cases.
- **Build a Gotchas section.** The highest-signal content in any skill. Grow it
  over time from actual failure points Claude hits.
- **Avoid railroading.** Give Claude information and flexibility. Overly specific
  step-by-step instructions break when the situation doesn't match exactly.

### Configuration
- **The description field is for the model.** It's not a summary — it's a trigger
  spec. Write it as "use when X, Y, Z" so Claude can match requests to skills.
- **On-demand hooks > always-on hooks.** Skills like `/careful` and `/freeze`
  register hooks only when invoked, keeping other sessions clean.
- **Store setup in `config.json`.** If a skill needs user context (Slack channel,
  project ID), persist it in the skill directory so it's only asked once.

### Memory & data
- **Skills can store data.** Append-only logs, JSON state, SQLite — whatever fits.
  Use `${CLAUDE_PLUGIN_DATA}` for stable storage that survives skill upgrades.
- **Previous results improve future runs.** A standup skill that reads its own
  history can show deltas. A review skill that tracks past findings avoids repeats.

### Skill categories to consider
1. **Library & API reference** — internal libs, CLIs, SDKs with gotchas and examples
2. **Product verification** — test flows with Playwright/tmux, programmatic assertions
3. **Data fetching & analysis** — connect to monitoring, dashboards, event sources
4. **Business process automation** — standup posts, ticket creation, weekly recaps
5. **Code scaffolding & templates** — framework boilerplate with org conventions
6. **Code quality & review** — style enforcement, adversarial review, test practices
7. **CI/CD & deployment** — PR babysitting, deploy pipelines, cherry-pick workflows
8. **Runbooks** — symptom → investigation → structured report
9. **Infrastructure operations** — cleanup, dependency management, cost investigation

## Available skills

### Harness engineering
- `/harness` — Orchestrate full dev/test/eval flow, audit three pillars, entropy cleanup
- `/generate` — Elite product builder (30-year Google/Apple/Microsoft PM/CTO/designer)
- `/evaluate` — Elite evaluator (30-year Google/AWS test expert + Apple design master)

### Development workflow
- `/office-hours` — Brainstorm ideas, startup diagnostic, builder mode
- `/plan-ceo-review` — CEO/founder-mode plan review (scope expansion/reduction)
- `/plan-eng-review` — Engineering architecture review
- `/plan-design-review` — Design dimension audit
- `/design-consultation` — Create a design system from scratch

### Implementation
- `/investigate` — Systematic debugging with root cause analysis
- `/coding-agent` — Run Codex/Claude/agents in background with worktrees
- `/careful` — Safety warnings for destructive commands
- `/freeze` / `/unfreeze` — Lock edits to a specific directory
- `/guard` — Maximum safety mode (careful + freeze)

### Quality & evaluation
- `/evaluate` — Generator/evaluator quality gate (grade any output against criteria)
- `/qa` — Browser QA testing + automatic fix loop
- `/qa-only` — QA report only, no fixes
- `/review` — Pre-landing PR code review
- `/design-review` — Visual design audit + fix loop

### PR workflow
- `/review-pr` — Structured PR review (sections A-J)
- `/prepare-pr` — Rebase, fix review issues, run gates, push
- `/merge-pr` — Squash merge after prepare-pr

### Release
- `/ship` — Full ship workflow: merge base, test, review, PR
- `/document-release` — Post-ship documentation updates
- `/retro` — Weekly engineering retrospective

### Design (impeccable)
- `/polish` — Refine UI details, spacing, alignment
- `/audit` — Design audit with P0-P3 severity ratings
- `/critique` — Score against Nielsen's 10 usability heuristics
- `/typeset` — Typography improvements
- `/colorize` — Color system refinement
- `/arrange` — Layout and composition
- `/animate` — Motion and transitions
- `/bolder` / `/quieter` — Increase or decrease visual weight
- `/overdrive` — Maximum design intensity
- `/distill` — Simplify and reduce
- `/adapt` — Responsive design adjustments
- `/harden` — Accessibility and robustness
- `/clarify` — Improve readability and hierarchy
- `/normalize` — Consistency pass
- `/delight` — Add micro-interactions and polish
- `/optimize` — Performance-focused design
- `/extract` — Extract design tokens and patterns
- `/onboard` — First-run and onboarding UX
- `/frontend-design` — Enhanced frontend design skill
- `/teach-impeccable` — Gather design context for all skills

### Second opinions
- `/codex` — Get a second opinion from OpenAI Codex CLI

## AI effort compression

When estimating or discussing effort, show both human-team and AI-assisted time:

| Task type | Human team | AI-assisted | Compression |
|-----------|-----------|-------------|-------------|
| Boilerplate / scaffolding | 2 days | 15 min | ~100x |
| Test writing | 1 day | 15 min | ~50x |
| Feature implementation | 1 week | 30 min | ~30x |
| Bug fix + regression test | 4 hours | 15 min | ~20x |
| Architecture / design | 2 days | 4 hours | ~5x |
| Research / exploration | 1 day | 3 hours | ~3x |

Completeness is cheap. Don't recommend shortcuts when the complete implementation
is achievable. Implement the full solution when the scope is reasonable.
