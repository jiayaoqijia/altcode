#!/bin/bash
# TUI end-to-end test suite using tmux for real terminal rendering.
# Captures screenshots of the TUI at key points and checks for rendering bugs.
#
# Usage: ./scripts/tui-e2e-test.sh [path-to-altcode-binary]
# Requires: tmux, bash
set -euo pipefail

ALTCODE="${1:-./dist/altcode}"
SESSION="altcode-tui-test"
PASS=0
FAIL=0
WIDTH=80
HEIGHT=24
SNAP_DIR="/tmp/altcode-tui-snapshots"
rm -rf "$SNAP_DIR" && mkdir -p "$SNAP_DIR"

RED='\033[31m'
GREEN='\033[32m'
DIM='\033[2m'
BOLD='\033[1m'
NC='\033[0m'

cleanup() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
}
trap cleanup EXIT

snap() {
  # Capture the tmux pane content (visible text, no ANSI)
  tmux capture-pane -t "$SESSION" -p 2>/dev/null
}

snap_file() {
  snap > "$SNAP_DIR/$1.txt"
}

assert_contains() {
  local name="$1" pattern="$2"
  local content
  content=$(snap)
  if echo "$content" | grep -q "$pattern"; then
    echo -e "${GREEN}PASS${NC} $name — found '$pattern'"
    PASS=$((PASS + 1))
  else
    echo -e "${RED}FAIL${NC} $name — missing '$pattern'"
    echo -e "${DIM}$(echo "$content" | head -10)${NC}"
    snap_file "FAIL-$name"
    FAIL=$((FAIL + 1))
  fi
}

assert_no_overflow() {
  # Check that no visible line exceeds terminal width
  local name="$1"
  local content
  content=$(snap)
  local max_len=0
  while IFS= read -r line; do
    # Strip ANSI codes for length check
    stripped=$(echo "$line" | sed 's/\x1b\[[0-9;]*m//g')
    len=${#stripped}
    if [ "$len" -gt "$max_len" ]; then max_len=$len; fi
  done <<< "$content"
  if [ "$max_len" -le "$WIDTH" ]; then
    echo -e "${GREEN}PASS${NC} $name — max line width $max_len <= $WIDTH"
    PASS=$((PASS + 1))
  else
    echo -e "${RED}FAIL${NC} $name — line overflow: $max_len > $WIDTH"
    snap_file "FAIL-$name"
    FAIL=$((FAIL + 1))
  fi
}

send_keys() {
  tmux send-keys -t "$SESSION" "$@"
}

wait_for() {
  # Wait up to N seconds for a pattern to appear in the pane
  local pattern="$1" timeout="${2:-10}"
  local deadline=$((SECONDS + timeout))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if snap | grep -q "$pattern"; then return 0; fi
    sleep 0.3
  done
  return 1
}

echo -e "${BOLD}altcode TUI e2e tests${NC}"
echo "Binary: $ALTCODE"
echo "Terminal: ${WIDTH}x${HEIGHT}"
echo "Snapshots: $SNAP_DIR"
echo ""

# ─────────────────────────────────────────────
# TEST 1: Startup — shows welcome view
# ─────────────────────────────────────────────
echo -e "${BOLD}TEST 1: Startup${NC}"
tmux new-session -d -s "$SESSION" -x "$WIDTH" -y "$HEIGHT" "$ALTCODE"
# Wait for TUI to render (MCP servers may delay startup)
if ! wait_for "altcode\|ALTCODE\|Enter\|ready" 10; then
  echo -e "${DIM}(TUI slow to start, extending wait)${NC}"
  sleep 3
fi
snap_file "01-startup"

assert_contains "welcome-title" "altcode"
assert_no_overflow "startup-no-overflow"

# ─────────────────────────────────────────────
# TEST 2: /help command — shows built-in commands
# ────��─────────────���──────────────────────────
echo ""
echo -e "${BOLD}TEST 2: /help${NC}"
send_keys "/help" Enter
wait_for "/status\|/clear\|/version" 8 || true
sleep 1
snap_file "02-help"

assert_contains "help-shows-commands" "/status\|/clear\|/version"
assert_no_overflow "help-no-overflow"

# ─────────────────────────────────────────────
# TEST 3: /status command
# ─────────────────────────────────────────────
echo ""
echo -e "${BOLD}TEST 3: /status${NC}"
send_keys "/status" Enter
wait_for "Model\|model\|Session\|session" 8 || true
sleep 1
snap_file "03-status"

assert_contains "status-shows-info" "Model\|model\|Session\|session\|gpt\|claude\|openai"
assert_no_overflow "status-no-overflow"

# ─────────────────────────────────────────────
# TEST 4: Type a prompt — text wraps correctly
# ��────────────────────────────────────────────
echo ""
echo -e "${BOLD}TEST 4: Long text wrapping${NC}"
# Type a long message and check it doesn't overflow
LONG_MSG="This is a test of word wrapping in the altcode TUI. The text should wrap at the terminal width boundary without overflowing."
send_keys "$LONG_MSG" Enter
sleep 5
snap_file "04-long-text"

assert_no_overflow "response-no-overflow"

# ──────���──────────────────────────��───────────
# TEST 5: /version command
# ─────────────────────────────────────────────
echo ""
echo -e "${BOLD}TEST 5: /version${NC}"
send_keys "/version" Enter
sleep 1
snap_file "05-version"

assert_contains "version-shows-value" "altcode\|version\|dev\|v0"

# ─────────────────────────────────────────────
# TEST 6: Ctrl+K palette opens
# ──────────────────��──────────────────────────
echo ""
echo -e "${BOLD}TEST 6: Command palette (Ctrl+K)${NC}"
send_keys C-k
sleep 1
snap_file "06-palette"

assert_contains "palette-visible" "status\|clear\|help\|model"

# Close palette
send_keys Escape
sleep 0.5

# ─────────────────────────────────────────────
# TEST 7: Vim mode (Esc)
# ─────────────────────────────────────────────
echo ""
echo -e "${BOLD}TEST 7: Vim mode${NC}"
send_keys Escape
sleep 0.5
snap_file "07-vim-mode"

assert_contains "vim-indicator" "NORMAL\|normal\|vim"

# Exit vim mode
send_keys "i"
sleep 0.5

# ─────────────────────────────────────────────
# RESULTS
# ────────────────────���────────────────────────
echo ""
echo "═══════════════════════════════════════"
echo -e "${BOLD}Results: ${GREEN}$PASS pass${NC}, ${RED}$FAIL fail${NC}"
echo "Snapshots saved to: $SNAP_DIR"
echo "═════════════════���═════════════════════"

cleanup

if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo "Failed snapshots:"
  ls "$SNAP_DIR"/FAIL-*.txt 2>/dev/null | sed 's/^/  /'
  exit 1
fi
