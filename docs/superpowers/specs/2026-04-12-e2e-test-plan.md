# AltFix E2E Test Plan — Daemon + TUI + CLI (v2)

> **Designed as a 30-year AI/test expert.** Tests the full system against
> the altcode repo itself: daemon HTTP API, TUI interactive sessions, and
> CLI headless mode. Each test has explicit setup, commands, expected
> output, and pass/fail criteria.
>
> **v2**: Incorporates CC + Codex review feedback — added 15 missing
> tests (webhooks, checkpoints, concurrent tasks, budget, security scan,
> batch mode, --image, --commit, --fork-session), fixed order-dependent
> Phase 1 (each test creates own task), bumped TUI timeouts to 60s,
> added SKIP semantics for Phase 4 when agents unavailable.

## Test Infrastructure

### Test Repository — altcode itself

**The test target is the altcode repo itself** (`github.com/jiayaoqijia/altcode`).
This is a real 30K+ LoC Go project with 26 packages, 300+ tests, CI, MCP
servers, skills, and plugins. Testing against a real repo catches issues
that synthetic test beds miss.

```bash
# The repo is already cloned at the working directory
cd /home/coder/github/altcode
```

**Daemon tasks use altcode's own open issues and codebase:**
- Bug fix tasks: "Fix any go vet warnings in internal/daemon/"
- Feature tasks: "Add a --version flag to the daemon subcommand"
- Review tasks: "Review internal/daemon/store.go for error handling gaps"

### Environment Variables

```bash
export ALTFIX_AUTH_TOKEN="test-e2e-token-$(date +%s)"
export ALTFIX_TEST_REPO="https://github.com/jiayaoqijia/altcode"
export ALTFIX_TEST_REPO_DIR="/home/coder/github/altcode"
```

### Build Artifacts

```bash
GOFLAGS=-mod=mod go build -o /tmp/altcode-e2e ./cmd/altcode/
```

---

## Phase 1: Daemon Smoke Tests (no agents, just HTTP)

### Test 1.1: Daemon starts and serves health

**Setup:**
```bash
/tmp/altcode-e2e daemon --port 9199 --auth-token "$ALTFIX_AUTH_TOKEN" --data-dir /tmp/e2e-daemon &
DAEMON_PID=$!
sleep 2
```

**Execute:**
```bash
curl -s http://localhost:9199/health | jq .
```

**Expected:**
```json
{"status": "ok", "version": "dev"}
```

**Pass criteria:** HTTP 200, JSON body has `status: "ok"`.

**Teardown:**
```bash
kill $DAEMON_PID 2>/dev/null
```

---

### Test 1.2: Auth rejects unauthorized requests

**Execute:**
```bash
# No token
curl -s -o /dev/null -w "%{http_code}" http://localhost:9199/tasks
# Wrong token
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer wrong" http://localhost:9199/tasks
# Correct token
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" http://localhost:9199/tasks
```

**Expected:**
```
401
401
200
```

**Pass criteria:** First two return 401, third returns 200.

---

### Test 1.3: Create + Get + List task round-trip

**Execute:**
```bash
# Create
TASK_ID=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"https://github.com/test/repo","task":"fix the Add bug"}' \
  | jq -r '.id')
echo "Created task: $TASK_ID"

# Get
curl -s http://localhost:9199/tasks/$TASK_ID \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq '.task.status'

# List
curl -s http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq 'length'
```

**Expected:**
```
Created task: <32-char hex ID>
"pending"
1
```

**Pass criteria:** Task created with valid ID, status is "pending", list returns 1 task.

---

### Test 1.4: Duplicate delivery ID rejected

**Execute:**
```bash
# First submission
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"r","task":"t","delivery_id":"dedup-test-1"}'

# Second with same delivery_id
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"r","task":"t2","delivery_id":"dedup-test-1"}'
```

**Expected:**
```
201
500  (or 409 — unique constraint violation)
```

**Pass criteria:** Second submission fails with non-2xx status.

---

### Test 1.5: Stop task (cancel)

