#!/bin/bash
# Precise timing benchmark for altcode across models
set -euo pipefail

MODELS=(
  "openai/deepseek/deepseek-chat-v3-0324|DeepSeek"
  "openai/qwen/qwen3-coder-next|Qwen"
  "openai/moonshotai/kimi-k2.5|Kimi"
)

# Add Claude if available
if [ -f "$HOME/.claude/.credentials.json" ]; then
  MODELS+=("anthropic/claude-haiku-4-5-20251001|Claude")
fi

# Add GPT if available
if [ -f "$HOME/.codex/auth.json" ]; then
  MODELS+=("openai/gpt-5.4|GPT-5.4")
fi

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║              altcode Performance Benchmark                   ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""

echo "Test 1: Time to First Token (simple prompt)"
echo "──────────────────────────────────────────────"
for entry in "${MODELS[@]}"; do
  IFS='|' read -r model name <<< "$entry"
  
  if [[ "$model" == anthropic/* ]]; then
    CONFIG=""  # auto-detect
  elif [[ "$model" == openai/gpt* ]]; then
    CONFIG=""  # auto-detect via codex relay  
  else
    CONFIG="--config /tmp/altcode-openrouter.json"
  fi
  
  START=$(date +%s%N)
  timeout 30 ./dist/altcode $CONFIG --model "$model" "hi" >/dev/null 2>&1
  END=$(date +%s%N)
  MS=$(( (END - START) / 1000000 ))
  printf "  %-12s %5dms\n" "$name" "$MS"
done

echo ""
echo "Test 2: Tool Call Latency (ls)"  
echo "──────────────────────────────────────────────"
for entry in "${MODELS[@]}"; do
  IFS='|' read -r model name <<< "$entry"
  
  if [[ "$model" == anthropic/* ]]; then
    CONFIG=""
  elif [[ "$model" == openai/gpt* ]]; then
    CONFIG=""
  else
    CONFIG="--config /tmp/altcode-openrouter.json"
  fi
  
  START=$(date +%s%N)
  timeout 45 ./dist/altcode $CONFIG --model "$model" "Use ls on cmd/. Brief." >/dev/null 2>&1
  END=$(date +%s%N)
  MS=$(( (END - START) / 1000000 ))
  printf "  %-12s %5dms\n" "$name" "$MS"
done

echo ""
echo "Test 3: Startup Time (--version)"
echo "──────────────────────────────────────────────"
for i in 1 2 3; do
  START=$(date +%s%N)
  ./dist/altcode --version >/dev/null 2>&1
  END=$(date +%s%N)
  MS=$(( (END - START) / 1000000 ))
  printf "  Run %d: %3dms\n" "$i" "$MS"
done

echo ""
echo "Done."
