#!/bin/bash
# AltFix Benchmark Runner
# Usage: ./benchmarks/run.sh [--issues N] [--model MODEL] [--parallel P]
#
# Environment variables:
#   MODEL        Model to use (default: altllm-basic)
#   MAX_ISSUES   Number of issues to run (default: 10)
#   PARALLEL     Concurrent task limit (default: 1)
#   DAEMON_PORT  Port for AltFix daemon (default: 9200)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

ISSUES_FILE="$SCRIPT_DIR/issues.json"
RESULTS_DIR="$SCRIPT_DIR/results/$(date +%Y%m%d-%H%M%S)"
MODEL="${MODEL:-altllm-basic}"
MAX_ISSUES="${MAX_ISSUES:-10}"
PARALLEL="${PARALLEL:-1}"
DAEMON_PORT="${DAEMON_PORT:-9200}"
DAEMON_TOKEN="bench-$(openssl rand -hex 8)"

mkdir -p "$RESULTS_DIR"

echo "=== AltFix Benchmark Suite ==="
echo "Model:    $MODEL"
echo "Issues:   $MAX_ISSUES"
echo "Parallel: $PARALLEL"
echo "Results:  $RESULTS_DIR"
echo ""

# Validate issues file
if [ ! -f "$ISSUES_FILE" ]; then
  echo "ERROR: issues file not found: $ISSUES_FILE"
  exit 1
fi

TOTAL_AVAILABLE=$(jq length "$ISSUES_FILE")
if [ "$MAX_ISSUES" -gt "$TOTAL_AVAILABLE" ]; then
  echo "WARNING: requested $MAX_ISSUES issues but only $TOTAL_AVAILABLE available"
  MAX_ISSUES="$TOTAL_AVAILABLE"
fi

# Kill any existing process on the daemon port
kill "$(lsof -t -i:"$DAEMON_PORT")" 2>/dev/null || true
sleep 1

# Start daemon
SIGNING_KEY=$(openssl rand -hex 32)
"$PROJECT_ROOT/dist/altcode" daemon \
  --port "$DAEMON_PORT" \
  --auth-token "$DAEMON_TOKEN" \
  --data-dir "$RESULTS_DIR/daemon" \
  --max-concurrent "$PARALLEL" &
DAEMON_PID=$!