**Execute:**
```bash
TASK_ID=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"r","task":"long task"}' | jq -r '.id')

curl -s -X POST http://localhost:9199/tasks/$TASK_ID/stop \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq .
```

**Expected:** 202 with `{"status": "stopping"}`.

---

### Test 1.6: SSE event streaming

**Execute:**
```bash
TASK_ID=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"r","task":"t"}' | jq -r '.id')

# Connect to SSE, read for 5 seconds
timeout 5 curl -s -N http://localhost:9199/tasks/$TASK_ID/sse \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" 2>&1 | head -20
```

**Expected:** SSE format lines starting with `id:`, `event:`, `data:`, or `: heartbeat`.

**Pass criteria:** At least one heartbeat received within 5 seconds.

---

### Test 1.7: Steer with message

**Execute:**
```bash
curl -s -X POST http://localhost:9199/tasks/$TASK_ID/steer \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"message":"also add tests for edge cases"}' | jq .
```

**Expected:** 202 with `{"status": "acknowledged"}`.

---

### Test 1.8: Steer without message

**Execute:**
```bash
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:9199/tasks/$TASK_ID/steer \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}'
```

**Expected:** `400`.

---

### Test 1.9: Checkpoint list (empty)

**Execute:**
```bash
curl -s http://localhost:9199/tasks/$TASK_ID/checkpoints \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq 'length'
```

**Expected:** `0` (no checkpoints yet for a pending task).

---

### Test 1.10: Crash recovery on restart

**Execute:**
```bash
# Create a task, manually set it to "implementing" in the DB
sqlite3 /tmp/e2e-daemon/tasks.db "UPDATE tasks SET status='implementing' WHERE id='$TASK_ID'"

# Kill and restart daemon
kill $DAEMON_PID
sleep 1
/tmp/altcode-e2e daemon --port 9199 --auth-token "$ALTFIX_AUTH_TOKEN" --data-dir /tmp/e2e-daemon &
DAEMON_PID=$!
sleep 2

# Check — task should now be "failed"
curl -s http://localhost:9199/tasks/$TASK_ID \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq '.task.status'
```

**Expected:** `"failed"` with error message containing "daemon restart".

---

### Test 1.11: Webhook endpoint — valid signature accepted

```bash
SECRET="webhook-test-secret"
PAYLOAD='{"action":"labeled","label":{"name":"altfix"},"issue":{"number":1,"title":"test","body":"test body"},"repository":{"full_name":"test/repo"}}'
SIG=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print "sha256="$2}')

# Restart daemon with webhook secret
kill $DAEMON_PID 2>/dev/null; sleep 1
/tmp/altcode-e2e daemon --port 9199 --auth-token "$ALTFIX_AUTH_TOKEN" \
  --data-dir /tmp/e2e-daemon --webhook-secret "$SECRET" &
DAEMON_PID=$!; sleep 2

curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:9199/webhooks/github \
  -H "X-GitHub-Event: issues" \
  -H "X-GitHub-Delivery: test-delivery-1" \
  -H "X-Hub-Signature-256: $SIG" \
  -d "$PAYLOAD"
```

**Expected:** `200` (webhook accepted, task created).

---

### Test 1.12: Checkpoint create + list

```bash
# Create a task and manually add a checkpoint via API
TASK_ID=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"r","task":"checkpoint test"}' | jq -r '.id')

# List checkpoints (may be empty for a pending task)
curl -s http://localhost:9199/tasks/$TASK_ID/checkpoints \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq 'type'
```

**Expected:** Returns a JSON array (even if empty).

---

### Test 1.13: Concurrent task submission

```bash
# Submit 3 tasks rapidly
for i in 1 2 3; do
  curl -s -X POST http://localhost:9199/tasks \
    -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"repo_url\":\"r\",\"task\":\"concurrent test $i\"}" &
done
wait

COUNT=$(curl -s http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq 'length')
echo "Total tasks: $COUNT"
```

**Expected:** COUNT includes all 3 new tasks (no deadlock, no corruption).

