#!/usr/bin/env bash
# Frozen evaluator for M4: SIGWINCH → stable frame latency in the TUI.
# Output: a single integer on stdout (milliseconds).
#
# Method:
#   1. Build /tmp/altcode-latency.
#   2. Launch in a tmux pty at 80x24.
#   3. Wait for the header to render.
#   4. Send a series of `tmux resize-window -x 120 -y 30` events.
#   5. After each resize, poll capture-pane every 25ms until the output
#      hasn't changed for 50ms (≈ stable frame). Record the elapsed wall
#      time from issue→stable. Take the median across N=5 resizes.
#
# Anti-gaming:
#   The script always uses tmux + a real subprocess (PTY). It can't be
#   gamed by mocking the resize handler — the metric crosses the process
#   boundary. baseline.json pins the expected env (terminal size, polling
#   interval) so a "fast" measurement on a smaller terminal is rejected.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v tmux >/dev/null 2>&1; then
  echo "tmux not installed" >&2
  exit 2
fi

bin=/tmp/altcode-latency
GOFLAGS=-mod=mod go build -o "$bin" ./cmd/altcode/ >/dev/null 2>&1

session="altcode-latency-$$"
tmux kill-session -t "$session" 2>/dev/null || true
tmux new-session -d -s "$session" -x 80 -y 24 "$bin"

# Wait for header.
for _ in $(seq 1 60); do
  out=$(tmux capture-pane -t "$session" -p 2>/dev/null || true)
  if echo "$out" | grep -qE 'altcode|claude|codex'; then
    break
  fi
  sleep 0.1
done

measure_one() {
  local target="$1"
  local before; before=$(tmux capture-pane -t "$session" -p)
  local start_ms; start_ms=$(date +%s%3N)
  tmux resize-window -t "$session" -x "$target" -y 30 2>/dev/null
  local last="$before"
  local stable_count=0
  while [[ $stable_count -lt 2 ]]; do
    sleep 0.025
    local cur; cur=$(tmux capture-pane -t "$session" -p)
    if [[ "$cur" == "$last" ]]; then
      stable_count=$((stable_count + 1))
    else
      stable_count=0
      last="$cur"
    fi
    local now_ms; now_ms=$(date +%s%3N)
    if (( now_ms - start_ms > 5000 )); then
      echo "5000"
      return
    fi
  done
  local end_ms; end_ms=$(date +%s%3N)
  echo $(( end_ms - start_ms ))
}

samples=()
for w in 100 110 120 100 110; do
  samples+=("$(measure_one $w)")
  sleep 0.2
done

tmux kill-session -t "$session" 2>/dev/null || true

# Median.
printf '%s\n' "${samples[@]}" | sort -n | awk 'NR==3'
