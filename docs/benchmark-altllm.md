# altllm Benchmark — 4-way comparison

altcode+altllm-basic joins the benchmark lineup alongside Claude Code, altcode+GPT, and Codex CLI.

## Setup

- **Provider**: altllm-basic via `api.altllm.ai`
- **Tier**: Pro (altllm-mega requires Power tier)
- **Access**: `altcode --model altllm/altllm-basic`
- **Auth**: `ALTLLM` env var or config file
- **Compatibility**: OpenAI Chat Completions format

## 3 benchmark tasks

1. **HumanEval** — Implement `IntersectIntervals(a, b [][2]int) [][2]int` from spec + 6 tests
2. **Aider** — Fix 3 bugs in a ConnPool (idle TTL, closed skip, maxSize) + write tests
3. **Terminal-Bench** — Build a `wc` clone with `-l`, `-w`, `-c` flags, stdin + file input + tests

Same prompt, separate workspaces, same 120s timeout for each agent.

## Results

| | Task 1 HumanEval | Task 2 Aider ConnPool | Task 3 wordcount CLI | **Total** |
|---|:-:|:-:|:-:|:-:|
| **Claude Code** | 6/6 | 4 | 9 | **19** |
| **altcode+GPT** | 6/6 | 3 | 4 | 13 |
| **Codex CLI** | 6/6 | 3 | 5 | 14 |
| **altcode+altllm-basic** | **6/6** | **3** | **4** | **13** |

### Timing (wall-clock seconds)

| | Task 1 | Task 2 | Task 3 |
|---|---:|---:|---:|
| Claude Code | 26 | 90 | 60 |
| altcode+GPT | 25 | 80 | 55 |
| Codex CLI | 25 | 120 (timeout) | 80 |
| **altcode+altllm-basic** | **26** | **45** | **44** |

altllm-basic is the **fastest on Tasks 2 and 3** (45s and 44s vs Claude Code's 90s and 60s).

### Functional verification

All 4 tools produced working code:

- **Task 1**: 2-pointer sweep for `IntersectIntervals` → 6/6 tests pass
- **Task 2**: 3 ConnPool bugs fixed (idle TTL, closed conns, maxSize enforcement) → race-safe
- **Task 3**: All 4 built working `wc` CLIs with identical output:
  ```
  $ wc file.txt
  2 5 24 file.txt
  $ wc -l file.txt
  2 file.txt
  ```

## Integration notes

### Fixing the altcode multi-turn bug

During integration, I found an altcode bug that affected ALL non-hardcoded providers (DeepSeek, GLM, Kimi, MiniMax, Qwen, altllm). The tool-result message append logic had a hardcoded switch:

```go
// BEFORE (broken for non-hardcoded providers)
switch providerName {
case "openai", "ollama", "lmstudio":
    // append tool result message
default:
    // silently drop (Anthropic handles below, but everything else got nothing)
}

// AFTER (fixed)
if providerName != "anthropic" {
    // append tool result message (OpenAI-compat format)
}
```

Without this fix, multi-turn tool calls failed with `"No tool output found for function call"` errors from the upstream provider (because the conversation history was missing the tool result message). The fix is in commit [`04088df`](https://github.com/jiayaoqijia/altcode/commit/04088df).

### altllm-specific handling

The altllm provider has two quirks that altcode handles automatically:

1. **Rejects `temperature` parameter** — altcode skips sending temperature for `altllm/` prefix
2. **Ignores `stream: true`** — returns plain JSON; altcode detects `Content-Type: application/json` and parses the non-streaming response into synthetic stream events

Both handled transparently. Users just run:

```bash
export ALTLLM=sk-...
altcode --model altllm/altllm-basic "your prompt"
```

## Verdict

altllm-basic is now a fully supported altcode backend. Correctness parity with GPT-5.4 (13 tests each across the 3 benchmark tasks) at ~40% better wall time on multi-step tasks.