---

### Test 1.14: Steer on completed task → 409

```bash
TASK_ID=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"r","task":"will complete"}' | jq -r '.id')

# Manually mark as merged for testing
sqlite3 /tmp/e2e-daemon/tasks.db "UPDATE tasks SET status='merged' WHERE id='$TASK_ID'"

curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:9199/tasks/$TASK_ID/steer \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"message":"too late"}'
```

**Expected:** `409` (task already completed).

---

### Test 1.15: SSE replay with Last-Event-ID

```bash
TASK_ID=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_url":"r","task":"sse replay test"}' | jq -r '.id')

# Mark terminal so SSE closes quickly
sqlite3 /tmp/e2e-daemon/tasks.db "UPDATE tasks SET status='merged' WHERE id='$TASK_ID'"

# Connect with Last-Event-ID: 0 — should get any events then close
timeout 5 curl -s -N http://localhost:9199/tasks/$TASK_ID/sse \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Last-Event-ID: 0" 2>&1 | head -5
```

**Expected:** SSE response with event format (or empty + close for terminal task).

---

## Phase 2: CLI Headless Tests (real provider calls)

These tests use the altcode CLI (not the daemon) to verify the Phase 1-13 CLI feature parity work against a real provider.

### Test 2.1: Basic prompt → text response

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 "What is 2+2? Answer with just the number." 2>/dev/null
```

**Expected:** Output contains `4`.

---

### Test 2.2: --output-format json

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --output-format json "Say hello" 2>/dev/null | jq '.text'
```

**Expected:** JSON object with `.text` field containing a greeting.

---

### Test 2.3: --output-format stream-json (JSONL)

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --json "Say hi" 2>/dev/null | head -3 | jq -r '.type'
```

**Expected:** First lines are valid JSONL with `type` field (e.g. `text_delta`).

---

### Test 2.4: --permission-mode plan (read-only)

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --permission-mode plan \
  "Write 'test' to /tmp/plan-test.txt" 2>/dev/null
ls /tmp/plan-test.txt 2>&1
```

**Expected:** File does NOT exist (plan mode denies writes).

---

### Test 2.5: --dry-run alias

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --dry-run \
  "Write 'test' to /tmp/dryrun-test.txt" 2>/dev/null
ls /tmp/dryrun-test.txt 2>&1
```

**Expected:** File does NOT exist (dry-run = plan mode).

---

### Test 2.6: --print-cost shows cost on stderr

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --print-cost "Say hello" 2>&1 >/dev/null | grep -c "altcode:"
```

**Expected:** `1` (one cost summary line on stderr).

---

### Test 2.7: --print-tools shows tool calls on stderr

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --print-tools \
  "Read the first line of README.md" 2>&1 >/dev/null | grep -c "\[read\]"
```

**Expected:** `1` or more (tool call logged to stderr).

---

### Test 2.8: --show-system prints instructions

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --show-system "hi" 2>&1 >/dev/null | head -5
```

**Expected:** Output contains `===` instruction headers on stderr.

---

### Test 2.9: --file context injection

```bash
echo "package main" > /tmp/e2e-ctx.go
/tmp/altcode-e2e --model openai/gpt-5.4 --file /tmp/e2e-ctx.go \
  "What language is this file written in? Answer in one word." 2>/dev/null
```

**Expected:** Output contains `Go`.

---

### Test 2.10: --prompt-file reads prompt from file

```bash
echo "What is the capital of France? One word answer." > /tmp/e2e-prompt.txt
/tmp/altcode-e2e --model openai/gpt-5.4 --prompt-file /tmp/e2e-prompt.txt 2>/dev/null
```

**Expected:** Output contains `Paris`.

---

### Test 2.11: --max-turns limits agent loop

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --max-turns 1 \
  "Read README.md, then write a summary to /tmp/summary.txt" 2>&1 | grep -i "budget\|max.turns"
