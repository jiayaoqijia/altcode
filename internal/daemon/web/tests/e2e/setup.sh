#!/usr/bin/env bash
# setup.sh — Build the altcode binary, start the daemon, run Playwright
# E2E tests, then tear everything down.
#
# Usage:
#   ./setup.sh          # run tests
#   ./setup.sh --headed # run tests in headed mode (for debugging)
#
# Environment:
#   ALTFIX_PORT    — port for the daemon (default 9200)
#   ALTFIX_BINARY  — path to pre-built binary (skips build)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../../.." && pwd)"
PORT="${ALTFIX_PORT:-9200}"
BINARY="${ALTFIX_BINARY:-/tmp/altcode-e2e}"
DAEMON_PID=""
DATA_DIR=""
SIGNING_KEY=""

cleanup() {
  if [ -n "$DAEMON_PID" ] && kill -0 "$DAEMON_PID" 2>/dev/null; then
    echo "Stopping daemon (PID $DAEMON_PID)..."
    kill "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  if [ -n "$DATA_DIR" ] && [ -d "$DATA_DIR" ]; then
    rm -rf "$DATA_DIR"
  fi
}
trap cleanup EXIT

# ---- Step 1: Build ----
if [ -z "${ALTFIX_BINARY:-}" ]; then
  echo "Building altcode binary..."
  cd "$REPO_ROOT"
  GOFLAGS=-mod=mod go build -o "$BINARY" ./cmd/altcode/
  echo "Built: $BINARY"
fi

# ---- Step 2: Start daemon ----
DATA_DIR="$(mktemp -d)"
SIGNING_KEY="$(openssl rand -hex 32)"

echo "Starting daemon on port $PORT (data: $DATA_DIR)..."
"$BINARY" daemon \
  --port "$PORT" \
  --data-dir "$DATA_DIR" \
  --github-client-id "e2e-test-client-id" \
  --github-client-secret "e2e-test-client-secret" \
  --signing-key "$SIGNING_KEY" \
  --auth-token "e2e-bearer-token" \
  --allowed-users "e2e-test-user" \
  --admin-users "e2e-test-user" \
  --test-mode \
  &
DAEMON_PID=$!
echo "Daemon PID: $DAEMON_PID"

# ---- Step 3: Wait for health ----
echo "Waiting for daemon to be ready..."
RETRIES=0
MAX_RETRIES=30
until curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1; do
  RETRIES=$((RETRIES + 1))
  if [ "$RETRIES" -ge "$MAX_RETRIES" ]; then
    echo "ERROR: Daemon did not become ready after ${MAX_RETRIES}s"
    exit 1
  fi
  sleep 1
done
echo "Daemon is ready."

# ---- Step 4: Run tests ----
cd "$SCRIPT_DIR"

# Install deps if needed.
if [ ! -d "node_modules" ]; then
  npm install --silent 2>/dev/null || true
fi

echo "Running Playwright E2E tests..."
EXTRA_ARGS=""
if [ "${1:-}" = "--headed" ]; then
  EXTRA_ARGS="--headed"
fi

ALTFIX_PORT="$PORT" npx playwright test $EXTRA_ARGS

echo "All E2E tests passed."
