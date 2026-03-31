# Harness Engineering Principles

Synthesized from OpenAI's Harness Engineering and Anthropic's Harness Design research.

## The Three Pillars (OpenAI)

### 1. Context Engineering
The repository is the single source of truth. Anything inaccessible to the agent
doesn't exist in its world.

- **Static context**: CLAUDE.md, AGENTS.md, design specs, architecture docs
- **Dynamic context**: CI status, test results, git history, observability data
- **Progressive disclosure**: Skills as folders with references the agent reads on demand

**Rule**: If the agent can't find it in the repo, it doesn't know it. Encode everything
that matters into files the agent can read.

### 2. Architectural Constraints
Constraining the solution space makes agents *more* productive, not less.

- **Dependency layering**: Types → Config → Repo → Service → Runtime → UI
- **Deterministic linters**: Catch violations at write-time, not review-time
- **Structural tests**: Enforce boundaries mechanically (ArchUnit, import rules)
- **LLM auditors**: Agent-based review of other agents' output
- **Pre-commit hooks**: The last gate before code enters the repo

**Rule**: If a constraint isn't mechanically enforced, it will be violated. One hook
beats a hundred prompts.

### 3. Entropy Management ("Garbage Collection")
Code decays over time. AI-generated code decays faster. Fight entropy with scheduled
cleanup.

- **Doc sync agents**: Verify documentation matches current code
- **Constraint scanners**: Detect architectural violations that slipped through
- **Pattern auditors**: Identify deviations from established patterns
- **Dependency auditors**: Flag outdated, unused, or vulnerable dependencies

**Rule**: Schedule cleanup as part of the development loop, not as an afterthought.

## Generator/Evaluator Architecture (Anthropic)

### Core Problem: Self-Evaluation Bias
Agents reliably praise their own work. Separating generator from evaluator works
better than making generators self-critical.

### Core Problem: Context Degradation
Models lose coherence as context fills. Context resets (structured handoffs) work
better than compaction.

### Three-Agent System
1. **Planner**: Converts brief prompts → detailed product specs. High-level direction,
   not granular implementation. Focuses on deliverables.
2. **Generator**: Implements features in sprints. Self-evaluates before QA handoff.
   Maintains git version control.
3. **Evaluator**: Tests as a user would (Playwright). Negotiates "sprint contracts"
   with generator before work begins.

### Sprint Contracts
Before each sprint, generator and evaluator agree on:
- What completion looks like
- What will be tested
- What the pass criteria are

This bridges specs ↔ testable implementations.

### Key Results
- Solo run: 20 min, $9 → functional but broken mechanics
- Full harness: 6 hours, $200 → 16-feature polished product
- 20x cost → dramatically superior output

### Evolution Principle
> "Find the simplest solution; only increase complexity when needed."

Every harness component encodes an assumption about what the model can't do alone.
Those assumptions grow stale. Re-examine with each new model:
- Remove non-load-bearing pieces
- Add components enabling previously impossible capabilities
- The space of useful harness combinations moves, it doesn't shrink

## Eval Structure (OpenAI)

An eval is: prompt → captured run (trace + artifacts) → checks → score.

### Four Check Categories
1. **Outcome goals**: Did the task complete? Does the feature work?
2. **Process goals**: Did the agent follow intended patterns and tools?
3. **Style goals**: Does output follow conventions?
4. **Efficiency goals**: Did it get there without unnecessary commands?

### Eval Loop
```
Write eval → Run agent → Capture trace → Score → Identify failures
     ↑                                                    │
     └────────── Update harness/prompts/constraints ───────┘
```

## The Meta-Principle

> When the agent struggles, treat it as a signal: identify what is missing —
> tools, guardrails, documentation. Fix the harness, not the symptom.

The model is not the bottleneck. The harness is the architecture.
