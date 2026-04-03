# Agent Skills

Agent skills run as background subprocesses in isolated worktrees. They are
compatible with Claude Code, Codex CLI, Gemini CLI, and Cursor.

## Hard rules for all agents

### Secrets — zero tolerance
- **Never commit, log, or output secrets.** API keys, private keys, tokens, passwords,
  connection strings, service account JSON — none in code, config, logs, or comments.
- **Before staging files, grep for secrets**: `password`, `secret`, `api_key`, `token`,
  `private_key`, `-----BEGIN`, `AKIA`, `sk-`, `ghp_`, `xoxb-`. Block commit if found.
- **Use environment variables** for all secrets. Never hardcode.

## Available agents

### evaluate
Independent evaluator for the generator/evaluator loop. Grades work products
(code, features, plans, designs) against explicit criteria with scored reports.

**Invoke**: Spawn as background agent in worktree after any major generation step.

**Input**: target (files/URLs), type (code|feature|plan|design|pr), optional custom criteria.

**Output**: Structured evaluation report with weighted scores, verdict (PASS/ITERATE/FAIL),
blockers, improvements, and nitpicks.

### review-pr
Structured GitHub PR review with sections A–J. Runs in a worktree, reads the
full diff, and produces a pass/fail report.

### prepare-pr
Rebase onto main, fix review findings, run quality gates, push to PR head.
Runs after review-pr identifies issues.

### merge-pr
Squash merge a PR after prepare-pr succeeds. Final gate before landing.

## Multi-AI Development Rules

**Hard rule: Use BOTH Claude Code and Codex CLI for design, thinking, and evaluation.**

### Design Phase — consult Codex before implementing

Before writing code for any non-trivial feature:

```bash
codex exec --dangerously-bypass-approvals-and-sandbox \
  "Read [relevant files]. Design [feature]. Show Go structs, functions, integration points."
```

Codex provides a second architectural perspective. Incorporate its suggestions or
explicitly document why they were rejected.

### Evaluation Phase — Codex adversarial review after implementing

After every major feature lands:

```bash
codex exec --dangerously-bypass-approvals-and-sandbox \
  "Read [changed files]. Find bugs, race conditions, security holes, edge cases. Be adversarial."
```

Every finding from Codex becomes a test case in the evaluator suite.

### Thinking Phase — use both for complex decisions

When facing architectural decisions with multiple valid approaches:

1. Ask Claude Code to propose options with trade-offs
2. Ask Codex to challenge the recommendation:
   ```bash
   codex exec --dangerously-bypass-approvals-and-sandbox \
     "Claude recommends [approach]. Challenge this. What could go wrong? Is there a simpler way?"
   ```
3. Synthesize both perspectives into the final design

### The dual-AI loop

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│ Claude Code  │────▶│ Codex       │────▶│ Claude Code  │────▶│ Codex       │
│ (design)     │     │ (challenge) │     │ (implement)  │     │ (evaluate)  │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

### Configuration

- Codex flag: `--dangerously-bypass-approvals-and-sandbox` (required in this environment)
- Codex model: configured in `~/.codex/config.toml` (currently GPT-5.4 via relay)
- Claude Code: uses `--model` flag or auto-detected subscription
- Both have full codebase read access

### When NOT to use Codex

- Simple bug fixes (< 10 lines changed)
- Documentation-only changes
- Dependency updates
- When Codex relay is down or timing out

## Harness engineering architecture

The harness wraps around agents to ensure quality. Three pillars from OpenAI;
generator/evaluator loop from Anthropic. Combined into a unified dev/test/eval flow.

