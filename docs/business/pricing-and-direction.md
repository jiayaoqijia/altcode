# Altcode: Product Direction & Monetization Strategy

## Executive Summary

Altcode pivots from "AI coding CLI" to **shared workspace orchestrator** — a git-native coordination layer where multiple AI agents (Claude Code, Codex, OpenCode, Cline) collaborate on codebases with shared context, session persistence, and GitHub-native workflows.

The core product is free and open source. Revenue comes from cloud persistence, team features, workflow marketplace, and outcome-based pricing.

---

## Market Landscape (April 2026)

### Competitors & Positioning

| Tool | Model | Revenue | Users | Key Strength |
|---|---|---|---|---|
| Claude Code | Subscription ($20-200/mo) | Anthropic-backed | Millions | Best single-agent coding |
| Codex CLI | Subscription (ChatGPT sub) | OpenAI-backed | Millions | GPT-5.4 + sandbox |
| Cursor | Subscription ($20-200/mo) | ~$100M+ ARR | 500K+ | IDE integration |
| GitHub Copilot | Subscription ($10-39/mo) | Microsoft-backed | Millions | Deepest GitHub integration |
| Cline | Free (BYOK) | VC-funded, pre-revenue | 100K+ | Open source, agent-agnostic |
| Cline Kanban | Free (open source) | $0 | New | Multi-agent kanban board |
| Multica | Open source | Pre-revenue | New | PM + agent platform |
| Linear Agent | Subscription (Linear pricing) | $50M+ ARR | Enterprise | Full PM context + agent |
| Devin | Usage-based | Cognition-backed | Enterprise | Fully autonomous agent |
| **Altcode** | **Open source + cloud tiers** | **$0 → target $1M+ ARR Y2** | **New** | **Git-native multi-agent workspace** |

### Market Trends

- **Gartner**: 40% of enterprise SaaS spend will shift to usage/outcome-based pricing by 2030
- **Linear CEO**: "Issue tracking is dead" — agents manage + execute, humans steer
- **75%** of Linear enterprise workspaces have coding agents installed
- **25%** of new Linear issues are agent-created (5x growth in 3 months)
- Cursor introduced usage multiplier tiers (1x/3x/20x) replacing request counts
- Enterprise vendors raising prices 20-30% for AI compute costs

### The Opportunity Gap

Nobody is doing **git-native agent coordination**:
- Cline Kanban has worktrees but coordinates via kanban board
- Multica has agent backends but coordinates via task queue
- Linear has context but coordinates via PM interface
- Altcode can coordinate via **git itself** — branches, commits, PRs, merge

---

## Product Direction: Shared Workspace Model

### Core Concept

A **shared workspace** where multiple AI agents collaborate on the same codebase with:
- Git worktree isolation per agent (own branch, shared .git)
- Shared context directory (`.altcode/workspace/`)
- Real-time visibility into each agent's progress
- Turn checkpoints for rollback
- GitHub-native coordination (branches → PRs → reviews → merge)

### Architecture

```
User: altcode workspace "add auth system"

.altcode/workspace/
├── session.json              # workspace metadata, task description
├── context.md                # shared notes, decisions (agents read+write)
├── agents/
│   ├── architect.json        # {branch, session_id, status, last_commit}
│   ├── implementer.json      # {branch, session_id, status, last_commit}
│   └── reviewer.json         # {branch, session_id, status, last_commit}
└── checkpoints/              # per-turn worktree snapshots

Git branches:
  main
  ├── altcode/architect/auth-design      (claude worktree)
  ├── altcode/implementer/auth-impl      (codex worktree)
  └── altcode/reviewer/auth-review       (claude worktree)
```

### Two Modes

1. **`/workspace "task"`** — Lightweight, git-native, agents coordinate via repo state
2. **`/workflow ship-feature "task"`** — Structured phases for complex tasks (existing system)

### Key Differentiators