```

**Expected:** Budget exceeded message (1 turn is not enough for read+write).

---

### Test 2.12: --print-tree shows tool tree at end

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --print-tree \
  "Read the first 5 lines of CLAUDE.md" 2>&1 >/dev/null | grep -c "tool tree"
```

**Expected:** `1` (tool tree header on stderr).

---

### Test 2.13: --save-cost writes JSON cost report

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --save-cost /tmp/e2e-cost.json \
  "Say hello" 2>/dev/null
jq '.total_usd' /tmp/e2e-cost.json
```

**Expected:** Valid JSON with numeric `total_usd` field.

---

### Test 2.14: --save-transcript writes JSONL

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --print-tree --save-transcript /tmp/e2e-transcript.jsonl \
  "Say hello" 2>/dev/null
wc -l /tmp/e2e-transcript.jsonl
head -1 /tmp/e2e-transcript.jsonl | jq '.type'
```

**Expected:** Multiple JSONL lines, first line has a valid `type` field.

---

### Test 2.15: Validation errors exit 64

```bash
/tmp/altcode-e2e --output-format yaml "hi" 2>/dev/null; echo $?
/tmp/altcode-e2e --permission-mode yolo "hi" 2>/dev/null; echo $?
/tmp/altcode-e2e --quiet --verbose "hi" 2>/dev/null; echo $?
```

**Expected:** All three exit with code `64` (EX_USAGE) or `1` (cobra mutex).

---

### Test 2.16: --doctor health check

```bash
/tmp/altcode-e2e --doctor 2>&1 | grep -c "✓"
```

**Expected:** At least 2 checkmarks (providers configured, git detected).

---

### Test 2.17: --print-config redacts secrets

```bash
/tmp/altcode-e2e --print-config 2>&1 | grep -c "redacted"
```

**Expected:** At least 1 (API keys redacted).

---

### Test 2.18: --commit creates a git commit (in temp repo)

```bash
cd /tmp && mkdir e2e-commit-test && cd e2e-commit-test
git init -q && git config user.email "t@t" && git config user.name "test"
echo "hi" > README.md && git add . && git commit -q -m "init"

/tmp/altcode-e2e --model openai/gpt-5.4 --permission-mode bypass --commit \
  "Write 'test' to hello.txt" 2>/dev/null

git log --oneline | head -2
cd /home/coder/github/altcode
```

**Expected:** Second commit exists with `[altcode]` prefix.

---

### Test 2.19: --save-diff writes diff file

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --permission-mode bypass \
  --save-diff /tmp/e2e-diff.patch \
  "Write 'test' to /tmp/e2e-diff-test.txt" 2>/dev/null
ls -la /tmp/e2e-diff.patch
```

**Expected:** Diff file exists (may be empty if no tracked files changed).

---

### Test 2.20: --system appends to system prompt

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 --show-system \
  --system "You are a pirate. Always say arr." \
  "Say hello" 2>&1 >/dev/null | grep -c "pirate"
```

**Expected:** `1` (system prompt contains "pirate" on stderr).

---

### Test 2.21: --fork-session creates a new session

```bash
# Create a session first
/tmp/altcode-e2e --model openai/gpt-5.4 "Say hello" 2>/dev/null

# Get the session ID
SESSION_ID=$(/tmp/altcode-e2e sessions 2>/dev/null | head -1 | awk '{print $1}')

# Fork it
/tmp/altcode-e2e --fork-session "$SESSION_ID" --model openai/gpt-5.4 \
  "Continue from the forked session" 2>/dev/null; echo "rc=$?"
```

**Expected:** Exits 0 (fork succeeded, new session created).

---

### Test 2.22: --prompt-each batch mode

```bash
echo "What is 2+2?" > /tmp/e2e-batch.txt
echo "What is 3+3?" >> /tmp/e2e-batch.txt

/tmp/altcode-e2e --model openai/gpt-5.4 --prompt-each /tmp/e2e-batch.txt \
  --quiet 2>&1 | grep -c "batch"
```

**Expected:** At least 2 `[batch N/2]` progress lines on stderr.

