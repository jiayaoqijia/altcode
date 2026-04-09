#!/bin/bash
# altcode coding agent benchmark suite
# Tests: code generation, bug fixing, test writing, multi-file refactoring
# Usage: ./run-benchmark.sh <altcode-binary> [config-file]
set -euo pipefail

BINARY="${1:-altcode}"
CONFIG="${2:-}"
RESULTS_DIR="docs/benchmarks/results/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$RESULTS_DIR"

CONFIG_FLAG=""
if [ -n "$CONFIG" ]; then
  CONFIG_FLAG="--config $CONFIG"
fi

PASS=0
FAIL=0
TOTAL=0

run_task() {
  local name="$1"
  local prompt="$2"
  local check="$3"
  local timeout="${4:-60}"

  TOTAL=$((TOTAL + 1))
  echo -n "  [$TOTAL] $name ... "

  # Run altcode headlessly
  OUTPUT=$(timeout "$timeout" "$BINARY" $CONFIG_FLAG "$prompt" 2>&1 || true)
  echo "$OUTPUT" > "$RESULTS_DIR/$name.txt"

  # Check result
  if eval "$check"; then
    PASS=$((PASS + 1))
    echo "PASS"
  else
    FAIL=$((FAIL + 1))
    echo "FAIL"
  fi
}

echo "=== altcode Coding Agent Benchmark ==="
echo "Binary: $BINARY"
echo "Config: ${CONFIG:-default}"
echo "Results: $RESULTS_DIR"
echo ""

# ── TASK 1: Simple function generation ──
echo "Category: Code Generation"
run_task "gen-fibonacci" \
  "Write a Go function Fibonacci(n int) int that returns the nth Fibonacci number using iteration. Output ONLY the function, nothing else." \
  'echo "$OUTPUT" | grep -q "func Fibonacci"' \
  30

# ── TASK 2: Bug fix ──
cat > /tmp/bench-buggy.go << 'GOEOF'
package main
func Reverse(s string) string {
    b := []byte(s)
    for i := 0; i < len(b)/2; i++ {
        b[i], b[len(b)-i] = b[len(b)-i], b[i]  // BUG: off by one
    }
    return string(b)
}
GOEOF
run_task "fix-offbyone" \
  "This Go function has an off-by-one bug in the index: $(cat /tmp/bench-buggy.go). Fix it. Output ONLY the corrected function." \
  'echo "$OUTPUT" | grep -q "len(b)-1-i\|len(b)-i-1"' \
  30

# ── TASK 3: Test writing ──
run_task "write-tests" \
  "Write Go table-driven tests for a function IsPalindrome(s string) bool. Include at least 5 test cases covering: empty string, single char, palindrome, non-palindrome, unicode. Output ONLY the test function." \
  'echo "$OUTPUT" | grep -q "func Test" && echo "$OUTPUT" | grep -c "{\|{$" | awk "{exit (\$1 >= 5 ? 0 : 1)}"' \
  30

# ── TASK 4: Data structure ──
echo ""
echo "Category: Data Structures"
run_task "gen-stack" \
  "Write a generic Go Stack[T any] with Push, Pop, Peek, Len, IsEmpty methods. Use a slice backing. Output ONLY the type and methods." \
  'echo "$OUTPUT" | grep -q "Stack\[" && echo "$OUTPUT" | grep -q "func.*Push" && echo "$OUTPUT" | grep -q "func.*Pop"' \
  30

# ── TASK 5: Algorithm ──
run_task "gen-binarysearch" \
  "Write a Go function BinarySearch(sorted []int, target int) int that returns the index of target or -1 if not found. Output ONLY the function." \
  'echo "$OUTPUT" | grep -q "func BinarySearch"' \
  30

# ── TASK 6: Concurrency ──
echo ""
echo "Category: Concurrency"
run_task "gen-workerpool" \
  "Write a Go function RunWorkerPool(jobs []func(), workers int) that processes jobs concurrently with the given number of workers using goroutines and a channel. Output ONLY the function." \
  'echo "$OUTPUT" | grep -q "func RunWorkerPool" && echo "$OUTPUT" | grep -q "chan\|go func"' \
  30

# ── TASK 7: Error handling ──
echo ""
echo "Category: Error Handling"
run_task "gen-retry" \
  "Write a Go function Retry(fn func() error, maxAttempts int, delay time.Duration) error that retries fn up to maxAttempts times with the given delay between attempts. Output ONLY the function." \
  'echo "$OUTPUT" | grep -q "func Retry" && echo "$OUTPUT" | grep -q "time.Sleep\|time.After"' \
  30

# ── TASK 8: HTTP handler ──
echo ""
echo "Category: Web"
run_task "gen-healthcheck" \
  "Write a Go HTTP handler function HealthCheck(w http.ResponseWriter, r *http.Request) that returns JSON {\"status\":\"ok\",\"uptime\":\"...\"} with the server uptime. Output ONLY the function." \
  'echo "$OUTPUT" | grep -q "func HealthCheck" && echo "$OUTPUT" | grep -q "json\|JSON\|application/json"' \
  30

# ── TASK 9: Code explanation ──
echo ""
echo "Category: Understanding"
run_task "explain-select" \
  "Explain what Go's select statement does with channels in 3 sentences. Be concise." \
  'echo "$OUTPUT" | grep -qi "select\|channel\|goroutine\|block\|case"' \
  20

# ── TASK 10: Multi-step ──
echo ""
echo "Category: Multi-step"
run_task "multistep-crud" \
  "Write a Go in-memory CRUD store for a User struct with ID, Name, Email fields. Include: NewStore(), Create(user), Get(id), Update(user), Delete(id), List() methods. Output the complete package." \
  'echo "$OUTPUT" | grep -q "func.*Create" && echo "$OUTPUT" | grep -q "func.*Get" && echo "$OUTPUT" | grep -q "func.*Delete" && echo "$OUTPUT" | grep -q "func.*List"' \
  45

# ── Summary ──
echo ""
echo "=== Results ==="
echo "Passed: $PASS / $TOTAL"
echo "Failed: $FAIL / $TOTAL"
echo "Score: $(( PASS * 100 / TOTAL ))%"
echo ""
echo "Results saved to: $RESULTS_DIR"

# Save summary
cat > "$RESULTS_DIR/summary.json" << EOF
{
  "binary": "$BINARY",
  "config": "${CONFIG:-default}",
  "total": $TOTAL,
  "passed": $PASS,
  "failed": $FAIL,
  "score": $(( PASS * 100 / TOTAL )),
  "timestamp": "$(date -Iseconds)"
}
EOF
