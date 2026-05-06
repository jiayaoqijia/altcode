#!/usr/bin/env bash
# Env verifier — emits a single `ok` or `mismatch:<reason>` to stdout.
# Used by `make tui-eval` to ensure the metrics are reproducible.
set -euo pipefail
cd "$(dirname "$0")/.."

baseline=scripts/baseline.json
[[ -f "$baseline" ]] || { echo "mismatch:no-baseline"; exit 0; }

# Helper: extract a string value from baseline.json.
extract() {
  local key="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "import json,sys
with open('$baseline') as f: d=json.load(f)
v = d['env'].get('$key')
print('' if v is None else v)
"
  else
    awk -v k="\"$key\"" '
      /"env"/ { in_=1 }
      in_ && index($0, k) {
        n=$0; sub(/.*: */, "", n); sub(/[",].*/, "", n); print n; exit
      }
    ' "$baseline"
  fi
}

# Compare a "min version" requirement.
ge() {
  local cur="$1" min="$2"
  printf '%s\n%s\n' "$min" "$cur" | sort -V | head -1 | grep -qx "$min"
}

problems=()

# GOOS / GOARCH — pinned exactly.
goos_pin=$(extract GOOS)
goarch_pin=$(extract GOARCH)
goos_cur=$(go env GOOS 2>/dev/null || uname -s | tr '[:upper:]' '[:lower:]')
goarch_cur=$(go env GOARCH 2>/dev/null || uname -m)
if [[ -n "$goos_pin" && "$goos_cur" != "$goos_pin" ]]; then
  problems+=("GOOS=$goos_cur!=$goos_pin")
fi
if [[ -n "$goarch_pin" && "$goarch_cur" != "$goarch_pin" ]]; then
  problems+=("GOARCH=$goarch_cur!=$goarch_pin")
fi

# Go version.
goex=$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//')
gomin=$(extract go_version_min)
if [[ -z "$goex" ]]; then
  problems+=("go-missing")
elif [[ -n "$gomin" ]]; then
  ge "$goex" "$gomin" || problems+=("go-$goex<$gomin")
fi

# tmux version — required (M4 needs it).
if ! command -v tmux >/dev/null 2>&1; then
  problems+=("tmux-missing")
else
  tex=$(tmux -V 2>/dev/null | awk '{print $2}')
  tmin=$(extract tmux_version_min)
  if [[ -n "$tmin" ]]; then
    ge "$tex" "$tmin" || problems+=("tmux-$tex<$tmin")
  fi
fi

# Node version — required (Playwright gate needs it).
if ! command -v node >/dev/null 2>&1; then
  problems+=("node-missing")
else
  nex=$(node -v 2>/dev/null | sed 's/^v//')
  nmin=$(extract node_version_min)
  if [[ -n "$nmin" ]]; then
    ge "$nex" "$nmin" || problems+=("node-$nex<$nmin")
  fi
fi

# Playwright version — required (gate runs against it).
pwmin=$(extract playwright_version_min)
if [[ -n "$pwmin" ]]; then
  pwex=""
  for cmd in "npx playwright --version" "playwright --version"; do
    pwex=$($cmd 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i ~ /^[0-9]+\.[0-9]+/){print $i; exit}}')
    [[ -n "$pwex" ]] && break
  done
  if [[ -z "$pwex" ]]; then
    problems+=("playwright-missing")
  else
    ge "$pwex" "$pwmin" || problems+=("playwright-$pwex<$pwmin")
  fi
fi

# GOMAXPROCS / GOGC.
if [[ "${GOMAXPROCS:-}" != "" && "${GOMAXPROCS}" != "8" ]]; then
  problems+=("GOMAXPROCS=$GOMAXPROCS!=8")
fi
if [[ "${GOGC:-}" != "" && "${GOGC}" != "100" ]]; then
  problems+=("GOGC=$GOGC!=100")
fi

if (( ${#problems[@]} == 0 )); then
  echo ok
else
  printf 'mismatch:%s\n' "$(IFS=,; echo "${problems[*]}")"
fi
