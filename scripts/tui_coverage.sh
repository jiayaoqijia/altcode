#!/usr/bin/env bash
# Frozen evaluator for M3: TUI line coverage with anti-gaming invariant.
# Output: a single floating-point number on stdout (the % of statements).
#
# Anti-gaming:
#   The ABSOLUTE count of covered statements must not decrease.
#   Coverage % can rise by deleting code; covered_count can only rise by
#   exercising more code. We emit two numbers to a side-file
#   (`/tmp/tui_coverage.last`) and the autoresearch loop checks them.
#
#   Specifically:
#     - If coverage % rose AND covered_count rose ≥ 0 → KEEP.
#     - If coverage % rose AND covered_count fell    → DISCARD.
set -euo pipefail
cd "$(dirname "$0")/.."

cover_out=$(mktemp)
trap "rm -f $cover_out" EXIT

GOFLAGS=-mod=mod go test ./internal/tui/ -cover -coverprofile="$cover_out" \
  -count=1 -timeout=300s > /dev/null 2>&1 || {
    echo "tui tests failed" >&2
    exit 2
  }

# Parse % from `go tool cover -func`.
pct=$(GOFLAGS=-mod=mod go tool cover -func="$cover_out" | awk '/^total:/ {gsub("%","",$3); print $3}')
[[ -n "$pct" ]] || { echo "could not parse coverage" >&2; exit 2; }

# Count covered statements from the profile.
# Go coverprofile format (after mode: header): "path:start.col,end.col numStmt count"
# So $2 == numStmt, $3 == count. We sum numStmt over rows where count>0.
covered=$(awk 'NR>1 && NF==3 && $3+0 > 0 { total += $2 } END { print total+0 }' "$cover_out")

# Side-file for the autoresearch loop.
mkdir -p /tmp
echo "${pct} ${covered}" > /tmp/tui_coverage.last

echo "$pct"
