#!/bin/bash
# AltFix VM Setup Script
# Usage: curl -fsSL https://altcode.io/setup-vm.sh | bash
#
# Installs altcode + codex + cc + dev tools on a fresh Ubuntu 24.04 VM.
# Creates 'altfix' user, configures systemd service, starts daemon.

set -euo pipefail

echo "=== AltFix VM Setup ==="
echo ""

# Check root
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: Run as root (sudo bash setup-vm.sh)"
    exit 1
fi

# System packages
echo "[1/8] Installing system packages..."
apt-get update -qq
apt-get install -y -qq --no-install-recommends \
    build-essential git curl wget unzip jq ripgrep tree \
    ca-certificates gnupg lsb-release sqlite3 tmux htop \
    python3 python3-pip python3-venv universal-ctags \
    > /dev/null

# Node.js 22
echo "[2/8] Installing Node.js 22..."
curl -fsSL https://deb.nodesource.com/setup_22.x | bash - > /dev/null 2>&1
apt-get install -y -qq nodejs > /dev/null
npm install -g pnpm > /dev/null 2>&1

# Go
echo "[3/8] Installing Go 1.24..."
curl -fsSL https://go.dev/dl/go1.24.2.linux-amd64.tar.gz | tar -C /usr/local -xz
echo 'export PATH="/usr/local/go/bin:$PATH"' >> /etc/profile.d/go.sh

# GitHub CLI
echo "[4/8] Installing GitHub CLI..."
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg 2>/dev/null
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null
apt-get update -qq && apt-get install -y -qq gh > /dev/null

# Python tools
echo "[5/8] Installing Python tools..."
pip3 install --break-system-packages -q uv ruff

# Agent CLIs
echo "[6/8] Installing agent CLIs..."
npm install -g @openai/codex > /dev/null 2>&1 || echo "  codex install skipped"
npm install -g @anthropic-ai/claude-code > /dev/null 2>&1 || echo "  claude-code install skipped"

# altcode
echo "[7/8] Installing altcode..."
if [ -f /tmp/altcode ]; then
    cp /tmp/altcode /usr/local/bin/altcode
elif command -v go > /dev/null; then
    go install github.com/altcode-ai/altcode/cmd/altcode@latest
    cp "$(go env GOPATH)/bin/altcode" /usr/local/bin/altcode
else
    curl -fsSL https://altcode.io/install.sh | bash
fi
chmod +x /usr/local/bin/altcode

# Create altfix user + directories
echo "[8/8] Configuring altfix service..."
id altfix > /dev/null 2>&1 || useradd -m -s /bin/bash altfix
mkdir -p /home/altfix/{repos,workspaces,.altcode/daemon}
chown -R altfix:altfix /home/altfix

# Install systemd service
cat > /etc/systemd/system/altfix.service << 'SVCEOF'
[Unit]
Description=AltFix Coding Agent Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=altfix
Group=altfix
WorkingDirectory=/home/altfix
ExecStart=/usr/local/bin/altcode daemon --port 9100 --data-dir /home/altfix/.altcode/daemon --max-concurrent 2
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=altfix
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/home/altfix/.altcode /home/altfix/repos /home/altfix/workspaces /tmp
LimitNOFILE=65536
MemoryMax=4G
EnvironmentFile=-/home/altfix/.altcode/env

[Install]
WantedBy=multi-user.target
SVCEOF

systemctl daemon-reload
systemctl enable altfix

echo ""
echo "=== AltFix VM Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. Add API keys to /home/altfix/.altcode/env:"
echo "     ALTLLM=sk-your-key"
echo "     ANTHROPIC_API_KEY=sk-ant-..."
echo "     OPENAI_API_KEY=sk-..."
echo ""
echo "  2. Add auth token:"
echo "     echo 'ALTFIX_AUTH_TOKEN=your-token' >> /home/altfix/.altcode/env"
echo ""
echo "  3. Start the daemon:"
echo "     sudo systemctl start altfix"
echo "     curl http://localhost:9100/health"
echo ""
echo "  4. Verify tools:"
echo "     altcode --help && codex --version && gh --version"