---

### Test 2.23: --hook registers ad-hoc hook

```bash
/tmp/altcode-e2e --model openai/gpt-5.4 \
  --hook "PreToolUse:echo hook-fired" \
  "Read the first line of README.md" 2>&1 | grep -c "hook-fired"
```

**Expected:** Hook output appears (at least 1 match, proving the hook fired).

---

## Phase 3: TUI Interactive Tests (tmux)

These tests launch altcode TUI in tmux and verify interactive behavior.

### Test 3.1: TUI starts and shows HUD

```bash
tmux kill-session -t e2e 2>/dev/null
tmux new-session -d -s e2e -x 160 -y 45 "/tmp/altcode-e2e --model openai/gpt-5.4"
sleep 5
tmux capture-pane -t e2e -p | tail -5
```

**Pass criteria:** Last line shows input placeholder "Ask anything..." and HUD shows model name.

---

### Test 3.2: /help renders all commands

```bash
tmux send-keys -t e2e "/help" Enter
sleep 3
tmux capture-pane -t e2e -p -S -50 | grep -c "/"
```

**Pass criteria:** At least 20 slash commands listed.

---

### Test 3.3: /doctor shows health

```bash
tmux send-keys -t e2e "/doctor" Enter
sleep 3
tmux capture-pane -t e2e -p | grep -c "✓"
```

**Pass criteria:** At least 2 checkmarks.

---

### Test 3.4: /tools lists registered tools

```bash
tmux send-keys -t e2e "/tools" Enter
sleep 3
tmux capture-pane -t e2e -p | grep -c "read\|write\|bash\|edit"
```

**Pass criteria:** At least 4 core tools listed.

---

### Test 3.5: Real prompt with tool dispatch

```bash
tmux send-keys -t e2e "Read the first 3 lines of README.md" Enter
sleep 60  # bumped from 30s per reviewer feedback — provider latency spikes
tmux capture-pane -t e2e -p -S -30 | grep -c "✓.*read"
```

**Pass criteria:** At least 1 successful read tool call (✓ read).

---

### Test 3.6: /cost shows accumulated cost

```bash
tmux send-keys -t e2e "/cost" Enter
sleep 2
tmux capture-pane -t e2e -p | grep -c "Total:"
```

**Pass criteria:** Cost breakdown with "Total:" line.

---

### Test 3.7: /context shows token breakdown

```bash
tmux send-keys -t e2e "/context" Enter
sleep 2
tmux capture-pane -t e2e -p | grep -c "System:"
```

**Pass criteria:** Context window breakdown visible.

---

### Test 3.8: Cancel mid-turn clears HUD

```bash
tmux send-keys -t e2e "Write a 1000 word essay about Go programming" Enter
sleep 5
tmux send-keys -t e2e Escape
sleep 2
tmux capture-pane -t e2e -p | tail -5
```

**Pass criteria:** "[cancelled]" appears. HUD does NOT show stale tool name (Bug 2 fix verified).

---

### Test 3.9: Context bar shows correct percentage

```bash
tmux capture-pane -t e2e -p | grep -o '[0-9]*%'
```

**Pass criteria:** Shows a reasonable percentage (1-20%, not 100%+ from the Bug 3 double-counting fix).

---

### Test 3.10: /memory shows loaded memories

```bash
tmux send-keys -t e2e "/memory" Enter
sleep 2
tmux capture-pane -t e2e -p -S -10 | grep -ci "memor"
```

**Pass criteria:** At least 1 line mentioning memory (loaded or "no memories").

---

### Test 3.11: /mcp shows configured servers

```bash
tmux send-keys -t e2e "/mcp" Enter
sleep 2
tmux capture-pane -t e2e -p | grep -c "stdio\|sse"
```

**Pass criteria:** MCP servers listed with transport type.

---

### Test 3.12: TUI exit

```bash
tmux send-keys -t e2e C-d
sleep 2
tmux has-session -t e2e 2>&1
```

**Pass criteria:** Session no longer exists (Ctrl+D quit).

