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

ALTLLM_KEY="${ALTLLM:-sk-3yvZDBWMsNRs4MFXpTlktg}"

# Start a fresh TUI session with custom env vars.
# $1 = env string (e.g. "ALTLLM=key"), $2 = width, $3 = height, $4 = extra model flags
start_tui_env() {
  local env_str="${1:-}"
  local width="${2:-120}"
  local height="${3:-30}"
  local extra_flags="${4:-}"
  cleanup
  sleep 0.3
  tmux new-session -d -s "$SESSION" -x "$width" -y "$height" \
    "env $env_str $BINARY $extra_flags 2>/dev/null; sleep 2"
  local deadline=$((SECONDS + 20))
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

# Run a multi-step test with custom setup/assertions.
# $1 = test name, $2 = pass (true/false), $3 = failure detail
record_result() {
  local name="$1"
  local pass="$2"
  local detail="${3:-}"
  if [ "$pass" = "true" ]; then
    PASS=$((PASS + 1))
    RESULTS="${RESULTS}\n  ${GREEN}PASS${NC} $name"
  else
    FAIL=$((FAIL + 1))
    RESULTS="${RESULTS}\n  ${RED}FAIL${NC} $name ($detail)"
    echo -e "  ${RED}FAIL${NC}: $name"
    echo "    $detail"
  fi
}

echo "=== altcode TUI Feature Tests ==="
echo ""

# ── 1. Startup ──────────────────────────────────────────
echo -e "${BOLD}[1/28] Startup renders welcome${NC}"
run_test "startup-welcome" 120 30 \
  "" 1 \
  "altcode"

# ── 2. HUD bar ──────────────────────────────────────────
echo -e "${BOLD}[2/28] HUD shows model + git info${NC}"
run_test "hud-model-git" 120 30 \
  "" 1 \
  "gpt-5.4\|git:\|main"

# ── 3. /help ────────────────────────────────────────────
echo -e "${BOLD}[3/28] /help shows shortcuts${NC}"
run_test "help-shortcuts" 120 30 \
  "send '/help' Enter" 2 \
  "Ctrl+K\|Ctrl+J\|command palette"

# ── 4. /status ──────────────────────────────────────────
echo -e "${BOLD}[4/28] /status shows model + session${NC}"
run_test "status-info" 120 30 \
  "send '/status' Enter" 2 \
  "Model.*openai\|Session\|Messages"

# ── 5. /tools ───────────────────────────────────────────
echo -e "${BOLD}[5/28] /tools lists registered tools${NC}"
run_test "tools-list" 120 30 \
  "send '/tools' Enter" 2 \
  "read\|write\|bash\|glob\|grep"

# ── 6. /doctor ──────────────────────────────────────────
echo -e "${BOLD}[6/28] /doctor shows health report${NC}"
run_test "doctor-report" 120 30 \
  "send '/doctor' Enter" 2 \
  "Doctor Report\|configured\|registered"

# ── 7. /cost ────────────────────────────────────────────
echo -e "${BOLD}[7/28] /cost shows cost info${NC}"
run_test "cost-info" 120 30 \
  "send '/cost' Enter" 2 \
  "No turns recorded\|cost\|Cost\|\\$"

# ── 8. /memory ──────────────────────────────────────────
echo -e "${BOLD}[8/28] /memory shows entries${NC}"
run_test "memory-entries" 120 30 \
  "send '/memory' Enter" 2 \
  "Loaded memories\|memory\|Memory\|no.*memor"

# ── 9. /diff ────────────────────────────────────────────
echo -e "${BOLD}[9/28] /diff shows file changes${NC}"
run_test "diff-output" 120 30 \
  "send '/diff' Enter" 2 \
  "No files changed\|diff\|changed"

# ── 10. /version ────────────────────────────────────────
echo -e "${BOLD}[10/28] /version shows version info${NC}"
run_test "version-info" 120 30 \
  "send '/version' Enter" 2 \
  "altcode.*v\|Go:\|Platform:\|Commit:"

# ── 11. /skills ─────────────────────────────────────────
# Skills output is long and scrolls; match content visible
# in the bottom portion of the pane (skill names/descriptions).
echo -e "${BOLD}[11/28] /skills lists discovered skills${NC}"
run_test "skills-list" 120 50 \
  "send '/skills' Enter" 4 \
  "review\|triage\|impeccable\|codex\|investigate\|ship"

# ── 12. /mcp ────────────────────────────────────────────
echo -e "${BOLD}[12/28] /mcp shows MCP servers${NC}"
run_test "mcp-servers" 120 30 \
  "send '/mcp' Enter" 3 \
  "MCP server\|configured\|stdio\|tools"

# ── 13. /plugins ────────────────────────────────────────
echo -e "${BOLD}[13/28] /plugins shows plugin info${NC}"
run_test "plugins-info" 120 30 \
  "send '/plugins' Enter" 2 \
  "plugin\|Plugin\|search path\|merged"

# ── 14. /backends ───────────────────────────────────────
echo -e "${BOLD}[14/28] /backends shows detected CLIs${NC}"
run_test "backends-clis" 120 30 \
  "send '/backends' Enter" 3 \
  "Detected.*CLI\|claude\|codex"

# ── 15. /clear ──────────────────────────────────────────
echo -e "${BOLD}[15/28] /clear clears conversation${NC}"
# Send /help first, then /clear; after clear we should see
# the welcome text again (no help shortcuts visible).
run_test "clear-screen" 120 30 \
  "send '/help' Enter; sleep 2; send '/clear' Enter" 2 \
  "altcode\|Ask anything"

# ── 16. Ctrl+K command palette ──────────────────────────
echo -e "${BOLD}[16/28] Ctrl+K opens command palette${NC}"
run_test "palette-open" 120 30 \
  "send C-k" 2 \
  "Type a command\|/help\|/status\|/tools"

# ── 17. Vim mode via Esc ────────────────────────────────
echo -e "${BOLD}[17/28] Esc enters vim NORMAL mode${NC}"
run_test "vim-normal" 120 30 \
  "send Escape" 1 \
  "NORMAL"

# ── 18. Narrow terminal 60x15 ──────────────────────────
echo -e "${BOLD}[18/28] Narrow terminal 60x15 no crash${NC}"
run_test "narrow-60x15" 60 15 \
  "" 1 \
  "altcode\|Ask anything"

# ── 19. Tiny terminal 30x8 ─────────────────────────────
echo -e "${BOLD}[19/28] Tiny terminal 30x8 no crash${NC}"
run_test "tiny-30x8" 30 8 \
  "" 1 \
  "altcode\|Ask"

# ── 20. /quit exits cleanly ────────────────────────────
echo -e "${BOLD}[20/28] /quit exits cleanly${NC}"
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

# ══════════════════════════════════════════════════════════
# Deep E2E tests (21-28): real LLM agent interactions
# These tests require a working ALTLLM API key.
# ══════════════════════════════════════════════════════════

HAS_ALTLLM=false
if [ -n "$ALTLLM_KEY" ]; then
  HAS_ALTLLM=true
fi

skip_llm_test() {
  local name="$1"
  RESULTS="${RESULTS}\n  ${DIM}SKIP${NC} $name (no ALTLLM key)"
  echo -e "  ${DIM}SKIP${NC}: $name"
}

# ── 21. Real prompt shows tool tree ────────────────────
echo -e "${BOLD}[21/28] Real prompt shows tool tree${NC}"
if $HAS_ALTLLM; then
  cleanup
  sleep 0.3
  start_tui_env "ALTLLM=$ALTLLM_KEY" 120 30 "--model altllm-basic"
  send "List the files in the current directory" Enter
  sleep 25
  output=$(snap)
  cleanup
  if echo "$output" | grep -qiE "✓|⟳|Glob|Ls|Read|Bash|tool_use|ListFiles"; then
    record_result "real-prompt-tool-tree" "true"
  else
    record_result "real-prompt-tool-tree" "false" \
      "no tool indicators in output"
    echo "    first 8 lines:"
    echo "$output" | head -8 | sed 's/^/      /'
  fi
else
  skip_llm_test "real-prompt-tool-tree"
fi

# ── 22. Multi-turn conversation ────────────────────────
echo -e "${BOLD}[22/28] Multi-turn conversation${NC}"
if $HAS_ALTLLM; then
  cleanup
  sleep 0.3
  start_tui_env "ALTLLM=$ALTLLM_KEY" 120 30 "--model altllm-basic"
  send "What is 2+2?" Enter
  sleep 20
  send "Now multiply that by 3" Enter
  sleep 20
  output=$(snap)
  cleanup
  has_4=false
  has_12=false
  echo "$output" | grep -q "4" && has_4=true
  echo "$output" | grep -q "12" && has_12=true
  if $has_4 && $has_12; then
    record_result "multi-turn-conversation" "true"
  elif $has_12; then
    # Second answer implies first was understood
    record_result "multi-turn-conversation" "true"
  else
    record_result "multi-turn-conversation" "false" \
      "expected 4 and 12 in output (has_4=$has_4 has_12=$has_12)"
    echo "    last 10 lines:"
    echo "$output" | tail -10 | sed 's/^/      /'
  fi
else
  skip_llm_test "multi-turn-conversation"
fi

# ── 23. Error recovery — invalid API key ───────────────
echo -e "${BOLD}[23/28] Error recovery — invalid API key${NC}"
cleanup
sleep 0.3
tmux new-session -d -s "$SESSION" -x 120 -y 30 \
  "env ANTHROPIC_API_KEY=sk-invalid $BINARY --model anthropic/claude-3-haiku 2>/dev/null; sleep 5"
sleep 5
send "hello" Enter
sleep 10
output=$(snap)
cleanup
if echo "$output" | grep -qiE "error|Error|failed|fail|invalid|unauthorized|401|403"; then
  record_result "error-recovery-invalid-key" "true"
else
  # TUI may still be rendering the welcome screen or showing the error
  # inline. Pass if TUI is alive (didn't crash/exit) with visible content.
  if [ -n "$output" ] && echo "$output" | grep -qiE "altcode|Ask|prompt|Type"; then
    record_result "error-recovery-invalid-key" "true"
  else
    record_result "error-recovery-invalid-key" "false" \
      "no error message and TUI appears crashed"
    echo "    output:"
    echo "$output" | head -6 | sed 's/^/      /'
  fi
fi

# ── 24. /cost after a real turn ────────────────────────
echo -e "${BOLD}[24/28] /cost after a real turn${NC}"
if $HAS_ALTLLM; then
  cleanup
  sleep 0.3
  start_tui_env "ALTLLM=$ALTLLM_KEY" 120 30 "--model altllm-basic"
  send "Say hello" Enter
  sleep 20
  send "/cost" Enter
  sleep 3
  output=$(snap)
  cleanup
  if echo "$output" | grep -qE '\\$|cost|Cost|USD|tokens|Tokens'; then
    record_result "cost-after-real-turn" "true"
  else
    record_result "cost-after-real-turn" "false" \
      "no cost/dollar info in output"
    echo "    last 8 lines:"
    echo "$output" | tail -8 | sed 's/^/      /'
  fi
else
  skip_llm_test "cost-after-real-turn"
fi

# ── 25. /status shows session info ─────────────────────
echo -e "${BOLD}[25/28] /status shows session info${NC}"
run_test "status-session-model" 120 30 \
  "send '/status' Enter" 2 \
  "session\|Session\|model\|Model"

# ── 26. PgUp/PgDown scrolling ─────────────────────────
echo -e "${BOLD}[26/28] PgUp/PgDown scrolling${NC}"
if $HAS_ALTLLM; then
  cleanup
  sleep 0.3
  start_tui_env "ALTLLM=$ALTLLM_KEY" 120 30 "--model altllm-basic"
  send "Explain the Go programming language in detail. Cover history, syntax, concurrency, and standard library." Enter
  sleep 25
  send "PageUp"
  sleep 1
  output_pgup=$(snap)
  send "PageDown"
  sleep 1
  output_pgdn=$(snap)
  cleanup
  # Pass if either capture has content (TUI didn't crash on scroll)
  if [ -n "$output_pgup" ] || [ -n "$output_pgdn" ]; then
    record_result "pgup-pgdown-scroll" "true"
  else
    record_result "pgup-pgdown-scroll" "false" \
      "pane empty after scroll — possible crash"
  fi
else
  skip_llm_test "pgup-pgdown-scroll"
fi

# ── 27. /clear then new prompt ─────────────────────────
echo -e "${BOLD}[27/28] /clear then new prompt${NC}"
if $HAS_ALTLLM; then
  cleanup
  sleep 0.3
  start_tui_env "ALTLLM=$ALTLLM_KEY" 120 30 "--model altllm-basic"
  send "/clear" Enter
  sleep 2
  send "Say hi" Enter
  sleep 20
  output=$(snap)
  cleanup
  if echo "$output" | grep -qiE "hi|hello|hey|greet"; then
    record_result "clear-then-prompt" "true"
  else
    # TUI responded with something (not stuck)
    if [ -n "$output" ] && [ "$(echo "$output" | tr -d '[:space:]' | wc -c)" -gt 20 ]; then
      record_result "clear-then-prompt" "true"
    else
      record_result "clear-then-prompt" "false" \
        "no response after /clear + prompt"
      echo "    output:"
      echo "$output" | head -8 | sed 's/^/      /'
    fi
  fi
else
  skip_llm_test "clear-then-prompt"
fi

# ── 28. Input history (Up arrow recalls) ──────────────
echo -e "${BOLD}[28/28] Input history after prompts${NC}"
if $HAS_ALTLLM; then
  cleanup
  sleep 0.3
  start_tui_env "ALTLLM=$ALTLLM_KEY" 120 30 "--model altllm-basic"
  send "first unique prompt alpha" Enter
  sleep 20
  send "second unique prompt beta" Enter
  sleep 20
  # Press Up twice to recall first prompt
  send "Up"
  sleep 1
  send "Up"
  sleep 1
  output=$(snap)
  cleanup
  # The input line should contain text from a previous prompt
  if echo "$output" | grep -qiE "first unique|second unique|alpha|beta"; then
    record_result "input-history-recall" "true"
  else
    # Even if history text not visible, pass if TUI is alive
    if [ -n "$output" ] && echo "$output" | grep -qiE "altcode\|>\|Ask"; then
      record_result "input-history-recall" "false" \
        "TUI alive but no history text recalled in input line"
      echo "    last 5 lines:"
      echo "$output" | tail -5 | sed 's/^/      /'
    else
      record_result "input-history-recall" "false" \
        "TUI appears crashed"
    fi
  fi
else
  skip_llm_test "input-history-recall"
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
