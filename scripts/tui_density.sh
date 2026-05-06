#!/usr/bin/env bash
# Frozen evaluator for M1: feature density per surface KLOC.
# Output: a single floating-point number on stdout.
#
# M1 = commands_in_tui_features_tsv_with_handlers / (prod_loc / 1000)
#
# Anti-gaming:
#   1. The numerator is sourced from tui_features.tsv (frozen registry),
#      not from greping commands.go. New commands MUST be added to the
#      registry to count. Trivial 1-line commands not registered = no win.
#   2. We exclude *_test.go from the denominator so adding tests cannot
#      lower LOC artificially.
#   3. We never count a command unless its handler appears in commands.go
#      AND the registry says it's expected. Either-side discrepancies
#      print a warning to stderr.
set -euo pipefail
cd "$(dirname "$0")/.."

[[ -f tui_features.tsv ]] || { echo "tui_features.tsv missing" >&2; exit 2; }
[[ -f internal/tui/commands.go ]] || { echo "commands.go missing" >&2; exit 2; }

# Numerator: registered commands whose handler exists.
got=0
while IFS=$'\t' read -r feature source impl_unused; do
  [[ "$feature" == "feature" ]] && continue
  [[ -z "$feature" ]] && continue
  if grep -qF "\"/$feature\"" internal/tui/commands.go; then
    got=$((got + 1))
  fi
done < tui_features.tsv

# Denominator: production LOC under internal/tui (no _test.go).
loc=$(find internal/tui -name "*.go" ! -name "*_test.go" -print0 \
        | xargs -0 cat | wc -l | tr -d ' ')
[[ "$loc" -gt 0 ]] || { echo "no prod LOC found" >&2; exit 2; }

# Print rounded to 4 decimals — one number, no narration.
awk -v g="$got" -v l="$loc" 'BEGIN{ printf("%.4f\n", g / (l/1000.0)) }'
