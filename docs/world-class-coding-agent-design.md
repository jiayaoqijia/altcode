# Altcode World-Class Coding Agent Design Doc

## Status
Draft

## Date
2026-04-07

## Authors
altcode + AI design review

---

## 1. Executive Summary

This document proposes how to evolve **altcode** from a strong multi-model coding harness into a **world-class coding agent**: fast, reliable, transparent, and measurably superior on real software engineering tasks.

Altcode already has unusually broad infrastructure for its size: a multi-turn agent loop, multi-provider support, tool dispatch with concurrency partitioning, workflows, hooks, permissions, MCP integration, persistent memory, session storage, and a terminal UI. The next phase is not about adding random features. It is about turning these parts into a coherent system that consistently outperforms generic coding CLIs on:

- time-to-first-useful-action
- success rate on multi-file coding tasks
- test-writing depth and verification quality
- recovery from failed edits, flaky commands, and ambiguous tasks
- session continuity across interruptions and long-running work
- operator trust and control

The core thesis of this design doc is:

> **World-class coding agents are not just “LLMs with tools.” They are disciplined software engineering runtimes.**

That means altcode should optimize for five pillars:

1. **Responsiveness** — the agent starts acting quickly and feels alive
2. **Reliability** — the agent recovers gracefully and avoids brittle failure modes
3. **Quality** — the agent produces verified code, not just plausible code
4. **Autonomy** — the agent decomposes and executes complex tasks intelligently
5. **Trust** — the operator understands what the agent is doing and why

This doc introduces a target architecture, execution model, quality loop, state model, evaluation framework, and phased implementation plan.

---

## 2. Goals and Non-Goals

### 2.1 Goals

Altcode should become a coding agent that:

1. **Feels extremely fast**
   - Low startup overhead
   - Low time-to-first-tool-call
   - Low time-to-first-edit
   - Streaming visible progress within the first second when possible

2. **Executes reliably on real repos**
   - Handles multi-file changes
   - Survives patch failures and compile errors
   - Avoids getting stuck in repetitive loops
   - Produces useful fallback behavior when uncertain

3. **Ships higher-quality code than model-only competitors**
   - Writes tests by default for code changes
   - Verifies changes before claiming completion
   - Uses review/evaluator passes to catch mistakes
   - Reports residual uncertainty honestly

4. **Scales from small fixes to large tasks**
   - Good defaults for simple tasks
   - Structured workflows for ambiguous or risky tasks
   - Multi-agent decomposition for larger work
   - Session resume with meaningful continuity

5. **Wins on benchmarkable engineering metrics**
   - SWE-bench-style issue resolution
   - terminal/CLI automation tasks
   - code editing/refactoring tasks
   - resume/interruption/recovery tasks

6. **Preserves operator trust**
   - Transparent tool use
   - Strong permissioning
   - Predictable execution policy
   - Clear summaries of what changed and what was verified

### 2.2 Non-Goals

This design does **not** aim to:

- build a cloud IDE or collaborative backend first
- compete on general chat UX unrelated to coding
- optimize for maximum autonomous risk-taking
- replace all human review for production-critical changes
- depend on a single model vendor or proprietary capability

Altcode should remain:

- local-first
- provider-agnostic
- scriptable
- terminal-native
- biased toward correctness over theatrics

---

## 3. Current Strengths and Current Gaps

### 3.1 Current Strengths

Based on the repository structure and public docs, altcode already has:

- a Go-native CLI with fast startup and single-binary distribution
- multi-turn agent loop infrastructure
- typed tool registry and dispatch
- concurrency-aware tool scheduling
- hooks and permissions
- workflow modes (interview / plan / ralph)
- multi-agent orchestration primitives
- persistent memory and session storage
- MCP support
- TUI and headless execution modes
- benchmark awareness and competitive analysis docs

This is a strong base. Most coding agents never get beyond “prompt + tools.” Altcode already thinks in systems.

### 3.2 Current Gaps

However, becoming world-class requires more than having components. The likely gaps are in the **behavioral integration** of those components.

