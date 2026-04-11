package mcp

import (
	"context"
	"fmt"
	"os"
	"sort"
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
	// Errors accumulated during connect/register so callers (e.g.
	// /doctor) can surface them instead of having them corrupt the
	// TUI by leaking to stderr.
	Errors []string
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
			// Previously: fmt.Fprintf(os.Stderr, ...) which corrupted
			// the TUI display. Accumulate instead and let callers
			// decide how to surface it.
			m.Errors = append(m.Errors, fmt.Sprintf("mcp: failed to connect %s: %v", r.name, r.err))
			continue
		}
		m.stdioClients[r.name] = r.client
	}
	return m
}

// RegisterAll discovers and registers tools from all connected servers.
//
// Iterates server names in sorted order so the registration sequence
// is deterministic across runs. Without sorting, map iteration order
// could change which server "wins" a name collision between restarts,
// producing surprising tool routing differences for the same config.
func (m *Manager) RegisterAll(ctx context.Context, registry *tool.Registry) {
	stdioNames := make([]string, 0, len(m.stdioClients))
	for n := range m.stdioClients {
		stdioNames = append(stdioNames, n)
	}
	sort.Strings(stdioNames)
	for _, name := range stdioNames {
		client := m.stdioClients[name]
		if err := RegisterMCPTools(ctx, registry, client, name); err != nil {
			m.Errors = append(m.Errors, fmt.Sprintf("mcp: tool discovery failed for %s: %v", name, err))
		}
	}

	sseNames := make([]string, 0, len(m.sseClients))
	for n := range m.sseClients {
		sseNames = append(sseNames, n)
	}
	sort.Strings(sseNames)
	for _, name := range sseNames {
		client := m.sseClients[name]
		if err := RegisterSSETools(ctx, registry, client, name); err != nil {
			m.Errors = append(m.Errors, fmt.Sprintf("mcp: SSE tool discovery failed for %s: %v", name, err))
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