# Ensure daemon is cleaned up on exit
cleanup() {
  echo ""
  echo "Cleaning up daemon (PID $DAEMON_PID)..."
  kill "$DAEMON_PID" 2>/dev/null || true
  wait "$DAEMON_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for daemon to be ready
echo "Waiting for daemon to start..."
RETRIES=0
MAX_RETRIES=10
while ! curl -sf "http://localhost:$DAEMON_PORT/health" > /dev/null 2>&1; do
  RETRIES=$((RETRIES + 1))
  if [ "$RETRIES" -ge "$MAX_RETRIES" ]; then
    echo "ERROR: daemon failed to start after ${MAX_RETRIES}s"
    exit 1
  fi
  sleep 1
done
echo "Daemon ready on port $DAEMON_PORT"
echo ""

# Submit issues
SUBMITTED=0
echo "id,task_id,repo,language,difficulty,category" > "$RESULTS_DIR/submissions.csv"

jq -c ".[:$MAX_ISSUES][]" "$ISSUES_FILE" | while read -r issue; do
  REPO=$(echo "$issue" | jq -r '.repo')
  DESC=$(echo "$issue" | jq -r '.description')
  ID=$(echo "$issue" | jq -r '.id')
  LANG=$(echo "$issue" | jq -r '.language')
  DIFF=$(echo "$issue" | jq -r '.difficulty')
  CAT=$(echo "$issue" | jq -r '.category')

  echo "Submitting: $ID ($LANG, $DIFF, $CAT)"
  echo "  $DESC"

  RESULT=$(curl -s -X POST "http://localhost:$DAEMON_PORT/tasks" \
    -H "Authorization: Bearer $DAEMON_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{
      \"repo_url\": \"https://github.com/$REPO\",
      \"task\": \"$DESC\",
      \"model\": \"$MODEL\"
    }")

  TASK_ID=$(echo "$RESULT" | jq -r '.id // empty')
  if [ -n "$TASK_ID" ]; then
    echo "  -> Task: $TASK_ID"
    echo "$ID,$TASK_ID,$REPO,$LANG,$DIFF,$CAT" >> "$RESULTS_DIR/submissions.csv"
    SUBMITTED=$((SUBMITTED + 1))
  else
    echo "  -> FAILED: $RESULT"
    echo "$ID,FAILED,$REPO,$LANG,$DIFF,$CAT" >> "$RESULTS_DIR/submissions.csv"
  fi
  echo ""
done

echo "Submitted: $SUBMITTED tasks"
echo ""

# Collect results (poll until all tasks complete or timeout)
echo "Waiting for tasks to complete (timeout: 30 min)..."
TIMEOUT=$((30 * 60))
START=$(date +%s)

while true; do
  ELAPSED=$(($(date +%s) - START))
  if [ "$ELAPSED" -gt "$TIMEOUT" ]; then
    echo "TIMEOUT after ${TIMEOUT}s"
    break
  fi

  # Check task statuses
  TASKS=$(curl -s "http://localhost:$DAEMON_PORT/tasks" \
    -H "Authorization: Bearer $DAEMON_TOKEN")

  PENDING=$(echo "$TASKS" | jq '[.[] | select(.status == "pending")] | length')
  RUNNING=$(echo "$TASKS" | jq '[.[] | select(.status | test("planning|implementing|reviewing|testing"))] | length')
  COMPLETED=$(echo "$TASKS" | jq '[.[] | select(.status | test("merged|completed|closed"))] | length')
  FAILED=$(echo "$TASKS" | jq '[.[] | select(.status == "failed")] | length')

  printf "  [%4ds] pending=%-3d running=%-3d completed=%-3d failed=%-3d\n" \
    "$ELAPSED" "$PENDING" "$RUNNING" "$COMPLETED" "$FAILED"

  if [ "$PENDING" -eq 0 ] && [ "$RUNNING" -eq 0 ]; then
    echo "All tasks finished."
    break
  fi

  sleep 10
done

# Generate results
echo ""
echo "=== Results ==="
echo ""

TASKS=$(curl -s "http://localhost:$DAEMON_PORT/tasks" \
  -H "Authorization: Bearer $DAEMON_TOKEN")

TOTAL=$(echo "$TASKS" | jq 'length')
MERGED=$(echo "$TASKS" | jq '[.[] | select(.status | test("merged|completed|closed"))] | length')
FAILED=$(echo "$TASKS" | jq '[.[] | select(.status == "failed")] | length')
TOTAL_COST=$(echo "$TASKS" | jq '[.[].api_cost_usd // 0] | add // 0')
AVG_COST=$(echo "$TASKS" | jq 'if length > 0 then ([.[].api_cost_usd // 0] | add) / length else 0 end')

echo "Total tasks:  $TOTAL"
echo "Completed:    $MERGED"
echo "Failed:       $FAILED"

if [ "$TOTAL" -gt 0 ]; then
  MERGE_RATE=$(echo "scale=1; $MERGED * 100 / $TOTAL" | bc)
  echo "Merge rate:   ${MERGE_RATE}%"
else
  echo "Merge rate:   N/A"
fi

echo "Total cost:   \$$TOTAL_COST"
echo "Avg cost:     \$$AVG_COST"
echo ""

# Breakdown by language
echo "--- By Language ---"
for lang in go javascript typescript python rust java; do
  LANG_TOTAL=$(jq -c ".[:$MAX_ISSUES][] | select(.language == \"$lang\")" "$ISSUES_FILE" | wc -l)
  if [ "$LANG_TOTAL" -gt 0 ]; then
    printf "  %-12s %d issues\n" "$lang" "$LANG_TOTAL"
  fi
done
echo ""

# Breakdown by difficulty
echo "--- By Difficulty ---"
for diff in easy medium hard; do
  DIFF_TOTAL=$(jq -c ".[:$MAX_ISSUES][] | select(.difficulty == \"$diff\")" "$ISSUES_FILE" | wc -l)
  if [ "$DIFF_TOTAL" -gt 0 ]; then
    printf "  %-8s %d issues\n" "$diff" "$DIFF_TOTAL"
  fi
done
echo ""

# Breakdown by category
echo "--- By Category ---"
for cat in bug-fix feature refactor test; do
  CAT_TOTAL=$(jq -c ".[:$MAX_ISSUES][] | select(.category == \"$cat\")" "$ISSUES_FILE" | wc -l)
  if [ "$CAT_TOTAL" -gt 0 ]; then
    printf "  %-10s %d issues\n" "$cat" "$CAT_TOTAL"
  fi
done
echo ""

# Save raw task data
echo "$TASKS" | jq '.' > "$RESULTS_DIR/tasks.json"

# Generate summary JSON
cat > "$RESULTS_DIR/summary.json" << SUMEOF
{
  "date": "$(date -Iseconds)",
  "model": "$MODEL",
  "total_tasks": $TOTAL,
  "completed": $MERGED,
  "failed": $FAILED,
  "merge_rate": $(if [ "$TOTAL" -gt 0 ]; then echo "scale=3; $MERGED / $TOTAL" | bc; else echo "0"; fi),
  "total_cost_usd": $TOTAL_COST,
  "avg_cost_usd": $AVG_COST,
  "results_dir": "$RESULTS_DIR"
}
SUMEOF

echo "Summary saved to: $RESULTS_DIR/summary.json"
echo "Raw tasks saved to: $RESULTS_DIR/tasks.json"
echo "Submissions log: $RESULTS_DIR/submissions.csv"
echo ""
echo "Done."
