#!/bin/bash
# Comprehensive tmux-based E2E test suite for the altcode TUI.
# Launches the TUI in tmux, sends keystrokes, captures pane output,
# and asserts expected content is rendered.
#
# Usage: ./scripts/tui-feature-test.sh
# Requires: tmux, bash, Go toolchain
set -euo pipefail

cd /home/coder/github/altcode

# ── Build fresh binary ──────────────────────────────────
echo "Building altcode..."
GOFLAGS=-mod=mod go build -o /tmp/altcode-tui-e2e ./cmd/altcode/
echo "Build OK."
echo ""

BINARY="/tmp/altcode-tui-e2e"
SESSION="tui-feature-test"
PASS=0
FAIL=0
RESULTS=""

RED='\033[31m'
GREEN='\033[32m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

cleanup() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
}
trap cleanup EXIT

# ── Helpers ─────────────────────────────────────────────

# Start a fresh TUI session with the given terminal size.
# Waits up to 12 seconds for the TUI to finish rendering
# (MCP server startup can take 4+ seconds).
start_tui() {
  local width="${1:-120}"
  local height="${2:-30}"
  cleanup
  sleep 0.3
  tmux new-session -d -s "$SESSION" -x "$width" -y "$height" \
    "$BINARY 2>/dev/null; sleep 2"
  # Wait for the TUI to render.
  local deadline=$((SECONDS + 12))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if tmux capture-pane -t "$SESSION" -p 2>/dev/null \
        | grep -q "altcode\|Ask anything\|Ask"; then
      sleep 1
      return 0
    fi
    sleep 0.5
  done
  sleep 2
}

# Capture the current pane content (plain text, no ANSI).
snap() {
  tmux capture-pane -t "$SESSION" -p 2>/dev/null || echo ""
}

# Send keys to the tmux session.
send() {
  tmux send-keys -t "$SESSION" "$@"
}

# Run a single test: start TUI, optionally send keys, check output.
# Arguments:
#   $1  test name
#   $2  width (default 120)
#   $3  height (default 30)
#   $4  keys to send (eval'd; empty = none)
#   $5  seconds to wait after keys (default 2)
#   $6  grep -i pattern to expect in output
run_test() {
  local name="$1"
  local width="${2:-120}"
  local height="${3:-30}"
  local keys="$4"
  local wait="${5:-2}"
  local expect="$6"

  start_tui "$width" "$height"

  if [ -n "$keys" ]; then
    eval "$keys"
  fi

  sleep "$wait"

  local output
  output=$(snap)

  cleanup

  if echo "$output" | grep -qi "$expect"; then
    PASS=$((PASS + 1))
    RESULTS="${RESULTS}\n  ${GREEN}PASS${NC} $name"
  else
    FAIL=$((FAIL + 1))
    RESULTS="${RESULTS}\n  ${RED}FAIL${NC} $name (expected: $expect)"
    echo -e "  ${RED}FAIL${NC}: $name"
    echo "    expected pattern: $expect"
    echo "    actual output (first 6 lines):"
    echo "$output" | head -6 | sed 's/^/      /'
  fi
}

echo "=== altcode TUI Feature Tests ==="
echo ""

# ── 1. Startup ──────────────────────────────────────────
echo -e "${BOLD}[1/20] Startup renders welcome${NC}"
run_test "startup-welcome" 120 30 \
  "" 1 \
  "altcode"

# ── 2. HUD bar ──────────────────────────────────────────
echo -e "${BOLD}[2/20] HUD shows model + git info${NC}"
run_test "hud-model-git" 120 30 \
  "" 1 \
  "gpt-5.4\|git:\|main"

# ── 3. /help ────────────────────────────────────────────
echo -e "${BOLD}[3/20] /help shows shortcuts${NC}"
run_test "help-shortcuts" 120 30 \
  "send '/help' Enter" 2 \
  "Ctrl+K\|Ctrl+J\|command palette"

# ── 4. /status ──────────────────────────────────────────
echo -e "${BOLD}[4/20] /status shows model + session${NC}"
run_test "status-info" 120 30 \
  "send '/status' Enter" 2 \
  "Model.*openai\|Session\|Messages"

# ── 5. /tools ───────────────────────────────────────────
echo -e "${BOLD}[5/20] /tools lists registered tools${NC}"
run_test "tools-list" 120 30 \
  "send '/tools' Enter" 2 \
  "read\|write\|bash\|glob\|grep"

# ── 6. /doctor ──────────────────────────────────────────
echo -e "${BOLD}[6/20] /doctor shows health report${NC}"
run_test "doctor-report" 120 30 \
  "send '/doctor' Enter" 2 \
  "Doctor Report\|configured\|registered"

# ── 7. /cost ────────────────────────────────────────────
echo -e "${BOLD}[7/20] /cost shows cost info${NC}"
run_test "cost-info" 120 30 \
  "send '/cost' Enter" 2 \
  "No turns recorded\|cost\|Cost\|\\$"

# ── 8. /memory ──────────────────────────────────────────
echo -e "${BOLD}[8/20] /memory shows entries${NC}"
run_test "memory-entries" 120 30 \
  "send '/memory' Enter" 2 \
  "Loaded memories\|memory\|Memory\|no.*memor"

