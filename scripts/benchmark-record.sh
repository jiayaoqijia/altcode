#!/bin/bash
# Record altcode benchmark performance with asciinema
# Usage: ./scripts/benchmark-record.sh [model]
set -euo pipefail

MODEL="${1:-openai/deepseek/deepseek-chat-v3-0324}"
OUTDIR="docs/recordings"
mkdir -p "$OUTDIR"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
NAME=$(echo "$MODEL" | tr '/' '-')

echo "Recording altcode benchmark: $MODEL"
echo "Output: $OUTDIR/$NAME-$TIMESTAMP.cast"

# Create a script that runs the benchmarks
cat > /tmp/altcode-bench-script.sh << 'SCRIPT'
#!/bin/bash
set -e

echo "=== altcode benchmark ==="
echo ""

# Test 1: Startup
echo "$ altcode --version"
./dist/altcode --version
echo ""
sleep 0.5

# Test 2: Simple text
echo "$ altcode 'What is 7*8? Number only.'"
time ./dist/altcode "What is 7*8? Number only." 2>&1
echo ""
sleep 0.5

# Test 3: Tool call
echo "$ altcode 'Use ls to list files in cmd/. Be brief.'"
time ./dist/altcode "Use ls to list files in cmd/. Be brief." 2>&1
echo ""
sleep 0.5

# Test 4: Code generation
echo "$ altcode 'Write a Go function IsPrime(n int) bool. Only the function.'"
time ./dist/altcode "Write a Go function IsPrime(n int) bool. Only the function." 2>&1
echo ""
sleep 0.5

# Test 5: Multi-turn
echo "$ altcode 'Read first 3 lines of Makefile and explain the build target.'"
time ./dist/altcode "Read first 3 lines of Makefile and explain the build target." 2>&1
echo ""

echo "=== benchmark complete ==="
SCRIPT
chmod +x /tmp/altcode-bench-script.sh

# Record with asciinema
asciinema rec \
  --title "altcode benchmark: $MODEL" \
  --command "bash /tmp/altcode-bench-script.sh" \
  --cols 100 --rows 30 \
  --idle-time-limit 3 \
  "$OUTDIR/$NAME-$TIMESTAMP.cast"

echo ""
echo "Recording saved: $OUTDIR/$NAME-$TIMESTAMP.cast"
echo "View: asciinema play $OUTDIR/$NAME-$TIMESTAMP.cast"
echo "Upload: asciinema upload $OUTDIR/$NAME-$TIMESTAMP.cast"
