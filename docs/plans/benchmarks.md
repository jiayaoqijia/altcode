# Coding Agent Benchmarks

## Benchmark Landscape (April 2026)

| Benchmark | Tasks | What it Tests | Top Score | Relevant to altcode? |
|-----------|:-----:|---------------|:---------:|:-------------------:|
| **SWE-bench Verified** | 484 | Real GitHub issue resolution | ~81% | Yes (agent loop) |
| **SWE-bench Pro** | harder | Same, less contaminated | ~46% | Yes |
| **Terminal-Bench 2.0** | 89 | CLI agent workflows (compile, configure, train) | ~82% | **Most relevant** |
| **Aider Polyglot** | 225 | Code editing across 6 languages | ~88% | Yes (tool use) |
| **HumanEval+** | 164 | Function generation from docstring | ~99% | Saturated |
| **FeatureBench** | 200 | End-to-end feature development | ~11% | Yes (hardest) |
| **LiveCodeBench** | 600+ | Competitive programming | ~83% | Less relevant |
| **SWE-Lancer** | 1400+ | Real freelance tasks ($1M payout) | ~$400K | Yes (real-world) |

## altcode Benchmark Results (7 tasks × 3 models)

| Task | DeepSeek V3 | Qwen Coder | Kimi K2.5 | Max |
|------|:-----------:|:----------:|:---------:|:---:|
| Bug Fix (SWE-bench) | 2/2 | 2/2 | 2/2 | 2 |
| Code Gen (HumanEval) | 3/3 | 2/3 | 2/3 | 3 |
| Tool Orchestration | 1/2 | 2/2 | 1/2 | 2 |
| Multi-file Understanding | 3/3 | 3/3 | 3/3 | 3 |
| File Creation + Run | 3/3 | 3/3 | 3/3 | 3 |
| Grep + Analyze | 2/2 | 2/2 | 1/2 | 2 |
| JSON Exec Mode | 3/3 | 3/3 | 3/3 | 3 |
| **Total** | **17/18** | **17/18** | **15/18** | **18** |
| **Percentage** | **94%** | **94%** | **83%** | |

## Key Finding

These "weak" open models achieve 83-94% on practical coding agent tasks when run through altcode.
**Claude Code CLI and Codex CLI cannot use any of these models.**
altcode is the only CLI that enables agentic coding with DeepSeek, Qwen, Kimi, GLM, and MiniMax.

## Next Steps

1. Run Terminal-Bench 2.0 subset (most relevant for CLI agents)
2. Run SWE-bench Lite with altcode + DeepSeek/Qwen
3. Run Aider Polyglot Go subset
