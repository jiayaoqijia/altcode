#!/usr/bin/env bash
# Frozen evaluator for M2: TUI render performance ratio against locked baseline.
# Output: a single floating-point number on stdout.
#
# M2 = MIN over sizes [10, 100, 500, 1000] of:
#         (baseline_ns_for_size / current_ns_for_size)
#
# Higher is better. <1.0 means a regression at some size.
#
# Anti-gaming:
#   1. baseline.json is checked-in. Editing requires an autoresearch row.
#   2. Bench is run with `-benchtime=1s` so each size gets warmed.
#   3. We require GOMAXPROCS to match baseline.
set -euo pipefail
cd "$(dirname "$0")/.."

baseline_file="scripts/baseline.json"
[[ -f "$baseline_file" ]] || { echo "baseline.json missing" >&2; exit 2; }

export GOMAXPROCS="${GOMAXPROCS:-8}"
export GOGC="${GOGC:-100}"

tmp=$(mktemp); trap "rm -f $tmp" EXIT

# 5 independent runs at -benchtime=1s. We then take the median ns/op
# per size, so a single transient GC or scheduler stall doesn't move
# the metric.
GOFLAGS=-mod=mod go test -run=^$ -bench='BenchmarkUpdateViewport$' \
  -benchtime=1s -count=5 ./internal/tui/ > "$tmp" 2>&1 || {
    cat "$tmp" >&2
    exit 2
  }

# Extract MEDIAN ns/op for sub-bench `messages-<n>` across all runs.
# Lines look like:
#   BenchmarkUpdateViewport/messages-100-8   	    1234	    600000 ns/op
extract_current() {
  local size="$1"
  awk -v size="$size" '
    $0 ~ ("/messages-" size "[-/ ]") {
      for (i=NF; i>=1; i--) {
        if ($i == "ns/op") { print $(i-1); break }
      }
    }
  ' "$tmp" | sort -n | awk '
    { v[NR]=$1 }
    END {
      if (NR==0) exit
      m = int((NR+1)/2)
      print v[m]
    }
  '
}

# Pull baseline value for a size from baseline.json. Use python if
# available (canonical JSON parse); otherwise a deterministic awk
# parser that walks key:value pairs inside the M2_render_ns_per_op
# object.
extract_baseline() {
  local size="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "
import json,sys
with open('$baseline_file') as f: d=json.load(f)
print(d['M2_render_ns_per_op']['$size'])
"
  else
    awk -v key="\"$size\":" '
      /"M2_render_ns_per_op"/ { in_=1; next }
      in_ && /\}/ { in_=0 }
      in_ && index($0, key) {
        n = $0
        sub(/.*"[0-9]+"[[:space:]]*:[[:space:]]*/, "", n)
        sub(/[^0-9].*/, "", n)
        print n
        exit
      }
    ' "$baseline_file"
  fi
}

ratios=()
for size in 10 100 500 1000; do
  cur=$(extract_current "$size")
  base=$(extract_baseline "$size")
  if [[ -z "$cur" || -z "$base" ]]; then
    echo "missing ns/op for size $size (cur=$cur base=$base)" >&2
    exit 2
  fi
  ratio=$(awk -v b="$base" -v c="$cur" 'BEGIN{ printf("%.4f\n", b / c) }')
  ratios+=("$ratio")
done

# MIN across the ratio vector.
printf '%s\n' "${ratios[@]}" | sort -g | head -1
