# Design Thinking Frameworks

Three reasoning frameworks that must be applied before and during every design
decision — whether architecture, UI, API surface, or code structure.

## 1. Socratic Questioning

Challenge every design decision with progressive questioning. Never accept
"that's how it's usually done" as justification.

### The five question types

**Clarifying** — What exactly do you mean? Can you give an example?
- "What problem does this component actually solve?"
- "Who is the user and what are they trying to do right now?"
- "What do you mean by 'clean design'? Clean for whom?"

**Probing assumptions** — Why do you believe that? What if the opposite were true?
- "Why does this need to be a modal? What if it were inline?"
- "We assume users read left-to-right — is that true for this audience?"
- "What if we removed this feature entirely? Who would notice?"

**Probing evidence** — How do you know? What data supports this?
- "What evidence shows users need this setting?"
- "Has this pattern been tested with real users or is it cargo-culted?"
- "Where in the analytics does this assumption hold?"

**Exploring alternatives** — What's another way? What would X do?
- "What would this look like with zero configuration?"
- "How would a newspaper solve this information hierarchy problem?"
- "What if we used progressive disclosure instead of tabs?"

**Examining consequences** — What follows from this? What are the second-order effects?
- "If we add this option, what else needs to change?"
- "This simplifies the happy path — what happens to error states?"
- "If this scales to 10x users, does the design still hold?"

### Applying to design reviews

Before scoring any design criterion, ask at least one question from each
category. If the design can't withstand basic Socratic questioning, it hasn't
been thought through — regardless of how polished it looks.

## 2. First Principles Thinking

Decompose every design problem to its fundamental truths, then rebuild.
Strip away convention, pattern libraries, and "best practices" until you
reach irreducible requirements.

### The decomposition process

**Step 1: Identify the real constraint.**
Not "we need a sidebar nav" but "users need to switch between 5 sections
without losing context." The constraint is context-preserving navigation,
not a sidebar.

**Step 2: List the physical/cognitive truths.**
- Humans can hold 4±1 items in working memory (Miller's Law, updated)
- Fitt's Law: larger, closer targets are faster to hit
- The eye reads in F-patterns on unfamiliar pages, Z-patterns on familiar ones
- Cognitive load has three types: intrinsic, extraneous, germane
- Attention is scarce; every element competes for it

**Step 3: Rebuild from truths, not templates.**
Given the constraint + truths, what's the simplest design that satisfies both?
This might match a known pattern (great, it's validated) or it might not
(also fine — patterns are conventions, not laws).

### Common first-principles decompositions

| Convention | First-principles question | Possible insight |
|-----------|--------------------------|-----------------|
| Hamburger menu | "Do users need all nav items equally?" | Maybe 2 items need prominence, rest can be discoverable |
| Settings page | "What if nothing needed configuration?" | Sensible defaults eliminate 80% of settings |
| Dashboard | "What one thing does the user check first?" | Maybe a single metric + drill-down beats a grid |
| Loading spinner | "What can we show immediately?" | Skeleton screens reduce perceived wait time |
| Confirmation dialog | "Is the action actually destructive?" | Undo is better than "are you sure?" |
| Pagination | "Why can't we show everything?" | Virtual scrolling, filtering, or better search |

### The "5 Whys" variant for design

1. Why does this screen exist? → "To show user activity."
2. Why does the user need to see activity? → "To know what happened."
3. Why don't they already know? → "They weren't online."
4. Why does being offline mean missing context? → "No notifications."
5. Why not just fix notifications? → **Root cause found.**

## 3. Occam's Razor

The simplest design that satisfies all requirements is the best design.
Every element, interaction, and state must justify its existence.

### Application rules

**If two designs solve the same problem, choose the simpler one.**
Not simpler to build — simpler to understand and use. A single well-designed
screen beats three "intuitive" ones.

**Every element must earn its place.**
- Does this label add information the input placeholder doesn't?
- Does this icon communicate faster than text?
- Does this animation help orientation or just delay the user?
- Does this border/divider clarify grouping or just add noise?

**Count the concepts.**
A design's complexity = the number of distinct concepts a user must learn.
Fewer concepts = simpler, even if there's more on screen. A list of 20 items
with one interaction model is simpler than 5 items with 5 interaction models.

**Prefer removing over adding.**
When a design doesn't work, the instinct is to add — a tooltip, a label, a
help section. First ask: what can I remove to make this unnecessary?

### The simplicity test

For any proposed design, answer:
1. Can I remove an element without losing function? → Remove it.
2. Can I merge two elements into one? → Merge them.
3. Can I replace a custom pattern with a platform convention? → Replace it.
4. Can I make this work with one fewer click? → Do it.
5. Would a new user understand this in 5 seconds? → If not, simplify.

If you reach a point where removing anything breaks the design, you've
found the Occam optimum.

## Combining the Three

Use them in sequence during design evaluation:

```
1. SOCRATIC QUESTIONING     → Surface hidden assumptions
   "Why does this exist? What if we didn't?"

2. FIRST PRINCIPLES         → Decompose to real constraints
   "What are the actual truths? Rebuild from them."

3. OCCAM'S RAZOR            → Choose the simplest valid solution
   "Is this the minimum that satisfies all constraints?"
```

When evaluating existing designs, use them as a scoring lens:

| Framework | What it catches | Score impact |
|-----------|----------------|-------------|
| Socratic | Unexamined assumptions, cargo-culted patterns | -2 per undefended assumption |
| First Principles | Over-engineered solutions, convention worship | -2 per unnecessary layer |
| Occam's Razor | Gratuitous complexity, feature creep | -1 per element that can't justify itself |
