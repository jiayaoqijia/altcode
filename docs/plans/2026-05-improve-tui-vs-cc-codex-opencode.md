# altcode TUI Improvement Plan — Compete with Claude Code, Codex, OpenCode

> **Status:** v2 (autoresearch — review round 2 incoming).
>
> **Frame:** karpathy/autoresearch applied to a Go TUI codebase. Each iteration is one
> focused change that moves a single mechanical metric. `keep` if metric improves and
> all gates pass; `discard` (git revert) otherwise. Loop forever until stop conditions hit.
>
> **One command to reproduce all metrics on this commit:** `make tui-eval`.
> It emits one TSV row:
> `commit  M1_density  M2_perf_min_ratio  M3_coverage_pct  M3_covered_count  M4_latency_ms  M5_discover_pct  env_status`.
> Each metric script is independent and emits a single number.
> `env_status` is `ok` or `mismatch:<reason>` from `scripts/tui_envcheck.sh`.

## 1. Goal

Make altcode TUI **strictly better than claude-code, codex, and opencode on three axes**:
features, UI, stability. "Strictly better" means: for every mechanical metric a fresh
reviewer would reach for, altcode either matches or beats the best of the three competitors,
AND the metric is reproducible by any cloner with one make target.

## 2. Locked baseline (`scripts/baseline.json`, commit 4c99606, 2026-05-05)

| Metric | Baseline | Source-of-truth |
|---|---|---|
| M1 — feature density (cmds/KLOC, prod-only) | **5.5604** | `make tui-density` → `scripts/tui_density.sh` |
| M2 — render perf min ratio vs baseline | **1.0** (tautologically) | `make tui-perf` → `scripts/tui_perf.sh` (uses `BenchmarkUpdateViewport`) |
| M3 — `internal/tui/` line coverage % | **32.4** | `make tui-coverage` → `scripts/tui_coverage.sh` |
| M3* — covered statement count (anti-gaming) | **1263** | emitted by `tui_coverage.sh`; cached at `/tmp/tui_coverage.last` for the loop driver |
| M4 — SIGWINCH→stable-frame latency (ms) | **123** | `make tui-latency` → `scripts/tui_latency.sh` (tmux PTY) |
| M5 — discoverability % (commands with ≤3-char unique prefix) | **80.39** | `scripts/tui_discover.sh` |

ns/op baseline (locked in `scripts/baseline.json`): 10→83921, 100→676452, 500→3425001, 1000→7897849.

`scripts/baseline.json` also pins env minima: `go ≥ 1.22`, `tmux ≥ 3.2`, `node ≥ 20`,
`playwright ≥ 1.48`, `GOMAXPROCS=8`, `GOGC=100`. `make tui-eval` runs `tui_envcheck.sh`
and emits a final `env_status` column; an iteration whose `env_status` is anything other
than `ok` cannot be a KEEP.

Pinned env (also in `baseline.json`): Linux/amd64, `GOMAXPROCS=8`, `GOGC=100`. Editing
`baseline.json` requires a recorded justification row in `autoresearch-altcode.tsv`.

## 3. Metrics — formal definitions

Each metric script emits **one number on stdout**, no narration. The plan grades each
on five dimensions (mechanical, monotone, convergent, gaming-resistant, reproducible).

### 3.1 M1 — Feature density per surface KLOC

```
M1 = (count of /<feature> handlers in commands.go that are also listed in tui_features.tsv)
     / (LOC under internal/tui/ excluding *_test.go, divided by 1000)
```

**Why divide by tui_features.tsv?** It's a frozen registry of competitor features. Adding a
trivial command not in the registry doesn't move M1 — kills the "spam tiny commands to win"
attack codex flagged. Adding a registered command that competitors have but we don't *does*
move M1, which is the desired direction.

