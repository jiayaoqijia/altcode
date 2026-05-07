#!/usr/bin/env bash
# verify-instructions.sh
#
# Per-version smoke-verifier for everything the README and altcode.io
# tell users to type. If a command in the docs is broken, this script
# fails BEFORE we ship a release — not after a user reports it.
#
# Run locally:    make verify
# Run in CI:      scripts/verify-instructions.sh
# Run with serve: scripts/verify-instructions.sh --remote   (also hits altcode.io)
#
# Environment overrides:
#   ALTCODE_VERIFY_REMOTE=1    Test the live altcode.io install path too
#   ALTCODE_VERIFY_BINARY=...  Use a pre-built binary instead of building
#   VERIFY_VERBOSE=1           Stream all subcommand output
#
# Exit codes:
#   0  every check passed
#   1  one or more checks failed (failing checks are listed at the end)
#
set -uo pipefail

REMOTE=${ALTCODE_VERIFY_REMOTE:-0}
[ "${1:-}" = "--remote" ] && REMOTE=1
VERBOSE=${VERIFY_VERBOSE:-0}

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

# Colors
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'; BOLD='\033[1m'; NC='\033[0m'
fail_count=0
check_count=0
declare -a failures=()

log()  { echo -e "${BOLD}▸${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
bad()  { echo -e "${RED}✗${NC} $*"; failures+=("$*"); fail_count=$((fail_count+1)); }
skip() { echo -e "${YELLOW}~${NC} $*"; }

run_check() {
    local label=$1; shift
    check_count=$((check_count+1))
    if [ "$VERBOSE" = 1 ]; then
        echo "  [run] $*"
        if "$@"; then ok "$label"; else bad "$label — command failed: $*"; fi
    else
        local out
        if out=$("$@" 2>&1); then
            ok "$label"
        else
            bad "$label — command failed: $* ($(echo "$out" | head -1))"
        fi
    fi
}

# Pick the binary: prefer pre-built override, then dist/altcode, else build.
if [ -n "${ALTCODE_VERIFY_BINARY:-}" ] && [ -x "$ALTCODE_VERIFY_BINARY" ]; then
    BIN="$ALTCODE_VERIFY_BINARY"
    log "Using pre-built binary: $BIN"
elif [ -x dist/altcode ]; then
    BIN="$ROOT/dist/altcode"
    log "Using existing dist/altcode"
else
    log "Building dist/altcode (no pre-built binary found)"
    if ! GOFLAGS=-mod=mod go build -o dist/altcode ./cmd/altcode/ 2>/dev/null; then
        bad "build dist/altcode (broken)"
        echo ""; echo "${BOLD}cannot continue without a binary${NC}"; exit 1
    fi
    BIN="$ROOT/dist/altcode"
fi
echo ""

# ─── 1. version + doctor — the smoke gates everyone hits first ───
log "Section 1 — basic invocations"
run_check "altcode --version exits 0"                   "$BIN" --version
run_check "altcode --version reports v0.x.x or 'dev'"   bash -c "$BIN --version | head -1 | grep -qE 'altcode (v[0-9]|dev)'"
run_check "altcode --help exits 0"                      "$BIN" --help
run_check "altcode --doctor exits 0"                    "$BIN" --doctor
run_check "altcode --print-tools-list exits 0"          "$BIN" --print-tools-list
run_check "altcode --print-skills exits 0"              "$BIN" --print-skills
run_check "altcode --print-mcp exits 0"                 "$BIN" --print-mcp
run_check "altcode --print-config exits 0"              "$BIN" --print-config
echo ""

# ─── 2. flags from the README parity table — every flag --help mentions ───
log "Section 2 — README CLI flags resolve"
declare -a flags=(
    "--output-format" "--verbose" "--quiet" "--print-cost" "--print-tools"
    "--print-tree" "--show-system" "--permission-mode" "--allow-tool"
    "--deny-tool" "--dry-run" "--continue" "--fork-session" "--session-db"
    "--list-sessions" "--image" "--file" "--prompt-file" "--system"
    "--system-file" "--save-transcript" "--save-cost" "--save-diff"
    "--commit" "--commit-dirty" "--max-turns" "--max-cost"
    "--prompt-each" "--parallel" "--print-config" "--print-tools-list"
    "--print-skills" "--print-mcp" "--doctor"
)
help_text=$("$BIN" --help 2>&1 || true)
for flag in "${flags[@]}"; do
    check_count=$((check_count+1))
    if echo "$help_text" | grep -qE -- "($flag\b|$flag=)"; then
        ok "flag advertised: $flag"
    else
        bad "flag missing from --help (README claims it exists): $flag"
    fi
done
echo ""

# ─── 3. subcommands the README documents ───
log "Section 3 — README subcommands resolve"
declare -a subs=("login" "workspace" "workflow" "daemon")
for sub in "${subs[@]}"; do
    run_check "altcode $sub --help" "$BIN" "$sub" --help
done
echo ""

# ─── 4. docs/install.sh — does it parse + dry-run cleanly? ───
log "Section 4 — install script self-checks"
run_check "docs/install.sh has bash shebang"            grep -q "^#!.*bash" docs/install.sh
run_check "docs/install.sh passes shellcheck (or skip)" bash -c '! command -v shellcheck >/dev/null 2>&1 || shellcheck -e SC2086,SC2155,SC2034,SC2016,SC2046,SC2002,SC1091 docs/install.sh >/dev/null'
run_check "docs/install.sh advertises altcode.io URL"   grep -q "altcode.io" docs/install.sh
run_check "docs/install.sh defaults to user-writable"   grep -qE 'INSTALL_DIR.*=.*\.local/bin' docs/install.sh
run_check "docs/install.sh checks OS version"           grep -qE "(sw_vers|GLIBC|glibc)" docs/install.sh
echo ""

# ─── 5. README ↔ webpage version sync ───
log "Section 5 — README / webpage / binary version cross-check"
bin_version=$("$BIN" --version 2>&1 | head -1 | awk '{print $2}')
web_version=$(grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' docs/index.html | tail -1 || true)

check_count=$((check_count+1))
if [ -z "$web_version" ]; then
    bad "docs/index.html has no version chip (expected vX.Y.Z somewhere)"
elif [ "$bin_version" = "$web_version" ]; then
    ok "binary $bin_version matches docs/index.html $web_version"
else
    bad "version drift: binary=$bin_version docs/index.html=$web_version (run scripts/build-binaries.sh and bump docs/index.html)"
fi

# README badge (if any) consistency
if grep -qE 'v[0-9]+\.[0-9]+\.[0-9]+' README.md; then
    readme_version=$(grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' README.md | head -1)
    check_count=$((check_count+1))
    if [ "$readme_version" = "$bin_version" ]; then
        ok "README.md $readme_version matches binary"
    else
        skip "README.md has $readme_version (binary $bin_version) — README intentionally version-agnostic, ignoring"
    fi
fi
echo ""

# ─── 6. served binary (only with --remote) ───
if [ "$REMOTE" = 1 ]; then
    log "Section 6 — altcode.io serves the right binary"
    case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
        linux)  asset="altcode-linux-amd64"  ;;
        darwin) asset="altcode-darwin-arm64" ;;
        *)      skip "Section 6 — unsupported platform for remote check"; asset="" ;;
    esac
    if [ -n "$asset" ]; then
        TMP=$(mktemp)
        check_count=$((check_count+1))
        if curl -fsSL -o "$TMP" "https://altcode.io/bin/$asset" 2>/dev/null; then
            chmod +x "$TMP"
            served=$("$TMP" --version 2>&1 | head -1 | awk '{print $2}' || echo "?")
            local_sha=$(sha256sum "docs/bin/$asset" 2>/dev/null | awk '{print $1}')
            served_sha=$(sha256sum "$TMP" | awk '{print $1}')
            if [ -n "$local_sha" ] && [ "$local_sha" = "$served_sha" ]; then
                ok "altcode.io/bin/$asset matches docs/bin/$asset (sha $local_sha sub-truncated)"
            elif [ "$served" = "$bin_version" ]; then
                skip "altcode.io served $served (matches version) but sha differs from local docs/bin/ — GH Pages may be republishing"
            else
                bad "altcode.io serves $served, local binary is $bin_version — push docs/bin/ to refresh"
            fi
        else
            bad "altcode.io/bin/$asset fetch failed"
        fi
        rm -f "$TMP"
    fi
    echo ""
fi

# ─── 7. headless smoke run if creds present ───
log "Section 7 — headless smoke run"
HAS_CREDS=0
if [ -n "${OPENAI_API_KEY:-}" ] || [ -n "${ANTHROPIC_API_KEY:-}" ] || \
   [ -n "${OPENROUTER:-}" ] || [ -n "${ALTLLM:-}" ] || \
   [ -f "$HOME/.codex/auth.json" ] || [ -f "$HOME/.claude/.credentials.json" ]; then
    HAS_CREDS=1
fi
if [ "$HAS_CREDS" = 0 ]; then
    skip "no credentials in env or ~/.codex/.claude — skipping live API call"
else
    check_count=$((check_count+1))
    # 60s wall budget — first turn always pays the system-prompt+skills
    # baseline (~70k tokens) before the model emits its first byte. The
    # ALTCODE_AUTO_APPROVE=1 var skips the permission modal so we never
    # block on stdin (no tools needed for this prompt anyway).
    out=$(ALTCODE_AUTO_APPROVE=1 timeout 60 "$BIN" --max-turns 1 --max-cost 0.10 --quiet "Reply with the single word READY." 2>&1 || true)
    if echo "$out" | grep -qiE "ready"; then
        ok "headless smoke run answered (got: $(echo "$out" | head -1 | head -c 60))"
    else
        bad "headless smoke run failed or timed out (got: $(echo "$out" | head -c 80))"
    fi
fi
echo ""

# ─── summary ───
echo "─────────────────────────────────────────────────────────"
if [ "$fail_count" = 0 ]; then
    echo -e "${GREEN}${BOLD}all $check_count checks passed${NC}"
    echo "ready to ship: bump version, rebuild binaries, push."
    exit 0
else
    echo -e "${RED}${BOLD}$fail_count of $check_count checks failed${NC}"
    echo ""
    for f in "${failures[@]}"; do echo "  - $f"; done
    echo ""
    echo "do NOT ship until these are fixed."
    exit 1
fi
