# AltFix VM Image — pre-installed altcode + codex + cc + dev tools
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive
ENV HOME=/root

# System essentials
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential gcc g++ make cmake \
    git curl wget unzip jq ripgrep tree \
    ca-certificates gnupg lsb-release \
    sqlite3 libsqlite3-dev \
    tmux screen htop \
    python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Node.js 22 (for codex CLI)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs \
    && npm install -g pnpm \
    && rm -rf /var/lib/apt/lists/*

# Go 1.24
RUN curl -fsSL https://go.dev/dl/go1.24.2.linux-amd64.tar.gz | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/root/go"
ENV PATH="${GOPATH}/bin:${PATH}"

# Rust (for clippy + cargo)
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Java 21 (JDK)
RUN apt-get update && apt-get install -y --no-install-recommends openjdk-21-jdk-headless \
    && rm -rf /var/lib/apt/lists/*

# GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Python tools
RUN pip3 install --break-system-packages uv ruff

# Go linters
RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# universal-ctags
RUN apt-get update && apt-get install -y --no-install-recommends universal-ctags \
    && rm -rf /var/lib/apt/lists/*

# Codex CLI
RUN npm install -g @openai/codex

# Claude Code CLI (npm package)
RUN npm install -g @anthropic-ai/claude-code || echo "Claude Code install skipped (may require auth)"

# altcode binary
COPY dist/altcode /usr/local/bin/altcode
RUN chmod +x /usr/local/bin/altcode

# AltFix directory structure
RUN mkdir -p /root/repos /root/workspaces /root/.altcode/daemon

# Systemd service file (for bare-metal VMs)
COPY deployments/altfix.service /etc/systemd/system/altfix.service

# Health check
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s \
    CMD curl -sf http://localhost:9200/health || exit 1

# Default: start daemon
EXPOSE 9200
CMD ["altcode", "daemon", "--port", "9200", "--data-dir", "/root/.altcode/daemon"]