**Direction:** higher.
**Soft target:** ≥ 7.0 commands/KLOC.
**Anti-gaming invariants** (codified in `tui_density.sh`):
- Numerator counts only registry entries (frozen list).
- Denominator excludes test files (so coverage tests don't lower it).
- Discrepancies between registry and handlers print to stderr.

### 3.2 M2 — TUI render performance, **min ratio** across sizes

```
M2 = MIN over sizes ∈ {10, 100, 500, 1000} of:
       baseline_ns_per_op[size] / current_ns_per_op[size]
```

**MIN, not average.** A regression at any single size kills the win. This addresses
codex's monotonicity finding directly.

**Direction:** higher (1.0 = baseline; >1.0 = faster; <1.0 = regression somewhere).
**Soft target:** ≥ 1.5 (50% faster minimum across all sizes vs baseline).
**Anti-gaming invariants** (in `tui_perf.sh`):
- Bench is run with locked GOMAXPROCS/GOGC matching `baseline.json`.
- 5 iterations per bench, parser uses median (filter cold-start spikes).
- Baseline is committed JSON, not embedded in prose.

### 3.3 M3 — TUI line coverage with absolute-statement guard

```
M3       = pct of statements covered in internal/tui/ (parsed from `go tool cover -func`)
M3_count = absolute count of covered statements (count > 0 in the profile)
```

**Direction:** M3 higher; M3_count must not decrease.

**Why two numbers?** Coverage % can rise by deleting code. The autoresearch loop
**rejects** any iteration where M3% rises but M3_count falls — that's the deletion-game.
This addresses codex's drift finding ("M3 cannot be gamed" was wrong).

**Soft target:** M3 ≥ 50%.
**Anti-gaming invariants** (in `tui_coverage.sh` + the loop):
- Both numbers always emitted.
- Loop discards if M3↑ AND M3_count↓.
- Loop discards if M3_count drops by more than 1% even when M3 unchanged.

### 3.5 M5 — Discoverability (% of commands with ≤3-char unique prefix)

```
implemented   = registry ∩ handlers   (registered AND has /handler in commands.go)
min_prefix(c) = smallest k such that ONLY c starts with c[:k] among implemented
M5            = 100 * |{ c | min_prefix(c) ≤ 3 }| / |implemented|
```

**Why this measures usability:** in a TUI palette, k=2 means "type 2 chars then Enter
works". A command with k=5 (e.g. `/spawn` shares prefix with `/share`, `/skills`,
`/stats`, `/status`, `/stop` — yes, that's why discoverability lags) costs more
keystrokes than the user expects. Codex's r2 review specifically asked for a usability
dimension; this is the script-able one.

**Direction:** higher.
**Soft target:** ≥ 90%.
**Anti-gaming invariants** (in `tui_discover.sh`):
- Numerator AND denominator both restricted to registry-implemented commands.
- Adding a redundant alias *increases* a neighbour's min unique prefix, *lowering* M5
  — spam aliases hurt the metric.
- Adding a real new command typically lowers a neighbour's prefix length too, so M5
  movement reflects real disambiguation, not surface count.

### 3.4 M4 — Resize→stable-frame latency (addressing CC's perceived-latency gap)

```
M4 = median over 5 SIGWINCH events of:
       wall_ms from `tmux resize-window` to first 50ms of unchanged capture-pane output
```

**Direction:** lower (ms).
**Soft target:** ≤ 100 ms.
**Anti-gaming invariants** (in `tui_latency.sh`):
- Always uses tmux + real subprocess (PTY crossing) — can't be unit-mocked.
- Terminal sizes pinned (80×24 → 100..120 width sweep).
- 5 samples, take median; bound any single sample to ≤ 5000 ms (timeout).

## 4. Iteration loop

```
LOOP (until stop conditions):
  1. read autoresearch-altcode.tsv tail to see what's been tried
  2. select an experiment from §5 below targeting the metric furthest from soft target
  3. apply changes; git commit "wip: experiment <id>: <description>"
  4. run `make tui-eval` and capture row
  5. run hard gates (§6)
  6. evaluate KEEP/DISCARD per §7 rules
  7. append row to autoresearch-altcode.tsv with status
  8. if DISCARD: `git revert HEAD --no-edit`
```

## 5. Experiment queue (open; appendable)

| # | Targets | Description | Files |
|---|---|---|---|
| E1 | M1 | Convert print-only stubs into real implementations: `/theme` applies a theme; `/vim` toggles modal input; `/share` writes markdown to `~/.altcode/shared/`. Adds ≥ 12 covered statements (M3 also rises) | `internal/tui/{commands,palette,app}.go` + tests |
| E2 | M2 | Promote 6 hot lipgloss styles to package-level vars. Expected: −15% ns at 1000 msgs | `internal/tui/{messages,inlinediff}.go` |
| E3 | M2 | Pre-allocate `strings.Builder` to previous render size in `updateViewport`. Expected: −8% ns at 1000 | `internal/tui/app.go` |
| E4 | M3 | Tests for `app_keys.go` handlers (`Ctrl+J`, `Up/Down` history, `Tab`). Adds ~80 covered statements | `internal/tui/app_keys_test.go` (new) |
| E5 | M3 | Tests for `commands_init.go` (`/init` → CLAUDE.md). | `internal/tui/commands_init_test.go` (new) |
| E6 | M3 | Tests for `git_undo.go` undo/redo state machine | `internal/tui/git_undo_test.go` (new) |
| E7 | M3+M4 | teatest for `/help`, `/doctor`, `/keymap` rendering; latency assertion via tmux | extend `tmux_pty_test.go` |
| E8 | M1+M2 | Inline 6 ANSI helper closures in `palette.go` to pre-built strings; drops allocs and ~40 LOC | `internal/tui/palette.go` |
| E9 | M1 | Fold 4 single-call helpers in `helpers.go` into call sites | `internal/tui/helpers.go` |
| E10 | M2 | Replace small `map[string]bool` sets (size <32) with sorted slice + `sort.SearchStrings` | grep callers |
| E11 | M4 | Debounce viewport rewrap on rapid SIGWINCH; coalesce within 16 ms window | `internal/tui/app.go` |
| E12 | M4 | Pre-cache theme styles per terminal width to avoid per-resize reflow | `internal/tui/theme.go` |
| E13 | M3 | Tests for `messages.go` `looksLikeDiff` / `parseProvider` edge cases (CJK, empty, malformed) | `internal/tui/messages_helpers_test.go` (extend) |

The queue is open: a maintainer may add new experiments as long as each row names ONE
metric it primarily targets and predicts a numeric delta.

## 6. Hard gates (every iteration)

```
GOFLAGS=-mod=mod go build ./...                                                    # compile
GOFLAGS=-mod=mod go vet ./...                                                      # static
GOFLAGS=-mod=mod go test ./internal/... -race -count=1 -timeout=600s               # race tests

# Build the daemon binary FRESH for each iteration; do not rely on a previous
# /tmp/altcode-e2e. setup.sh skips the build when ALTFIX_BINARY is set, which
# can mask a regression. So we rebuild explicitly:
GOFLAGS=-mod=mod go build -o /tmp/altcode-e2e ./cmd/altcode/
ALTFIX_BINARY=/tmp/altcode-e2e bash internal/daemon/web/tests/e2e/setup.sh         # daemon e2e

bash scripts/tui_envcheck.sh | grep -qx ok                                         # env pinned
make tui-eval                                                                       # metrics
```

The explicit `go build -o /tmp/altcode-e2e` pre-step is **not optional** — without it,
`setup.sh` can run against a stale binary and miss a real regression. Codex round-2
flagged this; the fix is mechanical (one line in the gate sequence).

## 7. KEEP / DISCARD rule

An iteration is **kept** iff ALL of the following hold:

1. All hard gates passed (§6), including `env_status == ok`.
2. At least one of M1, M2, M3, M4, M5 improved by **≥ T_metric** vs the previous KEEP row,
   where T = 5% for M1/M3/M4/M5 and **T = 8%** for M2 (the noisy GC-affected metric).
3. None of M1, M2, M3, M4, M5 regressed by **> R_metric**, where R = 2% for M1/M3/M4/M5
   and **R = 5%** for M2 (matching §11's acknowledged GC drift).
4. M3_count did not decrease (deletion-game guard).
5. The diff size is bounded: ≤ 400 LOC added, ≤ 20 files touched.

The asymmetric thresholds for M2 are deliberate: GC noise can swing a single ratio by
up to 5% (§11), so a 5% keep threshold would falsely accept noise wins. 8% sits one
σ above documented noise — a real performance improvement clears it; jitter does not.

A simplification-only iteration (no metric movement) is kept iff:

- Hard gates pass AND
- LOC strictly decreases AND
- M3_count does not decrease AND
- All other metrics stay within ±2%.

## 8. Stop conditions

- All five metrics ≥ soft targets:
  M1 ≥ 7.0, M2 ≥ 1.5, M3 ≥ 50%, M4 ≤ 100 ms, M5 ≥ 90%.
- OR 100 iterations completed.
- OR 15 consecutive discards (queue exhausted, escalate to a plan review).
- OR user interrupts.

## 9. Reproducibility statement

Anyone who clones the repo can run `make tui-eval` on any commit and reproduce M1, M2,
M3, M3_count, M4, M5 within ±2% (M2 ±5%) under the pinned env. The TSV log
(`autoresearch-altcode.tsv`) records every iteration, including discards. The frozen
evaluator is the union of:

```
scripts/tui_density.sh           # M1
scripts/tui_perf.sh              # M2 (5×1s benchruns, median per size)
scripts/tui_coverage.sh          # M3 + M3_count
scripts/tui_latency.sh           # M4 (tmux PTY)
scripts/tui_discover.sh          # M5
scripts/tui_envcheck.sh          # env validation
scripts/tui_eval.sh              # top-level harness
scripts/baseline.json            # locked numbers + env minima
tui_features.tsv                 # frozen feature registry
```

These files are the contract. Edits require an `autoresearch-altcode.tsv` row with
rationale (e.g. "baseline reset because Go 1.23 changed map allocation perf by 12%").

## 10. Deliverable on completion

`autoresearch-altcode.tsv` shows ≥ 50 keep rows across the five metrics, no remaining
gates failing, both reviewers (CC + codex) score this plan ≥ 9/10 on every dimension,
and altcode TUI sits at or above the soft targets for M1–M5.

## 11. Acknowledged limits

- `make tui-eval` takes ~3-5 min on baseline hardware (mostly Playwright). The autoresearch
  loop runs it once per accepted iteration, not per micro-edit.
- M2 is sensitive to GC tuning. The `GOGC=100` pin matters; running on a noisy CI runner
  may produce ratio drift up to 5%. The §7 KEEP rule asymmetry (M2 needs 8% to be
  kept, but only regresses on a >5% drop) is calibrated to that drift envelope.
- M4 requires `tmux` installed. On CI without tmux, the script returns sentinel `-` and
  the loop treats M4 as "unchanged" rather than failing.
- M1's registry (`tui_features.tsv`) is itself a human-curated artifact. Adding a row
  to the registry counts as an autoresearch experiment of its own (see E1, E11).
- M3 cannot fully resist all gaming (e.g. shallow assertions), but the M3_count guard
  blocks the most common attack (deleting untested code).

---

*This document IS the program.md equivalent for altcode autoresearch. The metrics are
the contract; the queue is open. Reviewers should grade on the five autoresearch
dimensions, not on whether the queue items look pretty.*
