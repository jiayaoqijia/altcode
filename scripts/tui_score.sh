#!/usr/bin/env bash
# Frozen evaluator for the altcode TUI feature-parity autoresearch loop.
# Read-only — equivalent to karpathy/autoresearch's prepare.py.
#
# Reads tui_features.tsv (the feature checklist) and re-evaluates the
# `altcode_implements` column by greping internal/tui/commands.go for
# the slash-command handler. Prints one number on stdout: the count
# of implemented features. Higher is better.
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ ! -f tui_features.tsv ]]; then
  echo "tui_features.tsv missing" >&2
  exit 1
fi

handler_file=internal/tui/commands.go
total=0
got=0

# Skip header.
while IFS=$'\t' read -r feature source impl_unused; do
  [[ "$feature" == "feature" ]] && continue
  [[ -z "$feature" ]] && continue
  total=$((total + 1))
  # Match the literal "/feature" string in the slash-command list. The
  # check is intentionally loose (substring search) — we only care
  # whether a handler entry mentions this command name.
  if grep -qF "\"/$feature\"" "$handler_file"; then
    got=$((got + 1))
  fi
done < tui_features.tsv

echo "$got/$total"