```
┌─────────────────────────────────────────────────────────────────┐
│                    HARNESS (the architecture)                   │
│                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────────────┐ │
│  │   Context     │  │ Constraints  │  │  Entropy Management   │ │
│  │ Engineering   │  │  (enforced)  │  │  (scheduled cleanup)  │ │
│  │              │  │              │  │                       │ │
│  │ CLAUDE.md    │  │ Hooks        │  │ Doc sync              │ │
│  │ AGENTS.md    │  │ Linters      │  │ Dead code removal     │ │
│  │ docs/plans/  │  │ CI checks    │  │ Constraint sweep      │ │
│  │ Skills       │  │ Type checks  │  │ Dependency audit      │ │
│  └──────────────┘  └──────────────┘  └───────────────────────┘ │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │              Dev/Test/Eval Flow                           │   │
│  │                                                          │   │
│  │  Plan ──→ Sprint Contract ──→ Generate ──→ Evaluate      │   │
│  │   │                              │              │        │   │
│  │   │                              └── iterate ←──┘        │   │
│  │   │                              (max 5 rounds)          │   │
│  │   ↓                                                      │   │
│  │  Eval: prompt → trace → checks → score → harness fix     │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### The meta-rule

> Every eval failure is a harness signal. Fix the environment, not the symptom.

When quality degrades, check:
1. **Missing context** — is the info in CLAUDE.md/AGENTS.md/docs?
2. **Weak constraint** — is the rule enforced mechanically or just prose?
3. **Broken feedback loop** — is the evaluator actually running?
4. **Stale assumption** — does the harness component still serve a purpose?

### Eval structure (OpenAI)

Every eval captures four dimensions:
- **Outcome**: Did the task complete? Does the feature work?
- **Process**: Did the agent follow intended patterns and tools?
- **Style**: Does output follow conventions?
- **Efficiency**: Did it get there without unnecessary commands?

### Harness evolution

Each harness component encodes an assumption about model limits. As models improve:
- Stress-test each assumption
- Remove non-load-bearing pieces
- Add components enabling previously impossible capabilities
- The space of useful combinations moves — it doesn't shrink

## Generator/evaluator pattern

These agents implement the generator/evaluator architecture from
[Anthropic's harness design research](https://www.anthropic.com/engineering/harness-design-long-running-apps).

```
Generator (main session)          Evaluator (background agent)
─────────────────────────         ──────────────────────────────
1. Implement feature        →     2. Read output, run tests,
                                     screenshot UI, grade criteria
3. Fix blockers from        ←     4. Return scored report with
   evaluation report                 PASS / ITERATE / FAIL verdict
5. Re-submit (max 5x)      →     6. Re-evaluate changed criteria only
```

### Why separate agents?

- **Self-evaluation bias**: Generators confidently praise their own mediocre output
- **Fresh context**: Evaluator starts clean, reads actual output (not the plan)
- **Isolation**: Worktree prevents evaluator from accidentally "fixing" things
- **Parallel execution**: Evaluator runs in background while user reviews generator output

### When to spawn evaluator agents

| Trigger | Agent | Mode |
|---------|-------|------|
| After feature implementation | evaluate | background worktree |
| Before shipping PR | evaluate (type: pr) | background worktree |
| After plan writing | evaluate (type: plan) | foreground |
| After design generation | evaluate (type: design) | background worktree |
| Long-running build (>30 min) | evaluate per sprint | background worktree |

## Design thinking frameworks

All evaluator agents apply three reasoning frameworks (see
`.claude/skills/evaluate/reference/design-thinking.md` for the full reference):

**Socratic Questioning** — Surface hidden assumptions before scoring. Ask clarifying,
assumption-probing, evidence-probing, alternative-exploring, and consequence-examining
questions. If a design can't survive basic questioning, it fails regardless of polish.

**First Principles** — Decompose to real constraints, not conventions. Identify the
actual requirement ("context-preserving navigation" not "sidebar"), list cognitive
truths (Fitt's Law, Miller's Law), and rebuild. Use the "5 Whys" to find root causes.

**Occam's Razor** — The simplest valid solution wins. Every element must earn its
place. Prefer removing over adding. Count concepts, not pixels.

Apply in sequence: Socratic (surface assumptions) → First Principles (decompose to
truths) → Occam's Razor (choose simplest valid solution).

## Adding new agents

Place agent skills in `.agents/skills/<name>/SKILL.md`. The SKILL.md follows the
same format as `.claude/skills/` but is designed for background execution:

- No interactive questions (use defaults or config.json)
- File-based input/output (structured markdown reports)
- Isolated worktree execution (no side effects on main workspace)
- Clear exit criteria (scored verdict, not open-ended)

## Build commands

```bash
npm test             # run tests
npm run build        # build project
npm run lint         # lint code
```

## Key conventions

- Agent skills are folders, not just markdown files
- Include `references/`, `scripts/`, `templates/` for progressive disclosure
- Store persistent data in `${CLAUDE_PLUGIN_DATA}` (survives upgrades)
- Description field is a trigger spec: "use when X, Y, Z"
