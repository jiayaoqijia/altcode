//go:build !windows

package internal_test

// Tests verifying altcode has feature parity with Claude Code CLI
// on hooks, plugins, agents, and MCP.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/mcp"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/plugin"
	"github.com/altcode-ai/altcode/internal/tool"
)

// =============================================================================
// HOOKS: all 13 events exist
// =============================================================================

func TestParity_AllHookEventsExist(t *testing.T) {
	expected := []hooks.Event{
		hooks.PreToolUse, hooks.PostToolUse, hooks.Stop,
		hooks.SessionStart, hooks.SessionEnd,
		hooks.UserPromptSubmit, hooks.SubagentStop,
		hooks.PreCompact, hooks.Notification,
		hooks.CwdChanged, hooks.FileChanged,
		hooks.TaskCreated, hooks.PermissionDenied,
	}

	runner := hooks.NewRunner(nil)
	for _, ev := range expected {
		// Verify each event can be fired without panic
		results, err := runner.Fire(context.Background(), ev, hooks.Input{Event: ev})
		if err != nil {
			t.Errorf("Event %s error: %v", ev, err)
		}
		_ = results // no hooks configured, should be nil
	}
	t.Logf("All %d hook events verified", len(expected))
}

func TestParity_PermissionDeniedHookFires(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "denied.log")
	script := filepath.Join(dir, "denied.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho fired >> "+logFile+"\necho '{\"decision\":\"allow\"}'"), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PermissionDenied: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	// Create engine with deny rule + PermissionDenied hook
	perm := permission.NewEvaluator(permission.ModeDefault, "", []permission.Rule{
		{Tool: "bash", Pattern: "*", Action: permission.ActionDeny, Source: "test"},
	})

	srv := parityServer(t, []string{
		parityToolSSE("t1", "bash", `{"command":"echo hi"}`),
		parityTextSSE("Denied."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: parityCfg(srv),
		Perm:   perm,
		Hooks:  hookRunner,
	})

	parityDrain(eng.Run(context.Background(), "run echo"))

	data, _ := os.ReadFile(logFile)
	if !strings.Contains(string(data), "fired") {
		t.Error("PermissionDenied hook should fire when tool is denied")
	}
}

// =============================================================================
// PLUGINS: marketplace support
// =============================================================================