The main gaps are:

1. **First-action latency is not yet optimized as a product principle**
2. **The quality loop is likely too optional, not strict enough by default**
3. **Recovery policies are not explicit and deterministic enough**
4. **Multi-agent capabilities need stronger automatic routing and specialization**
5. **Context management needs decision preservation, not just token trimming**
6. **Benchmarking needs to become a first-class engineering feedback loop**
7. **Trust UX must explain tool actions and residual risks more clearly**

---

## 4. Product Principles

Altcode should be shaped by the following product principles.

### 4.1 Principle: Action beats narration

The best coding agents do not spend too long describing what they might do. They start gathering evidence or making safe progress quickly.

Implications:
- Prefer targeted reads over broad repo scans
- Prefer early verification over long speculative planning
- Prefer partial implementation + test feedback over large unverified rewrites

### 4.2 Principle: Verification is part of generation

Verification is not a separate nice-to-have phase. It is part of the core execution loop.

Implications:
- Code changes should usually trigger tests/builds automatically
- A task is not done when text output sounds good; it is done when evidence supports completion
- Verification artifacts should be visible in the final summary

### 4.3 Principle: Structure should emerge from task complexity

Simple tasks should stay simple. Complex tasks should get more scaffolding.

Implications:
- Don’t force planning for obvious one-file fixes
- Do force structure for broad, ambiguous, or risky changes
- Mode selection should be task-aware and mostly automatic

### 4.4 Principle: The agent must remember decisions, not just messages

Long tasks fail when the agent forgets why it chose an approach or which attempts already failed.

Implications:
- Persist a decision ledger
- Persist verification history
- Persist unresolved questions and open risks
- Use semantic summaries, not only chat truncation

### 4.5 Principle: Every failure mode deserves a recovery strategy

Agent reliability improves dramatically when known failure classes map to explicit recovery policies.

Implications:
- Edit mismatch → re-read narrower region → patch strategy fallback
- Build failure → inspect diagnostics → localize fix → re-run targeted verification
- Test timeout → rerun targeted package/test → classify infrastructure vs code issue
- Ambiguous repo layout → use structural search/index before more edits

### 4.6 Principle: Trust grows from transparency and bounded autonomy

Operators trust systems that are legible and appropriately constrained.

Implications:
- Explain risky tool use before execution when necessary
- Separate read-only, modify, and external-side-effect actions
- Make policies visible and predictable
- Report unknowns rather than bluffing

---

## 5. Target User Experience

### 5.1 Simple bug fix

User:
> fix the nil dereference in cache eviction and add tests

Target behavior:
1. Agent immediately searches for cache/eviction-related files
2. Reads the likely implementation and test file in parallel if safe
3. Starts an edit quickly
4. Adds focused tests
5. Runs targeted tests, then package/build verification
6. Summarizes changes and confidence

The entire interaction should feel concise, fast, and evidence-driven.

### 5.2 Ambiguous feature request

User:
> add role-based access control to the admin API

Target behavior:
1. Agent classifies as multi-file / ambiguous / security-sensitive
2. Switches into structured mode automatically
3. Performs lightweight architecture discovery
4. Produces plan or asks clarifying questions if key constraints are missing
5. Executes in staged steps with verification after each major step
6. Runs reviewer pass focused on auth/security/test coverage

### 5.3 Long-running interrupted task

User starts work, interrupts, then runs:
> altcode --last

Target behavior:
- Restore objective, current plan step, changed files, latest test/build status, unresolved issues, and recommended next action
- Avoid repeating already-completed investigation
- Resume with minimal operator restatement

### 5.4 Code review / audit task

User:
> review the auth middleware for bugs and missing tests

Target behavior:
- Use read/search tools only unless explicitly allowed otherwise
- Produce findings ordered by severity
- Cite file paths and rationale
- Recommend tests or fixes
- Offer to patch if user approves

---

## 6. Success Metrics

Altcode should track improvements with hard metrics, not just anecdotes.

### 6.1 Core latency metrics

