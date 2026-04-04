# Three-Way Benchmark: Claude Code vs altcode+GPT vs Codex CLI

## Setup

- **Same prompt** for all three, **separate directories**, **120s timeout**
- Claude Code: `claude --print --dangerously-skip-permissions`
- altcode+GPT: `./dist/altcode` (GPT-5.4 via Codex relay)
- Codex CLI: `codex exec --dangerously-bypass-approvals-and-sandbox`
- All run in parallel on the same machine

## Test 1: KV Store — Fix 3 bugs + add 5 methods + write tests

**Input**: A Go key-value store with Get (returns expired), Len (counts expired), Watch (missing), and 5 missing methods (Delete, Keys, Snapshot, PutIfAbsent, GetVersion)

| | Claude Code | altcode+GPT | Codex CLI |
|---|:-:|:-:|:-:|
| **Time** | 116s | 116s | 120s (timeout) |
| **Build** | OK | OK | OK |
| **Bugs fixed** | 3/3 | 3/3 | 3/3 |
| **Methods added** | 5/5 + Watch | 5/5 + Watch | 5/5 + Watch |
| **Test file** | 292 lines | 202 lines | none |
| **Tests passing** | **23** | 8 | 0 |
| **Race-safe** | yes | yes | — |

All three fixed every bug and added every method. Claude Code also wrote 23 tests. altcode+GPT wrote 8. Codex timed out before writing tests.

### Code architecture comparison

| | Claude Code | altcode+GPT | Codex CLI |
|---|---|---|---|
| Expiry check | `Entry.expired()` method | `isExpired(e)` free function | `isExpired(entry, now)` method + `liveEntryLocked` |
| Watch design | Callback-based (`func(any)`) | Channel-based (`<-chan Entry`) | Channel-based (`<-chan Entry`) |
| Lock naming | Standard `Lock/Unlock` | Standard | `*Locked` suffix convention |
| Helpers | 1 (expired) | 4 (isExpired, deleteExpired, notify, expiresAt) | 4 (isExpired, liveEntryLocked, deleteExpiredLocked, notifyWatchersLocked) |

## Test 2: HTTP Middleware — Fix 3 bugs + add 3 features + write tests

**Input**: A Go HTTP middleware chain with Logger (no status capture), RateLimit (global, not per-IP), Recovery (doesn't log panic), and 3 missing middlewares (CORS, RequestID, Timeout)

| | Claude Code | altcode+GPT | Codex CLI |
|---|:-:|:-:|:-:|
| **Time** | 120s (timeout) | 120s (timeout) | 120s (timeout) |
| **Build** | OK | OK | OK |
| **Logger status capture** | yes (responseWriter wrapper) | no | no |
| **Per-IP rate limiting** | yes | no | no |
| **Recovery logs panic** | yes | no | no |
| **CORS added** | yes | no | no |
| **RequestID added** | yes | no | no |
| **Timeout added** | yes | no | no |
| **Tests passing** | **12** | 0 | 0 |

Claude Code completed all 8 requirements and wrote 12 tests. GPT and Codex both timed out before writing any edits — spent the full 120s reading and planning.

### Claude Code test coverage (12 tests)

```
TestLoggerCapturesStatus
TestLoggerDefaultStatus
TestRateLimitPerIP
TestRecoveryLogsPanic
TestCORSHeaders
TestCORSPreflight
TestRequestIDPresent
TestRequestIDUnique
TestTimeoutFires
TestTimeoutNoFire
TestChainOrdering
TestConcurrentSafety
```

## Analysis

### Why Claude Code wins on complex tasks

1. **Starts writing immediately** — reads the file, begins editing in the first few seconds
2. **Streams edits while thinking** — doesn't wait to read everything before acting
3. **One-shot approach** — handles the entire prompt in a single long response
4. **In-process tools** — no API round-trip per tool call

### Why GPT/Codex struggle at 120s

1. **Multiple API round-trips** — each tool call (read, edit, bash) requires a network call
2. **Reads before acting** — reads all files, then plans, then starts editing
3. **Sequential tool calls** — even with parallel_tool_calls enabled, complex edits are sequential
4. **Token budget** — large system prompt + instructions consume input tokens

### When GPT/Codex match Claude Code

From earlier benchmarks (simpler tasks, same machine):

| Task | Claude Code | altcode+GPT |
|------|-----------|------------|
| Simple question | 2.0s | **1.0s** |
| Read file + answer | 16.3s | **6.5s** |
| Write Go function | 13.3s | **3.2s** |
| Fix 2 bugs | ~30s | ~22s |
| Refactor 1 file | ~15s | ~34s |
| Add slash command (2 files) | ~20s | ~25s |

For **focused, single-file tasks**, altcode+GPT is competitive or faster. For **complex multi-method tasks**, Claude Code's streaming advantage dominates.

## Recommendations

| Use case | Best tool |
|----------|----------|
| Quick questions, simple fixes | altcode+GPT (fastest) |
| Single-file refactoring | altcode+GPT or Claude Code (similar) |
| Multi-method feature + tests | Claude Code (writes more, faster) |
| Multi-file changes | Claude Code (completes within timeout) |
| Budget-conscious | altcode + DeepSeek/Qwen via OpenRouter |
| Multi-model cross-check | altcode team (runs all models) |

## How to reproduce

```bash
# Claude Code
claude --print --dangerously-skip-permissions "Read store.go, fix bugs, add methods, write tests"

# altcode + GPT
altcode "Read store.go, fix bugs, add methods, write tests"

# Codex
codex exec --dangerously-bypass-approvals-and-sandbox "Read store.go, fix bugs, add methods, write tests"

# altcode + any model
altcode --model deepseek/deepseek-chat-v3-0324 "Read store.go, fix bugs, add methods, write tests"
```