| vs. | Altcode advantage |
|---|---|
| Cline Kanban | Automated orchestration (not manual cards) |
| Multica | No server required, workflow definitions |
| Linear Agent | Open source, any model, local-first |
| Devin | Transparent (you see everything), multi-agent |
| Cursor/Copilot | Multi-agent, not locked to one IDE |

### The Real Moat (not the workspace architecture)

The shared workspace is a feature, not a moat. Anthropic could ship `/workspace` in a point release. The actual defensible moats are:

1. **Community workflow library.** If 10,000 workflow definitions exist on altcode's marketplace, that is a switching cost no competitor can replicate quickly. This should be the PRIMARY strategic investment, not an afterthought.

2. **Provider-agnostic credential chain.** Altcode auto-detects Claude Code sub + Codex sub + 8 provider API keys + OpenRouter. Each vendor changes auth schemes constantly. Maintaining this across 13+ providers is genuinely painful to replicate.

3. **Chinese provider depth.** DeepSeek, Qwen, Zhipu, Moonshot, MiniMax — none of the Western competitors have production-grade support. This is an entire market segment (China + SEA) that altcode owns uniquely.

4. **Workflow portability.** YAML workflow definitions are provider-agnostic. A "security audit" workflow runs on claude today, codex tomorrow, deepseek next week. No other tool offers this.

---

## Monetization: 5 Tiers

### Tier 0: Free & Open Source (altcode core)

**Price**: Free forever

**Includes**:
- Full CLI/TUI tool
- Workflow engine with YAML definitions
- Shared workspace mode (local)
- All provider support (Claude, Codex, OpenCode, Cline, DeepSeek, Qwen, etc.)
- BYOK (bring your own API key)
- Git worktree isolation
- Split-pane TUI with live agent output

**Purpose**: Distribution layer. Build community, establish standard.

### Tier 0.5: altcode Free+ (Freemium with usage cap)

**Price**: Free

**Adds over Tier 0**:
- 50 agent-turns/month (enough for ~5 workflow runs)
- Basic cloud workspace sync (1 workspace, 7-day history)
- GitHub integration (read-only — link issues to workspaces)

**Purpose**: Lower acquisition friction. Users experience cloud value before hitting the paywall.

### Tier 1: altcode Cloud — Workspace Persistence ($19/mo per seat)

**Price**: $19/month per developer

**Adds**:
- Unlimited agent-turns
- Persistent shared workspaces (accessible from any machine)
- Cloud-synced session history + turn checkpoints
- Workspace state survives terminal close/machine restart
- GitHub/GitLab integration (auto-create PRs, link to issues)
- Session resume from any device
- 30-day workspace history
- $0.25/agent-turn overage if on Free+ cap

**Why users pay**: Cross-machine resume, turn checkpoints with rollback ("undo any agent action"), GitHub PR auto-creation.

**Target**: Individual developers who use altcode daily. Priced below Cursor Pro+ ($39) and above Copilot Pro ($10).

### Tier 2: altcode Teams ($49/mo per seat)

**Price**: $49/month per developer (10-seat team = $490/mo vs Copilot Enterprise $600/mo)

**Adds everything in Cloud, plus**:
- Team workspace sharing — see all agents across the org
- Shared workflow library with versioning
- Agent budget controls (token/cost limits per agent, per workflow, per team member)
- Full audit log (who ran what, what changed, cost breakdown)
- Approval gates — require human review before agent PRs merge
- SSO + role-based access control
- Priority support

**Why teams pay**: Enterprises need controls, auditing, cost governance, and coordination.

**Target**: Engineering teams (5-50 developers) using AI agents daily.

### Tier 3: Workflow Marketplace ($5-50 per workflow)

**Price**: Per-workflow purchase, one-time or subscription

**Examples**:
- "Django migration" workflow ($15)
- "React component extraction" workflow ($10)
- "Security audit" workflow — runs 5 specialized agents ($50)
- "Monorepo dependency update" workflow ($25)
- "API versioning" workflow ($20)

**Revenue split**: 70% creator / 30% altcode