- **Startup latency**: process start to ready prompt
- **Time to first token**: prompt submit to first visible output
- **Time to first tool**: prompt submit to first tool call
- **Time to first edit**: prompt submit to first file modification
- **Median tool round-trip latency**
- **Verification completion time**

### 6.2 Reliability metrics

- **Task success rate** on benchmark suites
- **Tool-call success rate**
- **Edit failure rate** (no-op edits, ambiguous matches, patch failures)
- **Loop rate**: repeated ineffective cycles per task
- **Recovery success rate** after failed build/test/edit
- **Resume success rate** after interruption

### 6.3 Quality metrics

- **Tests added per code-changing task**
- **Targeted verification run rate**
- **Final verification pass rate**
- **Reviewer-detected issue catch rate**
- **Post-task regressions on hidden evals**

### 6.4 UX/trust metrics

- **Permission override frequency**
- **User interruption rate**
- **User acceptance of generated patch without manual rewrite**
- **Clarity score** from human evaluation of final summaries

---

## 7. Target System Architecture

The target architecture is organized into eight major subsystems:

1. **Intent Router**
2. **Execution Planner**
3. **Agent Runtime**
4. **Tooling Layer**
5. **Verification Layer**
6. **State & Memory Layer**
7. **Interaction Layer**
8. **Evaluation Layer**

### 7.1 High-level flow

```text
User Prompt
  ↓
Intent Router
  ↓
Task Classification + Risk Scoring
  ↓
Execution Planner
  ↓
Primary Agent Runtime
  ├─ Tooling Layer
  ├─ Verification Layer
  ├─ State & Memory Layer
  └─ Optional Subagents (investigator / tester / reviewer)
  ↓
Streaming Interaction Layer
  ↓
Final Result + Evidence + Persisted Session State
```

---

## 8. Intent Router

The intent router determines how much structure a task needs.

### 8.1 Inputs

- user prompt
- repo metadata
- current session state
- permission mode
- optional CLI flags (`--model`, `--last`, workflow mode override, etc.)

### 8.2 Outputs

- task type
- risk level
- execution mode
- initial tool budget / allowed tool subset
- whether subagents should be considered

### 8.3 Task types

Recommended task classes:

1. **Q&A / explanation**
2. **Read-only review**
3. **Localized bug fix**
4. **Targeted refactor**
5. **Feature implementation**
6. **Exploratory debugging**
7. **Large architectural change**
8. **Session resume**

### 8.4 Risk scoring dimensions

- number of likely files touched
- whether auth/security/data migration is involved
- whether destructive commands may be needed
- whether task wording is ambiguous
- whether repo or language is unfamiliar in current session
- whether prior attempts failed

### 8.5 Mode selection policy

Recommended default routing:

- **Simple + low ambiguity** → direct execution
- **Medium complexity** → brief plan + execution
- **High ambiguity** → interview or clarification first
- **High risk / multi-area** → staged execution with reviewer pass
- **Large task** → execution plan with subagents

The user can still override mode explicitly, but the default should be smart.

---

## 9. Execution Planner

The execution planner converts the routed task into an explicit, minimal execution structure.

### 9.1 Planner responsibilities

- define objective and completion criteria
- identify likely files or subsystems
- choose search/read strategy
- decide whether to edit immediately or investigate first
- decide verification scope
- decide whether to spawn subagents

### 9.2 Planning granularity

Plans should be compact and task-sized.

For simple fixes:
- identify implementation file
- identify test file
- define verification command

For medium tasks:
- list 3–7 steps max
- indicate checkpoints
- identify fallback if assumption fails

For large tasks:
- split into milestones with persisted status

### 9.3 Adaptive replanning

Replanning should occur only when:
- a key assumption is disproven
- verification fails in an unexpected way
- discovered architecture differs materially from expectation
- context changes after human instruction

Replanning should not happen on every turn. Excessive replanning increases latency and drift.

---

## 10. Agent Runtime

The agent runtime is the core engine. It should behave like a disciplined loop with strong observability.

### 10.1 Core loop

Recommended loop:

