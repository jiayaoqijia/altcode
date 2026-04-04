package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/tool"
)

// ToolProvider is the common interface for MCP tool sources (stdio or SSE).
type ToolProvider interface {
	DiscoverTools(ctx context.Context) ([]ToolInfo, error)
	CallTool(ctx context.Context, name string, args []byte) (string, error)
	Close() error
}

// Manager handles multiple MCP server connections (stdio and SSE).
type Manager struct {
	stdioClients map[string]*Client
	sseClients   map[string]*SSEClient
}

// NewManager connects to all configured MCP servers in parallel.
// Servers that fail to connect are logged and skipped.
func NewManager(ctx context.Context, servers map[string]config.MCPServerConfig) *Manager {
	m := &Manager{
		stdioClients: make(map[string]*Client),
		sseClients:   make(map[string]*SSEClient),
	}

	type stdioResult struct {
		name   string
		client *Client
		err    error
	}
	ch := make(chan stdioResult, len(servers))
	stdioCount := 0

	for name, cfg := range servers {
		if cfg.URL != "" {
			headers := make(map[string]string)
			for k, v := range cfg.Env {
				headers[k] = os.ExpandEnv(v)
			}
			m.sseClients[name] = ConnectSSE(cfg.URL, headers)
		} else if cfg.Command != "" {
			stdioCount++
			go func(n string, c config.MCPServerConfig) {
				client, err := connectStdio(ctx, c)
				ch <- stdioResult{name: n, client: client, err: err}
			}(name, cfg)
		}
	}

	for i := 0; i < stdioCount; i++ {
		r := <-ch
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "mcp: failed to connect %s: %v\n", r.name, r.err)
			continue
		}
		m.stdioClients[r.name] = r.client
	}
	return m
}

// RegisterAll discovers and registers tools from all connected servers.
func (m *Manager) RegisterAll(ctx context.Context, registry *tool.Registry) {
	for name, client := range m.stdioClients {
		if err := RegisterMCPTools(ctx, registry, client, name); err != nil {
			fmt.Fprintf(os.Stderr, "mcp: tool discovery failed for %s: %v\n", name, err)
		}
	}
	for name, client := range m.sseClients {
		if err := RegisterSSETools(ctx, registry, client, name); err != nil {
			fmt.Fprintf(os.Stderr, "mcp: SSE tool discovery failed for %s: %v\n", name, err)
		}
	}
}

// Close terminates all MCP server connections.
func (m *Manager) Close() {
	for _, c := range m.stdioClients {
		c.Close()
	}
	for _, c := range m.sseClients {
		c.Close()
	}
}

func connectStdio(ctx context.Context, cfg config.MCPServerConfig) (*Client, error) {
	var env []string
	for k, v := range cfg.Env {
		env = append(env, k+"="+os.ExpandEnv(v))
	}

	args := cfg.Args
	command := cfg.Command
	if strings.Contains(command, " ") {
		parts := strings.Fields(command)
		command = parts[0]
		args = append(parts[1:], args...)
	}

	return Connect(ctx, command, args, env)
}
