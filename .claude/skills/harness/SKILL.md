---
name: harness
version: 1.0.0
description: |
  Harness engineering orchestrator for the full dev/test/eval flow. Enforces the
  three pillars (context engineering, architectural constraints, entropy management)
  and the generator/evaluator loop. Use when starting a project, beginning a feature,
  auditing harness health, or when agent output quality degrades.
  Use when: "harness check", "audit the harness", "setup harness", "why is quality
  degrading", "enforce constraints", "run entropy cleanup", "eval flow".
  Proactively suggest when starting a new project or when agent output quality drops.
allowed-tools:
  - Bash
  - Read
  - Write
  - Edit
  - Glob
  - Grep
  - Agent
  - WebFetch
---

# /harness — Harness Engineering Orchestrator

Read `reference/principles.md` for the full framework. This skill orchestrates
the three pillars of harness engineering across the entire dev/test/eval flow.

## When to run

| Trigger | Mode |
|---------|------|
| Starting a new project | **Setup** — scaffold context, constraints, evals |
| Beginning a major feature | **Plan** — planner → sprint contracts → eval criteria |
| Quality is degrading | **Diagnose** — identify missing context, weak constraints, stale assumptions |
| Post-ship | **Entropy** — cleanup, doc sync, constraint scan |
| New model available | **Evolve** — stress-test assumptions, remove non-load-bearing pieces |

## Pillar 1: Context Engineering

The repository is the single source of truth. Audit and fix context gaps.

### Checklist

```markdown
- [ ] CLAUDE.md exists and covers: commands, structure, hard rules, skill design
- [ ] AGENTS.md exists and documents all agents and patterns
- [ ] Every module has an index/barrel file declaring exports
- [ ] Dependencies are visible at file top (no buried dynamic imports)
- [ ] Design decisions are in docs/plans/ (not chat, not memory)
- [ ] Gotchas section exists for non-obvious project quirks
- [ ] CHANGELOG reflects what users can DO, not what was changed
```

### Fix protocol

For each missing item:
1. Check if the information exists elsewhere (git log, comments, memory)
2. If yes → extract and write to the correct file
3. If no → ask the user, then persist to the correct file
4. Never leave context in chat — it dies when the session ends

## Pillar 2: Architectural Constraints

Constraints must be mechanically enforced. Prose instructions get ignored.

### Constraint audit

```markdown
- [ ] File length ≤ 800 lines (enforced by: ___)
- [ ] Function length ≤ 30 lines (enforced by: ___)
- [ ] Nesting depth ≤ 3 (enforced by: ___)
- [ ] Branch count ≤ 3 per block (enforced by: ___)
- [ ] No secrets in code (enforced by: ___)
- [ ] Import structure follows dependency layers (enforced by: ___)
- [ ] Commit style enforced (enforced by: ___)
```

For each constraint, the "enforced by" must be one of:
- **Pre-commit hook** (best — catches at write-time)
- **CI check** (good — catches before merge)
- **Linter rule** (good — catches in editor)
- **Structural test** (good — catches in test suite)
- **LLM auditor agent** (acceptable — catches in review)
- **Prose in CLAUDE.md** (weak — relies on agent compliance)

### Upgrade protocol

For any constraint only enforced by prose:
1. Can it be a linter rule? → Write the rule
2. Can it be a pre-commit hook? → Write the hook
3. Can it be a structural test? → Write the test
4. Can it be a CI check? → Write the check
5. Only if none of the above → keep as prose, but flag as tech debt

## Pillar 3: Entropy Management

Code decays. AI-generated code decays faster. Schedule cleanup.

### Entropy scan

```bash
# Documentation drift
git diff HEAD~20 --name-only | grep -E '\.(ts|tsx|py|go|rs)$' | head -20
# Compare against: which docs reference these files?

# Orphaned code
# Files not imported by anything, not in tests, not entry points

# Constraint violations
# Run all linters, structural tests, type checks

# Pattern deviations
# Grep for anti-patterns: console.log, TODO, FIXME, any, @ts-ignore
```

### Cleanup protocol

1. **Doc sync**: For each changed source file, verify referencing docs are current
2. **Dead code**: Find and remove orphaned files, unused exports, dead branches
3. **Constraint sweep**: Run all enforcement tools, fix violations
4. **Dependency audit**: Check for outdated, unused, or vulnerable dependencies
5. **Pattern alignment**: Ensure new code follows established patterns

## Generator/Evaluator Flow

For any significant generation task, enforce the full loop:

### Step 1: Plan (Planner agent)
- Convert user request → detailed spec with deliverables
- Identify what completion looks like (testable criteria)
- Identify risks and unknowns

### Step 2: Sprint Contract (Generator ↔ Evaluator)
Before writing code, agree on:
```markdown
## Sprint Contract: [Feature Name]

### Deliverables
- [ ] [Specific, testable deliverable 1]
- [ ] [Specific, testable deliverable 2]

### Pass criteria
- [ ] All tests pass
- [ ] No new linter violations
- [ ] [Feature-specific criterion with measurement]

### How evaluator will verify
- [ ] Run test suite
- [ ] [Playwright flow / screenshot / API test]
- [ ] [Manual inspection point]
```

### Step 3: Generate (Generator agent)
- Implement in focused sprints (one feature at a time)
- Self-evaluate before handoff (but don't trust self-evaluation)
- Maintain git version control (commit per logical change)

### Step 4: Evaluate (Evaluator agent)
- Test as a user would (click through, submit forms, trigger edge cases)
- Score against sprint contract criteria
- Return structured report: PASS / ITERATE / FAIL

### Step 5: Iterate (max 5 rounds)
- Generator fixes only the blockers (no scope creep)
- Evaluator re-checks only changed criteria
- If still failing after 5 rounds → escalate to user

## Eval Structure

For each eval, capture:

### Prompt → Trace → Checks → Score

```markdown
## Eval: [Name]

### Prompt
[The exact input that triggered the work]

### Trace
[Key decisions, tool calls, files changed]

### Checks
- [ ] Outcome: Did the task complete? Does the feature work?
- [ ] Process: Did the agent follow intended patterns and tools?
- [ ] Style: Does output follow conventions?
- [ ] Efficiency: Did it get there without unnecessary commands?

### Score
[1-10 with justification]

### Harness feedback
[What should change in the harness to improve this score next time?]
```

The last field is critical: **every eval failure is a harness signal**, not just
an agent failure. When the agent struggles, fix the environment.

## Evolving the Harness

When a new model is available or quality changes:

1. **List all harness components** — each encodes an assumption about model limits
2. **Stress-test each assumption** — does the model still need this guardrail?
3. **Remove non-load-bearing pieces** — reduce overhead
4. **Add new components** — enable capabilities the old model couldn't handle
5. **Re-run evals** — compare scores before/after harness changes

> The space of useful harness combinations moves as models improve.
> It doesn't shrink — it shifts.