1. Check session state and constraints
2. Update task state / current objective
3. Request model output with current tools and context
4. Stream thought/action events to UI
5. Execute tool calls under scheduler and permissions
6. Record results into structured state, not just raw conversation
7. Trigger verification when edit boundaries or milestones are reached
8. Decide whether task is complete, needs repair, or needs escalation
9. Repeat until completion or bounded stop condition

### 10.2 Completion criteria

A task should be considered complete only when all applicable conditions hold:

- requested change appears implemented
- relevant tests were added or updated when needed
- verification policy succeeded or was blocked for a stated reason
- review pass found no blocking issues or operator accepted remaining risk

### 10.3 Stop reasons

The runtime should distinguish:

- success
- blocked_by_missing_info
- blocked_by_permissions
- blocked_by_external_dependency
- verification_failed
- iteration_limit
- user_interrupted
- unrecoverable_tool_error

This distinction should be persisted and shown in summaries.

### 10.4 Bounded autonomy

Autonomy should be aggressive on safe local actions and more conservative on risky actions.

For example:
- safe: read/search, local tests, adding tests, local edits
- cautious: package-wide refactors, dependency upgrades
- guarded: database migrations, external API calls, destructive shell commands

---

## 11. Tooling Layer

The tooling layer is one of altcode’s biggest leverage points.

### 11.1 Tool taxonomy

Tools should be treated by action class, not just by name:

1. **Discovery tools**
   - grep
   - glob
   - ls
   - symbol/index tools

2. **Read tools**
   - read
   - fetch docs / web when allowed
   - future: semantic code map queries

3. **Edit tools**
   - edit
   - write
   - apply_patch

4. **Execution tools**
   - bash
   - test/build wrappers
   - future sandbox-aware command runner

5. **External capability tools**
   - MCP tools
   - LSP tools
   - web research

### 11.2 Scheduling policy

Current concurrency partitioning is a good base. The design should strengthen it with policy-aware scheduling.

Recommended policy:
- **parallelize read-only tools aggressively** when independent
- **serialize filesystem writes** unless explicitly patch-batched
- **serialize side-effecting shell actions by default**
- **prioritize low-latency discovery tools early**
- **allow speculative prefetch of likely files**

### 11.3 Speculative prefetch

A world-class agent should issue likely-safe reads in parallel when confidence is high.

Example:
Prompt mentions “auth middleware tests failing.”

The runtime can often safely and immediately:
- grep for `auth`, `middleware`
- search for related tests
- list candidate directories

This reduces dead time waiting for serial exploration.

### 11.4 Edit reliability policy

Use a tiered editing strategy:

1. **Exact edit** when region is known and unique
2. **Apply patch** for larger or multi-file changes
3. **Rewrite file** only when file is small or full rewrite is justified
4. **Fallback re-read + narrower patch** when edit fails

### 11.5 Tool result normalization

Tool outputs should be normalized into structured evidence objects.

Examples:
- grep results → file path + line snippets + match count
- build/test results → status + package + failing symbols + key diagnostics
- edits → file path + hunk summary + success/failure + verification trigger

This makes the runtime more robust than passing raw text around.

### 11.6 Tool permissions and restrictions

Tool subsets should be easy to impose per context:
- review mode: read/search/bash limited to safe commands
- test-writer agent: read/edit/write, no network
- reviewer agent: read/search only
- planner agent: discovery tools only

---

## 12. Verification Layer

This is the highest-ROI area for making altcode world-class.

### 12.1 Verification philosophy

Every code-changing task should create or update evidence.

The goal is not “run the biggest command always.” The goal is to use a **verification ladder**:

1. narrowest useful check first
2. then broader confidence checks
3. stop early if a blocking failure reveals the next repair task

### 12.2 Verification ladder

Recommended default ladder:

1. **Syntax / parse confidence**
   - formatter or parser if available
2. **Targeted unit tests**
   - nearest impacted tests
3. **Package tests**
4. **Project build / typecheck**
5. **Optional lint/static analysis**
6. **Optional reviewer pass**

### 12.3 Automatic test policy