**How it works**:
1. Community creates workflow YAML definitions
2. Altcode reviews and tests for quality
3. Published to marketplace
4. Users install with `altcode install workflow security-audit`

**Why users pay**: Writing good workflow definitions is hard. Pre-built, tested workflows save hours and encode expert knowledge.

**Target**: All altcode users. Low friction, high volume.

### Tier 4: altcode Enterprise — Outcome-Based + On-Premise

**Price**: Hybrid — base seats ($49/user/mo) + outcome bonus ($2-10/qualified PR)

**Includes everything in Teams, plus**:
- Outcome-based pricing component: charge per "Qualified PR" (defined below)
- CI/CD integration — auto-verify agent work
- On-premise / air-gapped deployment ($50K-$200K/year license)
- Custom workflow development
- Dedicated support engineer
- SLA with uptime guarantees

**Qualified PR definition** (contractual):
> A "Qualified PR" is a pull request created by an altcode-orchestrated agent that:
> (a) passes all CI checks configured in the repository at time of creation,
> (b) receives at least one human approval, and
> (c) is merged to the target branch within 14 days.
> PRs that are reverted within 48 hours of merge are not charged.

**Why enterprises pay**: Ties cost to value. CFOs budget per-output not per-seat. The contractual precision removes procurement friction.

**Target**: Large engineering orgs (50+ developers).

### Tier 5: altcode CI — Programmatic/Headless Access

**Price**: $0.10 per agent-turn (metered)

**For**: CI/CD pipelines running altcode headlessly (GitHub Actions, GitLab CI)
- `altcode exec --json "fix lint errors"` in CI
- `altcode workflow run review` as a PR check
- Metered billing per 1,000 agent-turns
- Volume discounts at 10K+ turns/month

**Why pay**: Different use case than interactive dev. CI runs thousands of automated agent tasks. Metered is fairer than per-seat.

**Target**: DevOps teams automating code quality with agents.

---

## Competitive Pricing Comparison (April 2026)

### Detailed Competitor Breakdown

| Tool | Free | Solo | Team | Enterprise | Model |
|---|---|---|---|---|---|
| Claude Code | Limited | $20/mo Pro | N/A | $200/mo Max 20x | Token quota per 5h window |
| Codex CLI | Free (ChatGPT sub) | $20/mo | — | — | Subscription |
| Cursor | Free tier | $20/mo Pro | $200/mo Ultra | Custom | Usage multipliers (1x/3x/20x) |
| GitHub Copilot | Free (2K completions) | $10-39/mo | $19/user | $39/user (+$21 GH Enterprise) | Premium requests + $0.04 overage |
| Windsurf | Free | $20/mo | $40/user | $200/mo Max | Daily/weekly quotas |
| Augment Code | — | $20/mo (40K credits) | $60/user (130K credits) | Custom | Credit-based |
| Devin | — | $20/mo Core | $500/mo Team (250 ACUs) | Custom VPC | Agent Compute Units |
| Factory AI | — | $20/mo | Custom | Custom | Droids (task-based) |
| Cline | Free (BYOK) | Free | Free | Free | Open source |
| Cline Kanban | Free | Free | Free | Free | Open source |
| Multica | Free | Free | Free | Free | Open source |
| Linear + Agent | — | $8/mo | $8/user | Custom | PM subscription |
| **altcode** | **Free (BYOK)** | **$29/mo Cloud** | **$99/user Teams** | **$2-10/PR** | **Hybrid** |

### Key Pricing Insights

**The $20/mo floor**: Devin, Cursor, Windsurf, Augment, Factory all converge at $20/mo for solo. Devin's slash from $500 to $20 signals a race to the bottom for single-agent tools.

**The $200/mo ceiling**: Claude Max 20x, Cursor Ultra, Windsurf Max all hit $200/mo for power users. This is the "serious developer" price point.

**Outcome-based is emerging but not yet in coding**: Sierra.ai charges per resolved conversation ($0.50). HubSpot Breeze charges $0.50/resolution. Intercom Fin charges $0.99/resolution. But NO coding tool charges per-PR or per-commit yet. **This is an open opportunity.**