---

## Phase 4: Daemon + Agent Integration (real task execution)

These tests run the daemon with REAL agent subprocesses against the altcode repo.
**Requires:** codex or claude CLI installed and authenticated.

**Environment gate:**
```bash
# Check if agents are available
if ! command -v codex &>/dev/null && ! command -v claude &>/dev/null; then
    echo "SKIP: Phase 4 requires codex or claude CLI"
    # In CI release-gate: exit 1 (FAIL)
    # In developer smoke: exit 0 (SKIP)
fi
```

### Test 4.1: Submit task via API against altcode repo

```bash
# Start daemon
/tmp/altcode-e2e daemon --port 9199 --auth-token "$ALTFIX_AUTH_TOKEN" \
  --data-dir /tmp/e2e-daemon-int &
DAEMON_PID=$!
sleep 2

# Submit a real task against altcode's own codebase
TASK_ID=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"repo_url\":\"$ALTFIX_TEST_REPO\",\"task\":\"Run go vet on internal/daemon/ and fix any warnings\"}" \
  | jq -r '.id')

echo "Submitted task: $TASK_ID"
```

**Pass criteria:** Task ID returned, status transitions from `pending` → `planning`.

---

### Test 4.2: Poll task status during execution

```bash
for i in $(seq 1 30); do
  STATUS=$(curl -s http://localhost:9199/tasks/$TASK_ID \
    -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq -r '.task.status')
  echo "[$i] Status: $STATUS"
  if [ "$STATUS" = "merged" ] || [ "$STATUS" = "failed" ]; then break; fi
  sleep 10
done
```

**Pass criteria:** Task reaches a terminal status (merged or failed) within 5 minutes.

---

### Test 4.3: SSE events arrive during execution

```bash
# Start listening before submitting
TASK_ID2=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"repo_url\":\"$ALTFIX_TEST_REPO\",\"task\":\"Add a --version flag to the daemon subcommand that prints the altcode version\"}" \
  | jq -r '.id')

timeout 120 curl -s -N http://localhost:9199/tasks/$TASK_ID2/sse \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" > /tmp/e2e-sse.log &
SSE_PID=$!
sleep 60
kill $SSE_PID 2>/dev/null

cat /tmp/e2e-sse.log | grep "^event:" | sort -u
```

**Pass criteria:** At least `phase_started` and `phase_completed` events received.

---

### Test 4.4: Steer mid-execution

```bash
TASK_ID3=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"repo_url\":\"$ALTFIX_TEST_REPO\",\"task\":\"Review internal/daemon/store.go for missing error handling\"}" \
  | jq -r '.id')

sleep 15  # Wait for task to start

curl -s -X POST http://localhost:9199/tasks/$TASK_ID3/steer \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"message":"also handle division by zero"}' | jq .

# Verify steer event was logged
sleep 5
curl -s http://localhost:9199/tasks/$TASK_ID3/sse \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Last-Event-ID: 0" 2>&1 | grep "user_steer"
```

**Pass criteria:** Steer returns 202. Event log contains `user_steer` event.

---

### Test 4.5: Cancel running task

```bash
TASK_ID4=$(curl -s -X POST http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"repo_url\":\"$ALTFIX_TEST_REPO\",\"task\":\"Refactor internal/daemon/orchestrator.go to extract phase logic into separate methods\"}" \
  | jq -r '.id')

sleep 10  # Let it start

curl -s -X POST http://localhost:9199/tasks/$TASK_ID4/stop \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq .

sleep 5
STATUS=$(curl -s http://localhost:9199/tasks/$TASK_ID4 \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq -r '.task.status')
echo "Final status: $STATUS"
```

**Pass criteria:** Status is `cancelled` or `failed` (not still `implementing`).

---

## Phase 5: Cross-Component Regression Tests

### Test 5.1: CLI --print-config doesn't leak daemon secrets

```bash
/tmp/altcode-e2e --print-config 2>&1 | grep -i "sk-\|ghp_\|token" | grep -v "redacted"
```