For code changes, default expectation should be:
- add or update tests unless task is docs/config-only or repo genuinely lacks tests

Test guidance:
- one test per distinct behavior class
- include happy path
- invalid/missing input
- edge cases
- concurrency if applicable

### 12.4 Verification triggers

Verification should trigger:
- after Go file edits
- after test file creation
- at milestone boundaries in large tasks
- before final completion
- after reviewer-requested fixes

### 12.5 Reviewer/evaluator pass

Before final success on medium/high complexity tasks, run a reviewer pass.

Reviewer responsibilities:
- inspect changed files
- inspect tests and verification evidence
- look for correctness holes, security issues, maintainability concerns
- identify missing tests or suspicious assumptions

This can be:
- same model with different system prompt and restricted tools, or
- different model for adversarial diversity

### 12.6 Confidence reporting

Final output should include:
- verification run
- tests added/updated
- whether reviewer pass ran
- known limitations / remaining uncertainty

---

## 13. Failure Handling and Recovery

World-class agents are defined by recovery behavior.

### 13.1 Failure classes

The runtime should classify failures into explicit categories:

1. **Search failure** — target not found
2. **Read failure** — inaccessible or large/unexpected file
3. **Edit failure** — ambiguous or stale match
4. **Patch failure** — hunk mismatch or malformed patch
5. **Build failure** — compile/type error
6. **Test failure** — assertion failure, panic, timeout
7. **Tool transport failure** — provider/tool RPC issue
8. **Permission denial**
9. **Context insufficiency** — missing architecture understanding
10. **Ambiguity** — task lacks needed constraints

### 13.2 Recovery policies

#### Search failure
- broaden grep pattern
- check neighboring directories
- use symbol index if available
- ask user only if search remains inconclusive

#### Edit failure
- re-read exact file region
- switch to apply_patch if edit was brittle
- if multiple matches, narrow context automatically

#### Build failure
- parse diagnostics into structured issues
- localize likely source file and symbol
- run targeted fix loop
- avoid broad rework until diagnostics are explained

#### Test failure
- classify existing regression vs introduced regression
- inspect failing test source
- rerun narrowest possible command if failure is noisy
- if flaky suspicion, record and surface it

#### Provider/tool failure
- retry idempotent requests once
- downgrade to alternative compatible format if possible
- preserve current state so operator can resume

#### Ambiguity
- ask one compact clarifying question if decision materially affects implementation
- otherwise choose a conservative default and state it

### 13.3 Loop prevention

The runtime should detect repetitive loops such as:
- same command repeated with same result
- same edit target attempted multiple times unsuccessfully
- alternating between two hypotheses without new evidence

Loop breaker actions:
- summarize current evidence
- force replan
- switch to reviewer/investigator subagent
- ask human for clarification if ambiguity persists

---

## 14. Multi-Agent Strategy

Altcode should use multiple agents selectively, not theatrically.

### 14.1 When to use subagents

Use subagents when at least one is true:
- task spans multiple subsystems
- implementation and review can be parallelized
- architecture discovery is expensive
- verification requires specialized reasoning
- main loop is stalled or uncertain

### 14.2 Canonical agent roles

#### Investigator
Purpose:
- discover architecture
- identify likely files
- summarize constraints and patterns

Tools:
- read/search/index only

#### Implementer
Purpose:
- make code changes
- keep diffs focused

Tools:
- read/edit/write/apply_patch/bash limited to safe verify commands

#### Test Writer
Purpose:
- add and improve tests
- expand edge-case coverage

Tools:
- read/edit/write/bash limited to test commands

#### Reviewer
Purpose:
- challenge assumptions
- find bugs, security issues, style issues, missing tests

Tools:
- read/search and optionally diff inspection only

#### Verifier
Purpose:
- run/build/test and summarize diagnostics

Tools:
- safe execution tools only

### 14.3 Coordination model

Recommended pattern:
- parent agent owns objective and final synthesis
- child agents produce evidence packets, not free-form chat only
- evidence packets include:
  - findings
  - recommended action
  - confidence
  - file references

