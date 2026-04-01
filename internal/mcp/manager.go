package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/tool"
)

// Manager handles multiple MCP server connections.
type Manager struct {
	clients map[string]*Client
}

// NewManager creates a Manager and connects to all configured MCP servers.
func NewManager(ctx context.Context, servers map[string]config.MCPServerConfig) *Manager {
	m := &Manager{clients: make(map[string]*Client)}

	for name, cfg := range servers {
		client, err := connectServer(ctx, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: failed to connect %s: %v\n", name, err)
			continue
		}
		m.clients[name] = client
	}
	return m
}

// RegisterAll discovers and registers tools from all connected servers.
func (m *Manager) RegisterAll(ctx context.Context, registry *tool.Registry) {
	for name, client := range m.clients {
		if err := RegisterMCPTools(ctx, registry, client, name); err != nil {
			fmt.Fprintf(os.Stderr, "mcp: tool discovery failed for %s: %v\n", name, err)
		}
	}
}

// Close terminates all MCP server connections.
func (m *Manager) Close() {
	for _, client := range m.clients {
		client.Close()
	}
}

func connectServer(ctx context.Context, cfg config.MCPServerConfig) (*Client, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp server has no command")
	}

	// Build environment with variable expansion
	var env []string
	for k, v := range cfg.Env {
		expanded := os.ExpandEnv(v)
		env = append(env, k+"="+expanded)
	}

	args := cfg.Args
	command := cfg.Command

	// Handle "npx" style commands
	if strings.Contains(command, " ") {
		parts := strings.Fields(command)
		command = parts[0]
		args = append(parts[1:], args...)
	}

	return Connect(ctx, command, args, env)
}
