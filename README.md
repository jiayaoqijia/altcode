# coding-template

<p align="center">
  <strong>AI-native project template with 42 skills, evaluator agents, design system, and browser automation.</strong>
</p>

<p align="center">
  <a href="https://github.com/jiayaoqijia/coding-template/actions"><img src="https://github.com/jiayaoqijia/coding-template/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/jiayaoqijia/coding-template/releases"><img src="https://img.shields.io/github/v/release/jiayaoqijia/coding-template" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-blue" alt="License"></a>
</p>

<p align="center">
  <a href="#quickstart">Quickstart</a> &middot;
  <a href="#skills">Skills</a> &middot;
  <a href="CONTRIBUTING.md">Contributing</a> &middot;
  <a href="AGENTS.md">Agents</a>
</p>

---

## What is this?

A batteries-included project template for AI-assisted development with full harness
engineering. Clone it, start coding — you get 43 skills, a three-pillar harness
(context, constraints, entropy management), generator/evaluator loops, browser
automation, and design thinking frameworks baked in.

Skills auto-sync from [gstack](https://github.com/garrytan/gstack) on every
conversation start.

## Key Features

- **43 skills** — 22 dev workflow + 21 design ([impeccable](https://impeccable.style))
- **Harness engineering** — three pillars: context engineering, architectural constraints, entropy management ([OpenAI](https://openai.com/index/harness-engineering/) + [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps))
- **Generator/evaluator pattern** — independent evaluator agent grades output against explicit criteria
- **Design thinking** — Socratic Questioning, First Principles, Occam's Razor applied to every decision
- **Browser automation** — Playwright MCP + Chrome DevTools MCP for QA testing and screenshots
- **Hard code limits** — 800-line files, 30-line functions, 3-level nesting, 3-branch conditionals
- **Auto-sync** — skills update from upstream gstack on every conversation start
- **Multi-agent compatible** — works with Claude Code, Codex CLI, Gemini CLI, Cursor

## Quickstart

```bash
# Use as a template
gh repo create my-project --template jiayaoqijia/coding-template
cd my-project

# Or clone directly
git clone https://github.com/jiayaoqijia/coding-template.git my-project
cd my-project
```

Skills are ready immediately. Try `/office-hours`, `/investigate`, or `/ship`.

## Skills

### Harness engineering
| Skill | Description |
|-------|-------------|
| `/harness` | Orchestrate full dev/test/eval flow, audit three pillars, entropy cleanup |
| `/generate` | Elite product builder — 30-year Google/Apple/Microsoft PM/CTO/designer |
| `/evaluate` | Elite evaluator — 30-year Google/AWS test expert + Apple design master |

### Development workflow
| Skill | Description |
|-------|-------------|
| `/office-hours` | Brainstorm ideas, startup diagnostic, builder mode |
| `/plan-ceo-review` | CEO/founder-mode plan review |
| `/plan-eng-review` | Engineering architecture review |
| `/plan-design-review` | Design dimension audit |
| `/design-consultation` | Create a design system from scratch |

### Implementation
| Skill | Description |
|-------|-------------|
| `/investigate` | Systematic debugging with root cause analysis |
| `/coding-agent` | Run Codex/Claude/agents in background with worktrees |
| `/careful` | Safety warnings for destructive commands |
| `/freeze` / `/unfreeze` | Lock edits to a specific directory |
| `/guard` | Maximum safety mode (careful + freeze) |

### Quality & evaluation
| Skill | Description |
|-------|-------------|
| `/evaluate` | Generator/evaluator quality gate — grade any output |
| `/qa` | Browser QA testing + automatic fix loop |
| `/qa-only` | QA report only, no fixes |
| `/review` | Pre-landing PR code review |
| `/design-review` | Visual design audit + fix loop |

### Design ([impeccable](https://impeccable.style))
| Skill | Description |
|-------|-------------|
| `/polish` | Refine UI details, spacing, alignment |
| `/audit` | Design audit with P0-P3 severity ratings |
| `/critique` | Score against Nielsen's 10 usability heuristics |
| `/typeset` | Typography improvements |
| `/colorize` | Color system refinement |
| `/animate` | Motion and transitions |
| `/arrange` | Layout and composition |
| `/overdrive` | Maximum design intensity |
| + 13 more | `/bolder`, `/quieter`, `/distill`, `/adapt`, `/harden`, `/clarify`, `/normalize`, `/delight`, `/optimize`, `/extract`, `/onboard`, `/frontend-design`, `/teach-impeccable` |

### PR workflow & release
| Skill | Description |
|-------|-------------|
| `/ship` | Full ship workflow: test, review, PR |
| `/document-release` | Post-ship documentation updates |
| `/retro` | Weekly engineering retrospective |
| `/codex` | Second opinion from OpenAI Codex CLI |

### Agents (background, worktree-isolated)
| Agent | Description |
|-------|-------------|
| `evaluate` | Independent evaluator for generator/evaluator loop |
| `review-pr` | Structured PR review (sections A-J) |
| `prepare-pr` | Rebase, fix review issues, run gates, push |
| `merge-pr` | Squash merge after prepare-pr |

## Architecture

```
project/
├── .claude/
│   ├── settings.json        # Permissions, env vars, agent teams
│   └── skills/              # 21 Claude Code skills
│       ├── harness/         # Harness engineering orchestrator
│       ├── evaluate/        # Generator/evaluator quality gate
│       ├── investigate/     # Root cause debugging
│       ├── ship/            # Ship workflow
│       ├── review/          # PR review + checklists
│       ├── qa/              # QA testing + templates
│       └── ...              # 17 more skills
├── .agents/skills/          # Background agent definitions
│   ├── evaluate/            # Evaluator agent
│   ├── review-pr/           # PR review agent
│   ├── prepare-pr/          # PR preparation agent
│   └── merge-pr/            # Merge agent
├── .mcp.json                # MCP servers (Playwright, Chrome DevTools, LSPs)
├── CLAUDE.md                # Harness rules, three pillars, design thinking, skill design
├── AGENTS.md                # Agent architecture, harness diagram, eval structure
├── .github/                 # Issue templates, PR template, CODEOWNERS
├── .editorconfig            # Formatting rules
└── .gitignore               # Comprehensive polyglot ignores
```

## MCP Servers

| Server | Purpose |
|--------|---------|
| Playwright | Browser automation, screenshots, form testing |
| Chrome DevTools | DevTools protocol, DOM inspection, network, Lighthouse |
| Go Language Server | Go LSP for definitions, references, diagnostics |
| TypeScript Language Server | TypeScript LSP for type checking |

## Hard Rules

These are enforced, not suggested:

- **No AI attribution in commits** — no `Co-Authored-By`, no AI mentions
- **800 lines max per file** — split if exceeded
- **30 lines max per function** — extract helpers
- **3 levels max nesting** — use early returns
- **3 branches max per block** — use maps or strategy pattern
- **Plans in the repo** — `docs/plans/` or `PLAN.md`, not chat history
- **Hooks over prompts** — write linter rules, not prose

## Harness Engineering

Based on [OpenAI](https://openai.com/index/harness-engineering/) and [Anthropic](https://www.anthropic.com/engineering/harness-design-long-running-apps):

```
HARNESS = Context Engineering + Architectural Constraints + Entropy Management
                                      │
                    Plan → Sprint Contract → Generate → Evaluate
                                               │            │
                                               └── iterate ←┘ (max 5)
                                                        │
                              Eval: prompt → trace → checks → score → harness fix
```

The model is not the bottleneck. The harness is the architecture. Every eval failure
is a signal to fix the environment. See [AGENTS.md](AGENTS.md).

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE).
