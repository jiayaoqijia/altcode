#!/bin/bash
# altcode installer
# Usage: curl -fsSL https://altcode.io/install.sh | bash
set -euo pipefail

VERSION="${ALTCODE_VERSION:-latest}"
# Default to a user-writable location so `curl … | bash` doesn't need
# sudo. Mirrors what cargo, pipx, uv, rustup, and other modern
# installers do. Override with ALTCODE_INSTALL_DIR=/usr/local/bin for
# system-wide install.
DEFAULT_INSTALL_DIR="$HOME/.local/bin"
INSTALL_DIR="${ALTCODE_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
REPO="jiayaoqijia/altcode"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${BLUE}▸${NC} $*"; }
ok()    { echo -e "${GREEN}✓${NC} $*"; }
warn()  { echo -e "${YELLOW}!${NC} $*"; }
fail()  { echo -e "${RED}✗${NC} $*"; exit 1; }

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       fail "Unsupported architecture: $ARCH" ;;
esac

EXT=""
[ "$OS" = "windows" ] && EXT=".exe"
BINARY_NAME="altcode-${OS}-${ARCH}${EXT}"

# OS version compatibility check. Best-effort: warn loudly when the
# host is below the binary's minimum target so users on ancient
# distros / macOS versions get a useful error instead of a cryptic
# 'GLIBC_X.YZ not found' or 'Bad CPU type in executable'.
#
# Go 1.22+ amd64 binaries: glibc >= 2.17 (CentOS 7, Ubuntu 14.04+).
# Go 1.22+ darwin/arm64: macOS 11.0+. darwin/amd64: macOS 10.15+.
OS_DETAIL=""
case "$OS" in
    darwin)
        OS_VER=$(sw_vers -productVersion 2>/dev/null || echo "unknown")
        OS_DETAIL="macOS ${OS_VER}"
        if [ "$OS_VER" != "unknown" ]; then
            major=${OS_VER%%.*}
            min=10
            [ "$ARCH" = "arm64" ] && min=11
            if [ "$major" != "" ] && [ "$major" -lt "$min" ]; then
                warn "macOS ${OS_VER} is older than the supported minimum (${min}.0) for ${ARCH}."
                warn "altcode may fail with 'Bad CPU type in executable'. Continuing anyway."
            fi
        fi
        ;;
    linux)
        # glibc version detection — `ldd --version` works on most distros.
        GLIBC=$(ldd --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+$' | head -1)
        OS_DETAIL="Linux"
        if [ -n "$GLIBC" ]; then
            OS_DETAIL="Linux glibc ${GLIBC}"
            # 2.17 is the floor for Go 1.22+ amd64 binaries.
            major=${GLIBC%%.*}
            minor=${GLIBC##*.}
            if [ "$major" -lt 2 ] || ([ "$major" = 2 ] && [ "$minor" -lt 17 ]); then
                warn "glibc ${GLIBC} is older than 2.17. altcode may fail with"
                warn "'GLIBC_X.YZ not found'. Try: build from source with Go 1.22+,"
                warn "or upgrade to Ubuntu 14.04+ / CentOS 7+ / Debian 8+."
            fi
        fi
        # Kernel version (informational; Go binaries don't pin to it).
        KERN=$(uname -r 2>/dev/null)
        [ -n "$KERN" ] && OS_DETAIL="${OS_DETAIL} (kernel ${KERN})"
        ;;
    windows)
        OS_DETAIL="Windows"
        ;;
    *)
        warn "Unrecognised OS '$OS'. Pre-built binary may not work."
        ;;
esac

echo ""
echo -e "${BOLD}altcode installer${NC}"
echo -e "  Platform:  ${OS}/${ARCH}"
[ -n "$OS_DETAIL" ] && echo -e "  System:    ${OS_DETAIL}"
echo -e "  Version:   ${VERSION}"
echo -e "  Target:    ${INSTALL_DIR}/altcode"
echo ""

# Step 1: Download. Three ladders (in order):
#   1. https://altcode.io/bin/<binary> — fast, served by GitHub Pages
#      directly from this repo's docs/bin/ directory. No release-tag
#      lookup needed; always tracks main.
#   2. https://github.com/<repo>/releases/{latest,<version>}/download/...
#      — versioned binaries when the user passes ALTCODE_VERSION.
#   3. Source build via `go build` if Go is on PATH.
SITE_URL="https://altcode.io/bin/$BINARY_NAME"
if [ "$VERSION" = "latest" ]; then
    RELEASE_URL="https://github.com/$REPO/releases/latest/download/$BINARY_NAME"
else
    RELEASE_URL="https://github.com/$REPO/releases/download/${VERSION}/$BINARY_NAME"
fi

TMP=$(mktemp)
info "Downloading $BINARY_NAME ..."

if [ "$VERSION" = "latest" ] && curl -fsSL -o "$TMP" "$SITE_URL" 2>/dev/null; then
    chmod +x "$TMP"
    ok "Downloaded pre-built binary from altcode.io"
elif curl -fsSL -o "$TMP" "$RELEASE_URL" 2>/dev/null; then
    chmod +x "$TMP"
    ok "Downloaded pre-built binary from GitHub releases"