### 14.4 Cost discipline

Subagents should not run by default on trivial tasks. The router/planner should gate them based on expected value.

---

## 15. State, Memory, and Resume

This is another major opportunity.

### 15.1 State layers

Altcode should distinguish four state layers:

1. **Raw conversation history**
2. **Structured task state**
3. **Decision ledger**
4. **Long-term memory**

### 15.2 Structured task state

The structured task state should contain:
- objective
- task type
- current mode
- plan steps and statuses
- changed files
- tests added/changed
- verification results
- current blockers
- stop reason if any

### 15.3 Decision ledger

The decision ledger should record key engineering decisions:
- chosen implementation approach
- rejected alternatives
- assumptions made
- why those assumptions were chosen
- known risks

This is what prevents re-litigating the same choice after compaction or resume.

### 15.4 Verification ledger

Persist:
- commands run
- key pass/fail result
- failing tests
- build diagnostics summary
- timestamp / step relation

### 15.5 Resume behavior

`--last` or `--session` should restore not just text history, but a semantic working state:
- what was being done
- what succeeded
- what failed
- what remains
- recommended next action

### 15.6 Long-term memory

Long-term memory should be reserved for reusable knowledge:
- repo-specific conventions
- recurring pitfalls
- preferred commands
- architectural landmarks
- human preferences

Do not store transient step noise in long-term memory.

---

## 16. Context Management

### 16.1 Beyond token budgeting

Token budgeting is necessary but insufficient. Context management should preserve **task-relevant invariants**.

### 16.2 Preserve these during compaction

When compacting, preserve:
- objective
- accepted plan
- changed files
- unresolved failures
- decision ledger
- verification summaries
- open questions
- important user constraints

### 16.3 Drop these first

Compact away:
- stale raw tool outputs already normalized into evidence
- repeated explanations
- intermediate low-value reasoning traces
- superseded hypotheses

### 16.4 Repo map / structural memory

Introduce a lightweight repo map:
- package summaries
- symbol index
- file-to-test relationships
- notable entry points
- generated/vendor/ignored paths

This reduces repeated rediscovery cost.

---

## 17. Interaction Layer: TUI, CLI, and Headless UX

### 17.1 UX goals

The interaction layer should communicate progress without noise.

The operator should be able to answer three questions at all times:
1. what is the agent doing now?
2. why is it doing that?
3. how close is it to done?

### 17.2 Event model

Recommended event categories:
- planning
- discovery
- tool start/result/error
- edit applied
- verification started/result
- reviewer finding
- blocked state
- final summary

### 17.3 Status HUD

The HUD should surface:
- current objective
- current step
- active tools
- changed files count
- verification status
- cost/token estimate
- permission mode

### 17.4 Risk messaging

For risky actions, surface short intent text such as:
- “About to run package tests after editing auth middleware”
- “Need to modify 3 files across auth and routing; reviewer pass will run afterward”

### 17.5 Final summary template

A strong final summary should include:
- what changed
- files changed
- tests added/updated
- verification run and result
- remaining risks or follow-ups

---

## 18. Trust, Permissions, and Safety

### 18.1 Permission model

Permissions should remain a core differentiator.

Recommended action classes:
- read-only
- local write
- safe local execute
- risky local execute
- external network
- irreversible/destructive

### 18.2 Approval policy

Suggested defaults:
- read/search: auto-allow
- local edits in workspace: allow or ask depending on mode
- test/build commands: allow in coding modes
- package managers / installs / network / destructive commands: ask by default

### 18.3 Explain-before-risk

Before risky actions, provide a concise rationale.
Not a long essay; just enough to maintain trust.

### 18.4 Auditability

Every session should be reconstructible via:
- prompt
- tool calls
- key state transitions
- verification evidence
- final outcome

This matters for enterprise trust later.

---

## 19. Provider Strategy

Altcode’s provider breadth is a major advantage, but world-class behavior must remain consistent across models.

### 19.1 Model-agnostic runtime behavior

