#!/bin/bash
# altcode installer
# Usage: curl -fsSL https://raw.githubusercontent.com/jiayaoqijia/altcode/main/scripts/install.sh | bash
set -euo pipefail

VERSION="${ALTCODE_VERSION:-latest}"
INSTALL_DIR="${ALTCODE_INSTALL_DIR:-/usr/local/bin}"
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

echo ""
echo -e "${BOLD}altcode installer${NC}"
echo -e "  Platform:  ${OS}/${ARCH}"
echo -e "  Version:   ${VERSION}"
echo -e "  Target:    ${INSTALL_DIR}/altcode"
echo ""

# Step 1: Download
if [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/$BINARY_NAME"
else
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/${VERSION}/$BINARY_NAME"
fi

TMP=$(mktemp)
info "Downloading $BINARY_NAME ..."

if curl -fsSL -o "$TMP" "$DOWNLOAD_URL" 2>/dev/null; then
    chmod +x "$TMP"
    ok "Downloaded pre-built binary"
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

# Step 2: Install
info "Installing to $INSTALL_DIR ..."
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP" "$INSTALL_DIR/altcode${EXT}"
else
    sudo mv "$TMP" "$INSTALL_DIR/altcode${EXT}"
fi
ok "Installed to $INSTALL_DIR/altcode"

# Step 3: Verify
info "Verifying installation ..."
INSTALLED_VERSION=$(altcode --version 2>/dev/null || "$INSTALL_DIR/altcode${EXT}" --version 2>/dev/null)
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
