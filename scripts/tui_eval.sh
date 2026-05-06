#!/usr/bin/env bash
# Hermetic top-level evaluator. Runs all 4 metric scripts and emits one
# TSV row to stdout in the column order:
#   commit  M1_density  M2_perf_min_ratio  M3_coverage_pct  M3_covered_count  M4_latency_ms
#
# Used by `make tui-eval` and the autoresearch loop. Each metric script
# is independent and tolerates missing tools (skips with sentinel "-").
set -euo pipefail
cd "$(dirname "$0")/.."

commit=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")

run_or_dash() {
  local out
  if out=$(bash "$1" 2>/dev/null); then
    echo "$out"
  else
    echo "-"
  fi
}

env_status=$(bash scripts/tui_envcheck.sh 2>/dev/null || echo "envcheck-fail")

# Invalidate stale side-files before any metric runs. If a metric fails
# this iteration, M3_count will read "-" rather than yesterday's value.
rm -f /tmp/tui_coverage.last

m1=$(run_or_dash scripts/tui_density.sh)
m2=$(run_or_dash scripts/tui_perf.sh)
m3=$(run_or_dash scripts/tui_coverage.sh)
m3c="-"
# Only trust the side-file if M3 itself produced a real number this run.
if [[ "$m3" != "-" && -f /tmp/tui_coverage.last ]]; then
  m3c=$(awk '{print $2}' /tmp/tui_coverage.last)
fi
m4=$(run_or_dash scripts/tui_latency.sh)
m5=$(run_or_dash scripts/tui_discover.sh)

printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$commit" "$m1" "$m2" "$m3" "$m3c" "$m4" "$m5" "$env_status"