The runtime should own:
- tool scheduling policy
- verification policy
- state persistence
- recovery logic
- completion criteria

The model should not be solely responsible for these behaviors.

### 19.2 Model adaptation layer

Different providers have different quirks. The runtime should adapt per provider/model for:
- streaming behavior
- tool-call format
- patch tool support
- context window strategy
- reasoning verbosity limits
- cost/performance characteristics

### 19.3 Dynamic model routing

Longer-term, altcode can choose models by subtask:
- cheap/fast model for discovery
- strongest model for implementation or review
- local model for low-risk repo exploration

This should come after the core runtime is stable.

---

## 20. Evaluation Framework

If altcode wants to be world-class, evaluation cannot be ad hoc.

### 20.1 Required benchmark categories

1. **Issue resolution**
   - SWE-bench-style tasks
2. **Terminal workflows**
   - build/test/configure/CLI tasks
3. **Edit-heavy tasks**
   - refactors and multi-file patching
4. **Test-writing tasks**
   - bug fix + tests
5. **Recovery tasks**
   - intentionally broken intermediate states
6. **Resume tasks**
   - interrupt and continue
7. **Review tasks**
   - read-only issue finding

### 20.2 Internal eval dimensions

For each task, score:
- correctness
- verification completeness
- tests written
- latency
- number of ineffective loops
- need for human rescue
- summary clarity

### 20.3 Golden traces

Create golden traces for representative tasks showing:
- chosen mode
- first actions
- tool sequence
- verification sequence
- final summary

These traces make regressions visible.

### 20.4 Regression gates

Every major runtime change should be checked against:
- core tool-loop integration tests
- benchmark subset
- latency smoke tests
- resume-state correctness tests

---

## 21. Observability and Instrumentation

### 21.1 Required telemetry

Even local-first systems need internal observability.

Track per session:
- model/provider
- task class
- tool counts by type
- first-action timings
- edit success/failure events
- verification events
- stop reason
- token/cost summary

### 21.2 Event persistence

Store structured events so developers can answer:
- why did this task loop?
- why was verification skipped?
- why did edit reliability regress?
- which provider fails most often on tool results?

### 21.3 Privacy approach

Telemetry should default to local storage unless user opts in to remote reporting.

---

## 22. Phased Implementation Plan

### Phase 1: Core runtime excellence

Goal: make the default single-agent loop faster and more reliable.

Deliverables:
1. task classifier + mode router
2. first-action latency instrumentation
3. speculative parallel read/search policy
4. tiered edit strategy with patch fallback
5. structured verification ladder
6. failure classification + recovery policies
7. improved final summaries with evidence

Success criteria:
- lower time-to-first-tool and time-to-first-edit
- fewer edit failures
- higher verification run rate
- fewer repetitive loops

### Phase 2: Quality and trust

Goal: make altcode produce stronger code with better operator confidence.

Deliverables:
1. automatic test policy enforcement
2. reviewer/evaluator pass
3. risk-aware action messaging
4. verification ledger persistence
5. stop reason classification in summaries and sessions

Success criteria:
- more tests written per task
- higher hidden-eval pass rate
- better human ratings of summary clarity and trust

### Phase 3: State and resume intelligence

Goal: make long-running work resumable and coherent.

Deliverables:
1. structured task state persistence
2. decision ledger
3. resume with next-best-action recommendation
4. compaction preserving semantic state
5. lightweight repo map

Success criteria:
- high resume success rate
- less repeated investigation after interruption
- improved multi-step task completion

### Phase 4: Selective multi-agent orchestration

Goal: use subagents where they create measurable value.

Deliverables:
1. investigator / implementer / reviewer / verifier templates
2. evidence packet protocol
3. parent-child coordination logic
4. cost-aware routing policy

Success criteria:
- improved performance on complex multi-file tasks
- minimal latency penalty on simple tasks

### Phase 5: Benchmark leadership

Goal: create repeatable evidence that altcode is top-tier.

Deliverables:
1. curated public benchmark suite
2. golden traces
3. regression dashboards
4. model-by-model comparative reports