**The enterprise gap**: GitHub Copilot Enterprise is $60/user/mo (Copilot + GH Enterprise Cloud). Devin Team is $500/mo. There's room between these for team orchestration at $99/user.

**Altcode's position**: 
- Cheaper than Devin Team ($99 vs $500) for multi-agent orchestration
- More capable than Cline Kanban (automated workflows vs manual cards) at the same free tier
- Uniquely outcome-based for enterprise (no competitor does per-PR pricing)
- The $29/mo Cloud tier is above Copilot ($10) but below Cursor Pro+ ($39) — justified by multi-agent + persistence

---

## Revenue Projections

### Conservative Scenario (realistic for CLI tools — <2% conversion typical)

| | Y1 | Y2 | Y3 |
|---|---|---|---|
| **Total users** | 5,000 | 25,000 | 100,000 |
| **Free+ (freemium)** | 4,900 | 23,500 | 92,000 |
| **Cloud @ $19/mo** | 75 (1.5%) = $17K | 750 (3%) = $171K | 4,000 (4%) = $912K |
| **Teams @ $49/mo** | 0 | 150 seats = $88K | 1,500 seats = $882K |
| **Marketplace** | $0 | $30K | $300K |
| **CI metered** | $0 | $20K | $200K |
| **Enterprise** | $0 | $0 | $500K |
| **Total ARR** | **$17K** | **$309K** | **$2.8M** |

### Moderate Scenario (comparable to aider/lazygit trajectory)

| | Y1 | Y2 | Y3 |
|---|---|---|---|
| **Total users** | 10,000 | 50,000 | 200,000 |
| **Total ARR** | **$46K** | **$850K** | **$8M** |

### Aggressive Scenario (viral moment + VC marketing spend)

| | Y1 | Y2 | Y3 |
|---|---|---|---|
| **Total users** | 20,000 | 100,000 | 500,000 |
| **Total ARR** | **$100K** | **$2.5M** | **$20M** |

Note: CLI tool conversion rates are historically low (aider, lazygit: sub-2%). The freemium tier and team focus are designed to push conversion higher. The marketplace revenue compounds — once 1,000+ workflows exist, it becomes self-sustaining.

---

## Implementation Roadmap

### Phase 1: Foundation (Month 1-2)
- Ship shared workspace mode (`/workspace` command)
- Git worktree per agent
- Turn checkpoints
- Session persistence to disk
- Open source release

### Phase 2: Cloud (Month 3-4)
- Build altcode Cloud backend (workspace sync)
- GitHub/GitLab integration
- Billing system (Stripe)
- Launch Tier 1 ($29/mo)

### Phase 3: Teams (Month 5-6)
- Multi-user workspace sharing
- Audit log
- Budget controls
- SSO
- Launch Tier 2 ($99/mo)

### Phase 4: Marketplace (Month 7-8)
- Workflow publishing system
- Review + quality testing pipeline
- Creator dashboard
- Launch Tier 3

### Phase 5: Enterprise (Month 9-12)
- Outcome-based pricing engine
- CI/CD integration for PR verification
- On-premise deployment
- Enterprise sales motion
- Launch Tier 4

---

## Key Metrics to Track

| Metric | Target (Y1) | Target (Y2) |
|---|---|---|
| GitHub stars | 5K | 25K |
| Weekly active users | 1K | 10K |
| Workflows per user/week | 3 | 10 |
| Free → Cloud conversion | 5% | 8% |
| Cloud → Teams conversion | — | 15% |
| Monthly agent PRs (enterprise) | — | 10K |
| NPS | 50+ | 60+ |

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Claude Code / Codex add orchestration natively | Real moat is workflow library + provider-agnosticism, not workspace architecture |
| Cline Kanban gains traction | Altcode differentiates on automated phased workflows + Chinese provider support |
| Low paid conversion (<2% typical for CLI tools) | Freemium tier lowers friction; focus on teams (higher conversion) not individuals |
| Agent quality unreliable | Turn checkpoints + rollback + Qualified PR definition for enterprise |
| Enterprise sales cycle long | Developer-led adoption → team → enterprise. Bottom-up. |
| **API cost absorption** | **BYOK model means altcode never touches API costs. If we ever move to bundled inference, margin squeeze is existential. Stay BYOK; monetize orchestration value, not inference.** |
| Workspace feature copied by Anthropic/OpenAI | Expected. The moat is the 10K+ community workflows and 13-provider auth chain, not the architecture. |
| Per-PR pricing gaming | Qualified PR definition requires CI pass + human approval + 48h stability. Hard to game. |

