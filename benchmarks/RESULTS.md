# AltFix Benchmark Results

## Methodology
- 50 curated issues across Go, JS/TS, Python, Rust, Java
- Each issue submitted to AltFix daemon with default model
- Measured: merge rate, average time, average cost, retry rate

## Latest Results

| Metric | AltFix | Devin* | Copilot Workspace* | Jules* |
|--------|:------:|:------:|:-------------------:|:------:|
| Merge rate | TBD | 83%+ | 30%+ | N/A |
| Avg time | TBD | ~15 min | ~5 min | N/A |
| Avg cost | TBD | $2-5/task | included | N/A |
| Multi-model | Yes | No | No | No |
| Self-hosted | Yes | No | No | No |

*Competitor data from public announcements, not direct testing
+Devin claims "83% more junior-level tasks per ACU"; Cursor claims "30% of merged PRs"

## By Language

| Language | Issues | Merge Rate | Avg Cost |
|----------|:------:|:----------:|:--------:|
| Go | 15 | TBD | TBD |
| JavaScript/TS | 15 | TBD | TBD |
| Python | 10 | TBD | TBD |
| Rust | 5 | TBD | TBD |
| Java | 5 | TBD | TBD |

## By Difficulty

| Difficulty | Issues | Merge Rate | Avg Cost |
|------------|:------:|:----------:|:--------:|
| Easy | 20 | TBD | TBD |
| Medium | 20 | TBD | TBD |
| Hard | 10 | TBD | TBD |

## By Category

| Category | Issues | Merge Rate | Avg Cost |
|----------|:------:|:----------:|:--------:|
| Bug fix | 15 | TBD | TBD |
| Feature | 15 | TBD | TBD |
| Refactor | 10 | TBD | TBD |
| Test | 10 | TBD | TBD |

## Running Benchmarks

```bash
# Run 10 issues with default model
./benchmarks/run.sh

# Run all 50 with specific model
MAX_ISSUES=50 MODEL=altllm-standard ./benchmarks/run.sh

# Parallel execution
PARALLEL=3 ./benchmarks/run.sh
```