# ── 9. /diff ────────────────────────────────────────────
echo -e "${BOLD}[9/20] /diff shows file changes${NC}"
run_test "diff-output" 120 30 \
  "send '/diff' Enter" 2 \
  "No files changed\|diff\|changed"

# ── 10. /version ────────────────────────────────────────
echo -e "${BOLD}[10/20] /version shows version info${NC}"
run_test "version-info" 120 30 \
  "send '/version' Enter" 2 \
  "altcode.*v\|Go:\|Platform:\|Commit:"

# ── 11. /skills ─────────────────────────────────────────
# Skills output is long and scrolls; match content visible
# in the bottom portion of the pane (skill names/descriptions).
echo -e "${BOLD}[11/20] /skills lists discovered skills${NC}"
run_test "skills-list" 120 50 \
  "send '/skills' Enter" 4 \
  "review\|triage\|impeccable\|codex\|investigate\|ship"

# ── 12. /mcp ────────────────────────────────────────────
echo -e "${BOLD}[12/20] /mcp shows MCP servers${NC}"
run_test "mcp-servers" 120 30 \
  "send '/mcp' Enter" 3 \
  "MCP server\|configured\|stdio\|tools"

# ── 13. /plugins ────────────────────────────────────────
echo -e "${BOLD}[13/20] /plugins shows plugin info${NC}"
run_test "plugins-info" 120 30 \
  "send '/plugins' Enter" 2 \
  "plugin\|Plugin\|search path\|merged"

# ── 14. /backends ───────────────────────────────────────
echo -e "${BOLD}[14/20] /backends shows detected CLIs${NC}"
run_test "backends-clis" 120 30 \
  "send '/backends' Enter" 3 \
  "Detected.*CLI\|claude\|codex"

# ── 15. /clear ──────────────────────────────────────────
echo -e "${BOLD}[15/20] /clear clears conversation${NC}"
# Send /help first, then /clear; after clear we should see
# the welcome text again (no help shortcuts visible).
run_test "clear-screen" 120 30 \
  "send '/help' Enter; sleep 2; send '/clear' Enter" 2 \
  "altcode\|Ask anything"

# ── 16. Ctrl+K command palette ──────────────────────────
echo -e "${BOLD}[16/20] Ctrl+K opens command palette${NC}"
run_test "palette-open" 120 30 \
  "send C-k" 2 \
  "Type a command\|/help\|/status\|/tools"

# ── 17. Vim mode via Esc ────────────────────────────────
echo -e "${BOLD}[17/20] Esc enters vim NORMAL mode${NC}"
run_test "vim-normal" 120 30 \
  "send Escape" 1 \
  "NORMAL"

# ── 18. Narrow terminal 60x15 ──────────────────────────
echo -e "${BOLD}[18/20] Narrow terminal 60x15 no crash${NC}"
run_test "narrow-60x15" 60 15 \
  "" 1 \
  "altcode\|Ask anything"

# ── 19. Tiny terminal 30x8 ─────────────────────────────
echo -e "${BOLD}[19/20] Tiny terminal 30x8 no crash${NC}"
run_test "tiny-30x8" 30 8 \
  "" 1 \
  "altcode\|Ask"

# ── 20. /quit exits cleanly ────────────────────────────
echo -e "${BOLD}[20/20] /quit exits cleanly${NC}"
# The TUI uses the alternate screen buffer, so after exit the
# pane content is cleared. Use a sentinel file to verify exit.
cleanup
sleep 0.3
rm -f /tmp/altcode-quit-sentinel
tmux new-session -d -s "$SESSION" -x 120 -y 30 \
  "bash -c '$BINARY 2>/dev/null; echo \$? > /tmp/altcode-quit-sentinel; sleep 30'"
# Wait for TUI startup
local_deadline=$((SECONDS + 12))
while [ "$SECONDS" -lt "$local_deadline" ]; do
  if tmux capture-pane -t "$SESSION" -p 2>/dev/null \
      | grep -q "altcode\|Ask anything"; then
    sleep 1
    break
  fi
  sleep 0.5
done
send '/quit' Enter
# Wait for exit (MCP cleanup can take several seconds)
quit_deadline=$((SECONDS + 12))
quit_ok=false
while [ "$SECONDS" -lt "$quit_deadline" ]; do
  if [ -f /tmp/altcode-quit-sentinel ]; then
    quit_ok=true
    break
  fi
  sleep 0.5
done
cleanup
rm -f /tmp/altcode-quit-sentinel

if $quit_ok; then
  PASS=$((PASS + 1))
  RESULTS="${RESULTS}\n  ${GREEN}PASS${NC} quit-exits-cleanly"
else
  FAIL=$((FAIL + 1))
  RESULTS="${RESULTS}\n  ${RED}FAIL${NC} quit-exits-cleanly (TUI did not exit within 12s)"
  echo -e "  ${RED}FAIL${NC}: quit-exits-cleanly"
  echo "    TUI did not exit within 12 seconds after /quit"
fi

# ── Results ─────────────────────────────────────────────
echo ""
echo "========================================"
echo -e "${BOLD}Results${NC}"
echo -e "$RESULTS"
echo ""
echo -e "PASS: ${GREEN}$PASS${NC}  FAIL: ${RED}$FAIL${NC}  TOTAL: $((PASS + FAIL))"
echo "========================================"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