---

## Cloud VM Execution: Build or Skip?

### The Question
Should altcode offer cloud VMs to run coding agents, like Devin and Jules do?

### How Competitors Do It

| Tool | Execution Model | Cloud VM? | Cost to Provider |
|---|---|---|---|
| **Devin** | Full cloud sandbox (IDE, shell, browser) | Yes — Cognition pays | ~$8-9/hr per agent (1 ACU ≈ 15min) |
| **Jules** | Google Cloud VM per task | Yes — Google pays | Free tier 15 tasks/day, Pro $125/mo |
| **Codex** | Isolated cloud sandbox per task | Yes — OpenAI pays | 25-50 compute units/task |
| **OpenHands** | Docker container (cloud or local) | Optional (self-host or cloud) | E2B: $0.07/CPU-hour |
| **Claude Code** | Local machine | No — runs on user's computer | $0 infrastructure |
| **Cline Kanban** | Local machine (worktrees) | No — runs on user's computer | $0 infrastructure |
| **Cursor** | Local machine | No — runs in user's IDE | $0 infrastructure |

### Infrastructure Cost Reality

**E2B (leading sandbox provider):**
- $0.07/CPU-hour + $0.04/GB-hour memory
- A 2-CPU, 4GB agent VM = ~$0.30/hour
- An agent working 8 hours/day = $2.40/day = **$72/month per concurrent agent**

**Devin's math:**
- 1 ACU ≈ 15 minutes of VM time
- Core plan: $2.25/ACU = **$9/hour of agent work**
- Real-world consumption is 2-3x higher than estimates
- Devin is almost certainly losing money on infrastructure at $20/mo Core