**Pass criteria:** Empty output (no unredacted secrets).

---

### Test 5.2: Daemon store survives restart

```bash
# Count tasks before restart
BEFORE=$(curl -s http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq 'length')

kill $DAEMON_PID
sleep 2
/tmp/altcode-e2e daemon --port 9199 --auth-token "$ALTFIX_AUTH_TOKEN" \
  --data-dir /tmp/e2e-daemon-int &
DAEMON_PID=$!
sleep 2

AFTER=$(curl -s http://localhost:9199/tasks \
  -H "Authorization: Bearer $ALTFIX_AUTH_TOKEN" | jq 'length')

echo "Before: $BEFORE, After: $AFTER"
```

**Pass criteria:** AFTER >= BEFORE (tasks persist across restart).

---

### Test 5.3: Full build + vet + test gate

```bash
GOFLAGS=-mod=mod go build ./...
GOFLAGS=-mod=mod go vet ./...
GOFLAGS=-mod=mod go test ./... -race -count=1 -timeout=300s 2>&1 | tail -5
```

**Pass criteria:** ALL pass. Zero failures. Zero race conditions.

---

## Automation Script

Save as `scripts/e2e-test.sh` and run:

```bash
#!/bin/bash
set -e

BIN=/tmp/altcode-e2e
GOFLAGS=-mod=mod go build -o $BIN ./cmd/altcode/

PASS=0
FAIL=0

run_test() {
    local name="$1"
    shift
    echo -n "  $name ... "
    if eval "$@" >/dev/null 2>&1; then
        echo "PASS"
        ((PASS++))
    else
        echo "FAIL"
        ((FAIL++))
    fi
}

echo "=== Phase 1: Daemon Smoke ==="
$BIN daemon --port 9199 --auth-token test --data-dir /tmp/e2e-$$ &
PID=$!; sleep 2

run_test "1.1 health" 'curl -sf http://localhost:9199/health | jq -e .status'
run_test "1.2 auth reject" '[ $(curl -so /dev/null -w "%{http_code}" http://localhost:9199/tasks) = "401" ]'
run_test "1.3 create task" 'curl -sf -X POST http://localhost:9199/tasks -H "Authorization: Bearer test" -H "Content-Type: application/json" -d "{\"repo_url\":\"r\",\"task\":\"t\"}" | jq -e .id'

kill $PID 2>/dev/null; wait $PID 2>/dev/null

echo ""
echo "=== Phase 2: CLI Headless ==="
run_test "2.15 validation exit 64" '/tmp/altcode-e2e --output-format yaml "hi" 2>/dev/null; [ $? -eq 64 ]'
run_test "2.16 doctor" '/tmp/altcode-e2e --doctor 2>&1 | grep -q "✓"'
run_test "2.17 print-config redacts" '/tmp/altcode-e2e --print-config 2>&1 | grep -q "redacted"'

echo ""
echo "=== Phase 5: Regression ==="
run_test "5.3 build+vet+test" 'GOFLAGS=-mod=mod go build ./... && GOFLAGS=-mod=mod go vet ./...'

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ $FAIL -eq 0 ] && echo "ALL PASS" || echo "FAILURES DETECTED"
exit $FAIL
```

---

## CC + Codex Review Checklist

When reviewing E2E results, both reviewers must verify:

1. **Happy path**: Does a simple bug-fix task complete end-to-end (submit → plan → implement → review → PR)?
2. **Unhappy path**: Does cancel work? Does timeout fire? Does crash recovery mark orphans?
3. **Edge cases**: Duplicate delivery IDs, empty task description, steer on completed task, SSE reconnect with Last-Event-ID.
4. **Security**: Auth rejects bad tokens, --print-config redacts, webhook signature verification.
5. **Data integrity**: Tasks persist across daemon restart, events are monotonically ordered, checkpoints match git SHAs.
6. **Performance**: Daemon starts in <2s, health responds in <100ms, SSE heartbeat every 2s, task status update within 1s of phase transition.
