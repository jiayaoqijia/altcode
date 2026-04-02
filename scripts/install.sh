#!/bin/bash
# altcode installer — downloads pre-built binary or builds from source
set -euo pipefail

VERSION="${ALTCODE_VERSION:-latest}"
INSTALL_DIR="${ALTCODE_INSTALL_DIR:-/usr/local/bin}"
REPO="jiayaoqijia/altcode"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

EXT=""
[ "$OS" = "windows" ] && EXT=".exe"

echo "altcode installer"
echo "  OS:      $OS"
echo "  Arch:    $ARCH"
echo "  Version: $VERSION"
echo ""

# Try downloading pre-built binary
if [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/altcode-${OS}-${ARCH}${EXT}"
else
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/${VERSION}/altcode-${OS}-${ARCH}${EXT}"
fi

echo "Downloading from $DOWNLOAD_URL ..."
TMP=$(mktemp)
if curl -fsSL -o "$TMP" "$DOWNLOAD_URL" 2>/dev/null; then
    chmod +x "$TMP"
    if [ -w "$INSTALL_DIR" ]; then
        mv "$TMP" "$INSTALL_DIR/altcode${EXT}"
    else
        sudo mv "$TMP" "$INSTALL_DIR/altcode${EXT}"
    fi
    echo "Installed altcode to $INSTALL_DIR/altcode${EXT}"
elif command -v go &>/dev/null; then
    echo "Pre-built binary not available. Building from source..."
    rm -f "$TMP"
    BUILD_TMP=$(mktemp -d)
    git clone --depth 1 "https://github.com/$REPO.git" "$BUILD_TMP/altcode" 2>/dev/null
    cd "$BUILD_TMP/altcode"
    GOFLAGS=-mod=mod go build -ldflags="-s -w -X main.Version=$VERSION" -o altcode ./cmd/altcode
    if [ -w "$INSTALL_DIR" ]; then
        mv altcode "$INSTALL_DIR/altcode${EXT}"
    else
        sudo mv altcode "$INSTALL_DIR/altcode${EXT}"
    fi
    rm -rf "$BUILD_TMP"
    echo "Built and installed altcode to $INSTALL_DIR/altcode${EXT}"
else
    rm -f "$TMP"
    echo "Error: No pre-built binary and Go not found."
    echo "Install Go 1.23+: https://go.dev/dl/"
    echo "Or build manually: git clone ... && make build"
    exit 1
fi

echo ""
echo "Verify: altcode --version"
altcode --version 2>/dev/null || "$INSTALL_DIR/altcode${EXT}" --version

echo ""
echo "Get started:"
echo "  # If you have Claude Code or Codex CLI installed, just run:"
echo "  altcode"
echo ""
echo "  # Or set an API key:"
echo "  export ANTHROPIC_API_KEY=sk-ant-..."
echo "  altcode"
echo ""
echo "  # Or use OpenAI:"
echo "  export OPENAI_API_KEY=sk-..."
echo "  altcode --model openai/gpt-4"
