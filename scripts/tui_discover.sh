#!/usr/bin/env bash
# Frozen evaluator for M5: discoverability — % of registered features
# whose unique tab-completion prefix is ≤ 3 characters.
# Output: a single floating-point number on stdout (a percentage).
#
# Definition:
#   For each command in tui_features.tsv that is implemented (handler
#   in commands.go), compute its minimum unique prefix length: the
#   smallest k such that NO other implemented command shares that
#   k-character prefix. M5 = 100 * (count where k ≤ 3) / total.
#
# Why this measures usability:
#   It bounds the keystrokes a user needs to discover and disambiguate
#   any given command from the palette. In a TUI palette with prefix
#   completion, k=2 means "type 2 chars then Enter" works. k>3 means
#   the user must remember the full name or scroll a list — the
#   discoverability gap claude-code/codex have where altcode wins.
#
# Anti-gaming:
#   Numerator and denominator both come from the frozen registry. Adding
#   a new command shortens the average k for all neighbouring prefixes;
#   adding a redundant alias of an existing command extends a neighbour's
#   prefix and lowers M5 — so spam isn't a free win.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ -f tui_features.tsv ]] || { echo "tui_features.tsv missing" >&2; exit 2; }
[[ -f internal/tui/commands.go ]] || { echo "commands.go missing" >&2; exit 2; }

# Build the implemented-commands list (registry ∩ commands.go).
implemented=()
while IFS=$'\t' read -r feature _src _impl; do
  [[ "$feature" == "feature" || -z "$feature" ]] && continue
  if grep -qF "\"/$feature\"" internal/tui/commands.go; then
    implemented+=("$feature")
  fi
done < tui_features.tsv

total="${#implemented[@]}"
[[ "$total" -gt 0 ]] || { echo "no implemented commands" >&2; exit 2; }

# For each command, find its min unique prefix length.
fast=0
for cmd in "${implemented[@]}"; do
  len=${#cmd}
  for ((k=1; k<=len; k++)); do
    pfx="${cmd:0:$k}"
    matches=0
    for other in "${implemented[@]}"; do
      [[ "${other:0:$k}" == "$pfx" ]] && matches=$((matches + 1))
      [[ "$matches" -ge 2 ]] && break
    done
    if [[ "$matches" -le 1 ]]; then
      [[ "$k" -le 3 ]] && fast=$((fast + 1))
      break
    fi
  done
done

awk -v f="$fast" -v t="$total" 'BEGIN{ printf("%.2f\n", 100.0 * f / t) }'