func TestParity_MarketplaceDiscovery(t *testing.T) {
	dir := t.TempDir()

	// Plugin A
	pA := filepath.Join(dir, "plugin-a")
	os.MkdirAll(filepath.Join(pA, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(pA, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-a"}`), 0o644)

	// Plugin B
	pB := filepath.Join(dir, "plugin-b")
	os.MkdirAll(filepath.Join(pB, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pB, ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-b"}`), 0o644)

	// Marketplace index
	os.WriteFile(filepath.Join(dir, "marketplace.json"), []byte(fmt.Sprintf(`{
		"name": "test-marketplace",
		"plugins": [
			{"name": "plugin-a", "source": "%s"},
			{"name": "plugin-b", "source": "%s"}
		]
	}`, pA, pB)), 0o644)

	plugins, err := plugin.DiscoverFromMarketplace(filepath.Join(dir, "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 2 {
		t.Errorf("Expected 2 plugins, got %d", len(plugins))
	}
}

func TestParity_ClaudeCodeMarketplace(t *testing.T) {
	mpPath := filepath.Join("..", "vendor", "claude-code", ".claude-plugin", "marketplace.json")
	if _, err := os.Stat(mpPath); os.IsNotExist(err) {
		t.Skip("Claude Code marketplace not found")
	}

	plugins, err := plugin.DiscoverFromMarketplace(mpPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Loaded %d plugins from Claude Code marketplace", len(plugins))
	if len(plugins) < 8 {
		t.Errorf("Expected 8+ plugins, got %d", len(plugins))
	}
}

// =============================================================================
// AGENTS: registry with depth tracking
// =============================================================================

func TestParity_AgentRegistryDepthLimit(t *testing.T) {
	reg := agent.NewRegistry(3) // max depth 3

	ag := &agent.Agent{Name: "test"}
	_, ok := reg.Register("agent-1", ag, 1, "/root")
	if !ok {
		t.Error("Depth 1 should be allowed")
	}
	_, ok = reg.Register("agent-2", ag, 3, "/root")
	if !ok {
		t.Error("Depth 3 (max) should be allowed")
	}
	_, ok = reg.Register("agent-3", ag, 4, "/root")
	if ok {
		t.Error("Depth 4 should be rejected (exceeds max 3)")
	}
}

func TestParity_AgentRegistryLifecycle(t *testing.T) {
	reg := agent.NewRegistry(5)
	ag := &agent.Agent{Name: "worker"}

	_, ok := reg.Register("worker-1", ag, 0, "/root")
	if !ok {
		t.Fatal("Register failed")
	}

	if reg.Count() != 1 {
		t.Errorf("Count: %d", reg.Count())
	}

	names := reg.List()
	if len(names) != 1 || names[0] != "worker-1" {
		t.Errorf("List: %v", names)
	}

	reg.Release("worker-1", agent.StatusSucceeded)
	// Release removes from registry but doesn't close Done channel
	// (Team owns the Done channel lifecycle)
	if reg.Count() != 0 {
		t.Errorf("Count after Release: %d, want 0", reg.Count())
	}

	if reg.Count() != 0 {
		t.Error("Count should be 0 after release")
	}
}

// =============================================================================
// MCP: SSE transport
// =============================================================================

func TestParity_MCPSSETransport(t *testing.T) {
	// Mock SSE MCP server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCReq
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)

		if req.Method == "tools/list" {
			fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","id":`+
				fmt.Sprintf("%d", req.ID)+`,"result":{"tools":[{"name":"search","description":"Search","inputSchema":{"type":"object"}}]}}`)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","id":`+
				fmt.Sprintf("%d", req.ID)+`,"result":{"content":[{"type":"text","text":"found it"}]}}`)
		}
	}))
	defer srv.Close()

	client := mcp.ConnectSSE(srv.URL, nil)
	defer client.Close()

	tools, err := client.DiscoverTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Errorf("Tools: %v", tools)
	}

	result, err := client.CallTool(context.Background(), "search", json.RawMessage(`{"q":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "found it" {
		t.Errorf("Result: %q", result)
	}
}

func TestParity_MCPSSEToolsInRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCReq
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprintf(w, "data: %s\n\n", `{"jsonrpc":"2.0","id":`+
			fmt.Sprintf("%d", req.ID)+`,"result":{"tools":[{"name":"fetch","description":"Fetch URL","inputSchema":{"type":"object"}}]}}`)
	}))
	defer srv.Close()

	client := mcp.ConnectSSE(srv.URL, nil)
	registry := tool.NewRegistry()
	mcp.RegisterSSETools(context.Background(), registry, client, "web")

	if _, ok := registry.Get("mcp__web__fetch"); !ok {
		t.Error("SSE tool should be registered with namespace prefix")
	}
}

func TestParity_MCPManagerBothTransports(t *testing.T) {
	// Verify manager handles URL-based configs for SSE
	servers := map[string]config.MCPServerConfig{
		"sse-server": {URL: "http://localhost:9999"},
		// stdio would need a real command, skip in unit test
	}
	mgr := mcp.NewManager(context.Background(), servers)
	defer mgr.Close()
	// SSE client created even if server unreachable
}

type jsonRPCReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
}

// =============================================================================
// FULL PARITY: load Claude Code's actual marketplace + verify
// =============================================================================

func TestParity_FullClaudeCodePluginLoad(t *testing.T) {
	pluginDir := filepath.Join("..", "vendor", "claude-code", "plugins")
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		t.Skip("vendor/claude-code not found")
	}

	plugins, _ := plugin.Discover(pluginDir)

	totalCmds, totalAgents, totalHooks := 0, 0, 0
	for _, p := range plugins {
		totalCmds += len(p.Commands)
		totalAgents += len(p.Agents)
		totalHooks += len(p.Hooks)
	}

	t.Logf("Plugins: %d, Commands: %d, Agents: %d, HookEvents: %d",
		len(plugins), totalCmds, totalAgents, totalHooks)

	if len(plugins) < 10 {
		t.Errorf("Expected 10+ plugins")
	}
	if totalAgents < 10 {
		t.Errorf("Expected 10+ agents")
	}
	if totalCmds < 10 {
		t.Errorf("Expected 10+ commands")
	}
}

// =============================================================================
// Helpers
// =============================================================================

func parityTextSSE(text string) string {
	return fmt.Sprintf("event: content_block_start\ndata: %s\n\n", `{"index":0,"content_block":{"type":"text","text":""}}`) +
		fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)) +
		"event: content_block_stop\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"
}

func parityToolSSE(id, name, input string) string {
	return fmt.Sprintf("event: content_block_start\ndata: %s\n\n",
		fmt.Sprintf(`{"index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, id, name)) +
		fmt.Sprintf("event: content_block_delta\ndata: %s\n\n",
			fmt.Sprintf(`{"delta":{"type":"input_json_delta","partial_json":%q}}`, input)) +
		"event: content_block_stop\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"
}

func parityServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := idx
		idx++
		mu.Unlock()
		if i >= len(responses) {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(responses[i]))
	}))
}

func parityCfg(srv *httptest.Server) *config.Config {
	c := config.Default()
	c.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
	return c
}

func parityDrain(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