**Jules' math:**
- Free tier: 15 tasks/day on Google Cloud VMs
- Google subsidizes this from their cloud budget (loss leader for GCP)
- Pro: $125/mo for 100 tasks/day = ~$0.04/task (only viable at Google's scale)

### The Verdict: DON'T build cloud VMs (yet)

**Arguments against:**
1. **Margin killer.** At $0.30/hr per VM, a $19/mo Cloud tier user running agents 2hr/day costs altcode $18/mo in compute — 95% margin erosion. Devin is VC-funded and likely losing money. Google subsidizes from cloud revenue. Altcode can't.
2. **BYOK breaks.** Altcode's entire model is BYOK — user pays for LLM inference. Adding cloud VMs means altcode also pays for compute. Two cost centers instead of zero.
3. **Competitors already do it better.** Devin/Jules/Codex have billions in infrastructure. Altcode can't compete on cloud VM quality.
4. **Users have machines.** CLI tool users already have powerful dev machines. They don't need a cloud VM — they need orchestration.

**Arguments for (future consideration):**
1. **Async execution.** "Start a workflow, close your laptop, come back to a PR." This is powerful but requires cloud.
2. **CI/CD integration.** Running agents in CI pipelines needs headless cloud execution.
3. **Enterprise isolation.** Air-gapped VMs for sensitive codebases.

**Recommendation:**
- **Phase 1 (now):** Local-only. No cloud VMs. Users run agents on their own machines.
- **Phase 2 (Month 6+):** Partner with E2B or Fly.io for optional cloud execution. Charge a premium ($0.50/agent-hour markup over E2B's $0.30). Only for CI/async use cases.
- **Phase 3 (Month 12+):** If demand proves out, build own infrastructure. Only if async execution becomes the primary use case.

The key insight: **altcode's value is orchestration (which agents to run, in what order, with what context), not execution (providing the machine they run on).** Devin bundles both; altcode should unbundle and focus on orchestration.

---

## Agent Landscape Update (April 2026)

### Emerging Approaches

**Kiro (Amazon/AWS):** "Spec-driven development" — the agent and human co-create specifications before any code is written. The agent watches how the team works, learns patterns, then works independently for days. **Insight for altcode:** The spec-creation phase could be a workflow step — "design" phase that produces a spec, not just a plan.

**Jules (Google):** Fully async — clones repo to a Cloud VM, works in background, returns a PR. Audio changelogs. **Insight for altcode:** Async execution + PR output is the right UX for enterprise. Even without cloud VMs, altcode can do this locally (tmux/screen + auto-PR).

**Codex Subagents (OpenAI, March 2026):** Manager agent decomposes tasks → spawns worker agents in parallel cloud sandboxes → validates results → merges. 90% faster via container caching. **Insight for altcode:** This IS what altcode's workflow engine does, but locally. The architecture is validated by OpenAI's approach.

**OpenHands:** 87% bug resolution rate. $18.8M raised. Cloud or self-host. Web UI + multi-agent. **Insight for altcode:** Open source multi-agent is viable commercially. OpenHands proves the market exists.

### Market Numbers

- **Devin:** $73M ARR (VentureBeat)
- **Lovable:** $75M ARR, 30K+ paying users
- **OpenHands:** $18.8M raised, leading open-source agent
- **Poolside:** $3B valuation, $500M raised (enterprise-focused)

### Key Pattern: The Bifurcation

The market has split into two camps:

**Camp 1: Cloud-hosted autonomous agents** (Devin, Jules, Codex cloud)
- Fully autonomous, async, cloud VMs
- User submits task, gets back a PR
- High cost, high margin (if you're Google/OpenAI), high risk (if you're a startup)

**Camp 2: Local-first orchestration tools** (Claude Code, Cline, altcode)
- Run on user's machine, BYOK
- User watches and steers agents in real-time
- Low cost, high control, lower autonomy

**Altcode sits in Camp 2 but borrows the best from Camp 1** — automated phase-gated workflows (like Codex subagents), git-native coordination (like Jules PRs), spec-driven development (like Kiro) — all running locally with the user in control.

---

## Inspiration from Competitors

### From Devin: Agent Compute Units (ACUs)
Devin charges per-ACU — a unit measuring task complexity (planning, debugging, code execution, browser actions). This is more granular than per-seat. **Altcode could adopt**: charge per "workspace hour" instead of flat monthly — you pay for the time agents are actively working, not for idle seats.

### From GitHub Copilot: Premium Request Overage
Copilot gives a base allocation (300 requests/mo on Pro) then charges $0.04/request overage. Predictable base + metered burst. **Altcode could adopt**: $29/mo includes 100 workflow runs/month, $0.25/run overage. This is fairer than flat pricing for light users.

### From Linear Agent: Full Project Context
Linear Agent succeeds because it has the FULL project context — roadmap, backlog, customer feedback, code repos. It doesn't just execute tasks; it CREATES them. **Altcode could adopt**: integrate with GitHub Issues/Projects to give agents full project context, not just the current task prompt.

### From Sierra/Intercom: Outcome-Based Pricing
Sierra charges $0.50 per resolved conversation. Intercom Fin charges $0.99 per AI resolution. **Altcode could pioneer**: charge per merged PR ($2-10 based on complexity), per passing CI run, or per issue closed. This is the most defensible pricing because it aligns revenue with customer value.

### From Cline Kanban: Turn Checkpoints
Cline Kanban snapshots the worktree after each agent turn. This enables rollback and diff comparison per turn. **Altcode should adopt**: turn checkpoints are a premium feature for Cloud tier — "undo any agent action" is worth paying for.

### From Factory AI: Droid Specialization
Factory sells specialized "Droids" — each focused on one task (code review, test generation, migration). **Altcode could adopt**: pre-built workflow agents as marketplace items. "Security Auditor" droid = $10/mo, runs weekly security scans via workflow.

---

## Open Source Reference Projects

Six submodules studied for architecture patterns:

| Repo | Key Learnings for Altcode |
|---|---|
| **vendor/agent-orchestrator** (ComposioHQ) | **THE reference architecture.** 8-slot plugin system (Runtime/Agent/Workspace/Tracker/SCM/Notifier/Terminal/Lifecycle). Session lifecycle state machine: `spawning→working→pr_open→ci_failed→changes_requested→approved→mergeable→merged→done`. CI auto-fix loop with retry+escalation. Review comment routing back to agent. Flat file storage (no DB). Activity detection cascade (6 states). PR batch enrichment via GraphQL. Attention priority zones (red/orange/yellow/green). |
| **vendor/openhands** (65K stars, $18.8M raised) | Credit-based SaaS billing via Stripe, Docker/K8s sandboxing, microagent system (keyword-triggered specialists), nested delegation, enterprise tier with source-available licensing. **Proves the commercial model works.** |
| **vendor/cline-kanban** (Cline) | Git worktree per task, turn checkpoints (snapshot per agent turn), hooks-based coordination, session persistence in JSON files. **Best workspace model.** |
| **vendor/multica** | Backend interface (claude/codex/opencode), stream-json parsing, daemon task lifecycle, provider-aware context injection (CLAUDE.md/AGENTS.md). **Best agent backend patterns.** |
| **vendor/wshobson-agents** | 182 specialized agents + 16 orchestrators as Claude Code plugins. Sonnet+Haiku cost optimization. **Best agent definition patterns.** |
| **vendor/codex** (OpenAI) | JSON-RPC 2.0 agent protocol, context compaction, subagent architecture (manager + workers in parallel sandboxes). **Best protocol design.** |

### Key Takeaways Across All Projects

1. **Git worktrees are the standard** — agent-orchestrator, Cline Kanban, and Codex all isolate agents via worktrees. This is the proven pattern.

2. **Credit-based billing works** — OpenHands uses LiteLlm for cost tracking per user, Stripe for payments. This is the right billing architecture.

3. **Plugin/microagent systems beat monolithic agents** — OpenHands microagents, wshobson's 182 agents, agent-orchestrator's plugins all show that specialized small agents outperform one big agent.

4. **CI auto-fix is a killer feature** — agent-orchestrator automatically retries when CI fails. This turns "agent creates PR" into "agent creates PASSING PR." Worth adopting.

5. **Source-available licensing** — OpenHands uses MIT for core + proprietary for enterprise features. This is the standard model for commercial open source.

6. **Session lifecycle state machine** — agent-orchestrator's `spawning→working→pr_open→ci_failed→merged→done` is the gold standard. Altcode's current workflow has phase verdicts but no PR/CI tracking.

7. **Reaction system with escalation** — agent-orchestrator auto-fixes CI failures N times, then escalates to human. This turns "agent creates PR" into "agent creates PASSING PR." Critical for enterprise trust.

8. **Activity detection cascade** — 6 states (active/ready/idle/waiting_input/blocked/exited) with process check → native signal → JSONL fallback. Required for stuck detection and dashboard UX.

9. **PR batch enrichment** — single GraphQL query to check CI status + reviews for all active PRs. Avoids N+1 API calls. Essential at scale.

10. **Attention priority zones** — red (stuck), orange (PR ready), yellow (auto-fix failed), green (working). The dashboard only pulls humans in when needed. This is the right UX for orchestration.

---

## Summary

Altcode's moat is **git-native multi-agent coordination** — something no competitor offers. The free tier builds distribution, cloud persistence creates the upgrade path, team features create switching costs, and outcome-based pricing aligns revenue with customer value.

The shared workspace model is the strategic foundation: agents coordinate through git (the tool developers already use), not through kanban boards (a parallel system) or workflow DAGs (a rigid prescription).