elif command -v go &>/dev/null; then
    warn "Pre-built binary not available, building from source..."
    rm -f "$TMP"
    BUILD_TMP=$(mktemp -d)
    git clone --depth 1 "https://github.com/$REPO.git" "$BUILD_TMP/altcode" 2>/dev/null
    cd "$BUILD_TMP/altcode"
    GOFLAGS=-mod=mod go build -ldflags="-s -w -X main.Version=$VERSION" -o "$TMP" ./cmd/altcode
    chmod +x "$TMP"
    cd - >/dev/null
    rm -rf "$BUILD_TMP"
    ok "Built from source"
else
    rm -f "$TMP"
    fail "No pre-built binary and Go not found. Install Go 1.23+: https://go.dev/dl/"
fi

# Step 2: Install (no sudo by default; auto-creates ~/.local/bin)
info "Installing to $INSTALL_DIR ..."
mkdir -p "$INSTALL_DIR" 2>/dev/null || true

if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP" "$INSTALL_DIR/altcode${EXT}"
elif [ "$INSTALL_DIR" = "$DEFAULT_INSTALL_DIR" ]; then
    # User explicitly didn't set ALTCODE_INSTALL_DIR but the default
    # path isn't writable for some reason (read-only home, weird
    # perms). Fall back to ~/.local/bin and recreate it. We never
    # call sudo on the default path — that would surprise users who
    # piped `curl | bash` into a non-interactive shell.
    fail "Cannot write to $INSTALL_DIR. Try ALTCODE_INSTALL_DIR=<other-path> $0"
else
    # User set ALTCODE_INSTALL_DIR=/usr/local/bin (or similar) but
    # doesn't have write perms there. Ask before sudo'ing — never
    # silently elevate.
    warn "$INSTALL_DIR isn't writable. Need sudo to install there?"
    if [ -t 0 ]; then
        # Interactive shell: prompt explicitly.
        read -p "Use sudo to install to $INSTALL_DIR? [y/N] " yn
        case "$yn" in
            [Yy]*) sudo mv "$TMP" "$INSTALL_DIR/altcode${EXT}" ;;
            *)     fail "Aborted. Re-run with ALTCODE_INSTALL_DIR=\$HOME/.local/bin (no sudo)." ;;
        esac
    else
        # Non-interactive (curl | bash): refuse to sudo silently.
        fail "$INSTALL_DIR not writable and no TTY for sudo prompt. Re-run with ALTCODE_INSTALL_DIR=\$HOME/.local/bin (no sudo)."
    fi
fi
ok "Installed to $INSTALL_DIR/altcode"

# Step 2.5: PATH check — ~/.local/bin isn't always on PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;  # already on PATH
    *)
        echo ""
        warn "$INSTALL_DIR is NOT on your \$PATH."
        echo -e "  Add this to your shell rc (${BOLD}~/.bashrc${NC}, ${BOLD}~/.zshrc${NC}, etc):"
        echo -e "    ${GREEN}export PATH=\"$INSTALL_DIR:\$PATH\"${NC}"
        echo -e "  Or run altcode directly: ${GREEN}$INSTALL_DIR/altcode${NC}"
        echo ""
        ;;
esac

# Step 3: Verify
info "Verifying installation ..."
INSTALLED_VERSION=$("$INSTALL_DIR/altcode${EXT}" --version 2>/dev/null || altcode --version 2>/dev/null)
ok "$INSTALLED_VERSION"

# Step 4: Detect existing credentials
echo ""
CREDS=""
if [ -f "$HOME/.claude/.credentials.json" ]; then
    CREDS="${CREDS}Claude subscription "
fi
if [ -f "$HOME/.codex/auth.json" ]; then
    CREDS="${CREDS}Codex subscription "
fi
if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
    CREDS="${CREDS}ANTHROPIC_API_KEY "
fi
if [ -n "${OPENAI_API_KEY:-}" ]; then
    CREDS="${CREDS}OPENAI_API_KEY "
fi

if [ -n "$CREDS" ]; then
    ok "Auto-detected: ${CREDS}"
    echo ""
    echo -e "${BOLD}Ready! Just run:${NC}"
    echo -e "  ${GREEN}altcode${NC}"
else
    echo ""
    echo -e "${BOLD}Set up an API key:${NC}"
    echo ""
    echo -e "  ${YELLOW}# Option 1: Anthropic${NC}"
    echo -e "  export ANTHROPIC_API_KEY=sk-ant-..."
    echo ""
    echo -e "  ${YELLOW}# Option 2: OpenAI${NC}"
    echo -e "  export OPENAI_API_KEY=sk-..."
    echo ""
    echo -e "  ${YELLOW}# Option 3: Local (Ollama)${NC}"
    echo -e "  altcode --model ollama/llama3"
    echo ""
    echo -e "  ${YELLOW}# Option 4: Already use Claude Code or Codex CLI?${NC}"
    echo -e "  Just run ${GREEN}altcode${NC} — credentials auto-detected"
fi

echo ""
echo -e "${BOLD}Uninstall:${NC} rm $(which altcode 2>/dev/null || echo "$INSTALL_DIR/altcode")"
echo ""
