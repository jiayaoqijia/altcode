# Multi-Model Benchmark: 7 AI Models on Identical Coding Task

## Setup

- **Task**: Given a buggy Go cache implementation, find and fix bugs, add missing methods, write comprehensive tests
- **Input**: Identical `cache.go` with a Len() bug (counts expired items), missing Keys() and Cleanup() methods, no tests
- **Workspaces**: Each model gets its own copy of the project in a separate directory (no shared state)
- **Prompt**: Same prompt for all 7 models
- **Validation**: `go test -v -race` must pass with zero failures
- **Tools available**: read, write, edit, grep, glob, ls, bash (altcode's built-in tool set)

### Models tested

| # | Model | Provider | Access |
|---|-------|----------|--------|
| 1 | Claude Sonnet 4 | Anthropic | Claude Code CLI (separate instance) |
| 2 | GPT-5.4 | OpenAI | altcode via Codex relay |
| 3 | DeepSeek V3 | DeepSeek | altcode via OpenRouter |
| 4 | MiniMax 2.7 | MiniMax | altcode via OpenRouter |
| 5 | GLM-5 | Zhipu AI | altcode via OpenRouter |
| 6 | Kimi K2.5 | Moonshot | altcode via OpenRouter |
| 7 | Qwen3 Coder | Alibaba | altcode via OpenRouter |

## Results

### Scorecard

| Model | Tests | Len Fix | Keys() | Cleanup() | Race-safe | cache.go | test lines |
|-------|------:|:-------:|:------:|:---------:|:---------:|---------:|-----------:|
| Claude Code | **11** | YES | YES | YES | YES | 76 | 151 |
| Kimi K2.5 | **8** | YES | YES | YES | YES | 79 | 225 |
| Qwen3 Coder | **8** | YES | YES | YES | YES | 76 | 193 |
| GPT-5.4 | 6 | YES | YES | YES | YES | 79 | 116 |
| DeepSeek V3 | 6 | YES | YES | YES | YES | 76 | 92 |
| MiniMax 2.7 | 6 | YES | YES | YES | YES | 80 | 155 |
| GLM-5 | 6 | YES | YES | YES | YES | 79 | 167 |

**All 7 models passed every requirement.** The difference is test coverage depth.

### Test coverage detail

| Model | Unique/notable tests |
|-------|---------------------|
| Claude Code | GetMissing, GetExpired, LenEmpty, KeysEmpty, CleanupNothingExpired, SetOverwrite — most edge cases |
| Kimi K2.5 | GetNonExistent, CleanupConcurrency — only model to test concurrent cleanup |
| Qwen3 Coder | CleanupEmpty, CacheOverwrite — only model to test overwrite + empty cleanup |
| GPT-5.4 | CacheConcurrency with heavy goroutine stress (100 goroutines) |
| DeepSeek V3 | Minimal but clean — fewest lines, all cases covered |
| MiniMax 2.7 | Timing-aware expiration test (100ms sleep + verify) |
| GLM-5 | Similar to MiniMax pattern, solid timing tests |

### Full test lists

**Claude Code (11 tests)**
```
TestSetGet
TestGetMissing
TestGetExpired
TestLenExcludesExpired
TestLenEmpty
TestKeysExcludesExpired
TestKeysEmpty
TestCleanup
TestCleanupNothingExpired
TestSetOverwrite
TestConcurrency
```

**GPT-5.4 (6 tests)**
```
TestCacheSetGet
TestCacheExpiration
TestCacheLenExcludesExpired
TestCacheKeysReturnsNonExpired
TestCacheCleanupRemovesExpired
TestCacheConcurrency
```

**DeepSeek V3 (6 tests)**
```
TestCacheSetGet
TestCacheExpiration
TestCacheLen
TestCacheKeys
TestCacheCleanup
TestCacheConcurrency
```

**MiniMax 2.7 (6 tests)**
```
TestSetGet
TestLen
TestKeys
TestCleanup
TestExpiration
TestConcurrency
```

**GLM-5 (6 tests)**
```
TestSetGet
TestLen
TestKeys
TestCleanup
TestExpiration
TestConcurrency
```

**Kimi K2.5 (8 tests)**
```
TestSetGet
TestGetNonExistent
TestExpiration
TestLen
TestKeys
TestCleanup
TestConcurrency
TestCleanupConcurrency
```

**Qwen3 Coder (8 tests)**
```
TestCacheSetGet
TestCacheExpiration
TestCacheLen
TestCacheKeys
TestCacheCleanup
TestCacheConcurrency
TestCacheCleanupEmpty
TestCacheOverwrite
```

## Analysis

### What every model got right

1. **Bug identification**: All 7 identified that `Len()` counts expired items
2. **Fix approach**: All used `time.Now()` comparison to filter expired entries
3. **Keys() implementation**: All return only non-expired keys
4. **Cleanup() implementation**: All remove expired items and return the count
5. **Thread safety**: All maintained `sync.RWMutex` correctly (or upgraded to `sync.Mutex` where needed)
6. **Race detector**: All pass `go test -race` with zero data races

### Where models differed

| Dimension | Leader | Approach |
|-----------|--------|----------|
| **Most tests** | Claude Code (11) | Separate function per edge case |
| **Most thorough concurrency** | Kimi K2.5 | Only model to test concurrent Cleanup |
| **Most concise** | DeepSeek V3 (92 test lines) | Minimal but complete |
| **Most test code** | Kimi K2.5 (225 test lines) | Verbose but thorough |
| **Unique edge cases** | Qwen3 Coder | Overwrite behavior + empty cleanup |
| **Timing tests** | MiniMax 2.7, GLM-5 | Explicit sleep-based expiration verification |

### Code style comparison

- **Claude Code**: Separate test functions per case, descriptive names (TestGetMissing, TestLenEmpty)
- **GPT-5.4**: Prefixed names (TestCache*), moderate verbosity
- **DeepSeek V3**: Minimal, clean, Go-idiomatic
- **MiniMax 2.7**: Timing-based with explicit sleeps for expiration
- **GLM-5**: Similar to MiniMax, clean structure
- **Kimi K2.5**: Most defensive — tests concurrency in cleanup path
- **Qwen3 Coder**: Table-driven-style with clear naming

## Conclusion

All 7 models can serve as effective backends for altcode. The task — find bugs, add features, write tests — was completed correctly by every model. The primary differentiator is **test coverage depth**, not correctness:

- For **maximum test coverage**: Claude Code or Kimi K2.5
- For **fastest, cleanest code**: DeepSeek V3
- For **best edge case discovery**: Qwen3 Coder
- For **concurrent safety testing**: Kimi K2.5
- For **balanced quality**: GPT-5.4, MiniMax 2.7

altcode's multi-provider architecture means users can choose the model that best fits their needs — or use `altcode team` to run multiple models and cross-check results.

## How to reproduce

```bash
# Install altcode
curl -fsSL https://altcode.io/install.sh | bash

# Run with any model via OpenRouter
export OPENROUTER_API_KEY=sk-or-...
altcode --model openai/deepseek/deepseek-chat-v3-0324 \
  --config <(echo '{"provider":{"openai":{"apiKey":"'$OPENROUTER_API_KEY'","baseURL":"https://openrouter.ai/api"}}}') \
  "Fix the bugs in cache.go and write tests"

# Or use Claude Code subscription
altcode --model anthropic/claude-sonnet-4-20250514 "Fix the bugs in cache.go and write tests"

# Or use Codex subscription  
altcode "Fix the bugs in cache.go and write tests"  # defaults to GPT via Codex relay

# Or use Chinese AI providers directly (native support)
export DEEPSEEK_API_KEY=sk-...
altcode --model deepseek/deepseek-chat "Fix the bugs"

export ZHIPU_API_KEY=...
altcode --model zhipu/glm-5 "Fix the bugs"

export MOONSHOT_API_KEY=sk-...
altcode --model moonshot/kimi-k2.5 "Fix the bugs"

export MINIMAX_API_KEY=...
altcode --model minimax/MiniMax-M2.7 "Fix the bugs"

export DASHSCOPE_API_KEY=sk-...
altcode --model qwen/qwen3-coder "Fix the bugs"
```

## Native Chinese Provider Support

altcode supports 5 Chinese AI providers natively (all OpenAI-compatible):

| Provider | Prefix | Base URL | Env Variable | Models |
|----------|--------|----------|--------------|--------|
| DeepSeek | `deepseek/` | api.deepseek.com | `DEEPSEEK_API_KEY` | deepseek-chat, deepseek-reasoner |
| Zhipu AI | `zhipu/` or `glm/` | open.bigmodel.cn | `ZHIPU_API_KEY` | glm-4-plus, glm-5 |
| Moonshot | `moonshot/` or `kimi/` | api.moonshot.cn | `MOONSHOT_API_KEY` | kimi-k2.5, moonshot-v1-auto |
| MiniMax | `minimax/` | api.minimax.chat | `MINIMAX_API_KEY` | MiniMax-M2.5, MiniMax-M2.7 |
| Alibaba | `qwen/` or `dashscope/` | dashscope.aliyuncs.com | `DASHSCOPE_API_KEY` | qwen-max, qwen3-coder |

These also work through OpenRouter with the `openai/` prefix and an OpenRouter key.
