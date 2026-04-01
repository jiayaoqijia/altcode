#!/bin/bash
# altcode installer — downloads and installs the latest release
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

echo "altcode installer"
echo "  OS:      $OS"
echo "  Arch:    $ARCH"
echo "  Version: $VERSION"
echo "  Target:  $INSTALL_DIR/altcode"
echo ""

# For now, build from source (binary releases coming soon)
if command -v go &>/dev/null; then
    echo "Go found. Building from source..."
    TMP=$(mktemp -d)
    git clone --depth 1 "https://github.com/$REPO.git" "$TMP/altcode" 2>/dev/null
    cd "$TMP/altcode"
    GOFLAGS=-mod=mod go build -ldflags="-s -w -X main.Version=$VERSION" -o altcode ./cmd/altcode

    if [ -w "$INSTALL_DIR" ]; then
        mv altcode "$INSTALL_DIR/altcode"
    else
        sudo mv altcode "$INSTALL_DIR/altcode"
    fi

    rm -rf "$TMP"
    echo ""
    echo "Installed altcode to $INSTALL_DIR/altcode"
else
    echo "Go not found. Install Go 1.23+ first: https://go.dev/dl/"
    echo "Or build manually:"
    echo ""
    echo "  git clone https://github.com/$REPO.git"
    echo "  cd altcode"
    echo "  make build"
    echo "  sudo cp dist/altcode /usr/local/bin/"
    exit 1
fi

echo ""
echo "Get started:"
echo "  export ANTHROPIC_API_KEY=sk-ant-..."
echo "  altcode"
echo ""
echo "Or with OpenAI/Codex:"
echo "  export OPENAI_API_KEY=sk-..."
echo "  altcode --model openai/gpt-4"