Success criteria:
- measurable gains on issue resolution, terminal workflows, and review tasks
- stable performance across provider backends

---

## 23. Concrete Runtime Policies

This section gives sharper recommendations suitable for implementation.

### 23.1 First-action policy

On task start:
- if prompt names specific file(s), read them immediately
- if prompt names subsystem/functionality, run grep + glob + ls in parallel on likely scope
- if task is clearly localized, do not scan unrelated directories
- aim for first tool call within one interaction step and first edit as soon as sufficient evidence exists

### 23.2 Edit policy

- prefer minimal diff
- after reading target file, edit incrementally
- after edit failure, re-read exact region before retry
- prefer apply_patch for larger or multi-file changes
- after source edits, run relevant verification automatically

### 23.3 Test policy

If code changed and tests are feasible, add them.

Required categories where applicable:
- valid input / happy path
- missing or invalid input
- edge condition
- concurrency/race condition for shared-state logic

### 23.4 Verification policy

For Go repos:
- after `.go` edit: package test or build check
- after test changes: targeted test first
- before completion: broader package/build verification

### 23.5 Review policy

Run review pass if:
- 2+ files changed
- auth/security/concurrency touched
- verification initially failed then recovered
- user asked for production-quality implementation

### 23.6 Clarification policy

Ask the user only if one of these is true:
- multiple materially different valid implementations exist
- missing info blocks safe progress
- permissions prevent necessary action
- human preference is likely more important than default convention

Otherwise, choose the most conservative reasonable assumption and continue.

---

## 24. Risks and Tradeoffs

### 24.1 More structure can increase latency

Mitigation:
- route only complex tasks into structured flows
- keep plans compact
- use parallel reads aggressively

### 24.2 More verification can increase cost and runtime

Mitigation:
- use laddered verification
- favor narrow checks first
- make reviewer pass conditional, not universal

### 24.3 Multi-agent orchestration can become expensive theater

Mitigation:
- require measurable expected value
- keep child outputs structured and concise
- benchmark before expanding usage

### 24.4 Stronger state persistence can increase implementation complexity

Mitigation:
- phase it in behind simple schemas
- persist only high-value state first
- separate raw history from semantic state

### 24.5 Provider heterogeneity can make behavior inconsistent

Mitigation:
- keep the runtime policy model-agnostic
- isolate provider-specific quirks in adapters
- use evals to catch regressions by provider

---

## 25. Open Questions

1. Should reviewer/evaluator default to same model or a second model when available?
2. How much internal reasoning should be surfaced in the TUI versus only action summaries?
3. Should verification run through specialized tools instead of generic bash for better structure?
4. How aggressively should altcode auto-route into workflow modes without explicit user request?
5. What local schema should represent structured task state and decision ledgers?
6. When should long-term memory write be automatic versus explicit?
7. What benchmark subset should be public and reproducible in CI?

---

## 26. Recommended Near-Term Priorities

If only a small number of changes can be made soon, prioritize these in order:

1. **First-action latency instrumentation and policy**
2. **Structured verification ladder**
3. **Failure classification and deterministic recovery**
4. **Automatic test policy**
5. **Improved final summaries with evidence**
6. **Structured task state for resume**
7. **Reviewer pass for medium/high complexity tasks**
8. **Lightweight repo map / symbol-aware discovery**
9. **Selective subagent templates**
10. **Public benchmark harness and golden traces**

These changes compound. Together they move altcode from “capable tool harness” to “high-performance engineering runtime.”

---

## 27. Conclusion

Altcode already has something rare: a serious harness architecture in a compact, local-first CLI. The path to becoming world-class is not feature sprawl. It is disciplined integration.

Altcode should evolve around a few decisive ideas:
- act fast
- verify continuously
- recover deliberately
- remember decisions
- earn trust through transparency
- measure everything that matters

If these principles shape the runtime, tooling policy, state model, and evaluation loop, altcode can plausibly become one of the strongest open coding agents in the world: not because it depends on one magical model, but because its **system behavior** makes many models perform better.

That is the real moat.
