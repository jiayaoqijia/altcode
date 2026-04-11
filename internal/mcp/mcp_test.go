package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/mcp"
	"github.com/altcode-ai/altcode/internal/tool"
)

// mockMCPServer is a simple script that responds to JSON-RPC over stdio.
const mockMCPScript = `#!/usr/bin/env python3
import json, sys

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    method = req.get("method", "")
    rid = req.get("id", 0)

    if method == "tools/list":
        resp = {"jsonrpc": "2.0", "id": rid, "result": {
            "tools": [
                {"name": "echo", "description": "Echo input", "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}}},
                {"name": "add", "description": "Add numbers", "inputSchema": {"type": "object", "properties": {"a": {"type": "integer"}, "b": {"type": "integer"}}}}
            ]
        }}
    elif method == "tools/call":
        params = req.get("params", {})
        name = params.get("name", "")
        args = params.get("arguments", {})
        if isinstance(args, str):
            args = json.loads(args)
        if name == "echo":
            text = args.get("text", "")
            resp = {"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": text}]}}
        elif name == "add":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {"content": [{"type": "text", "text": str(args.get("a", 0) + args.get("b", 0))}]}}
        else:
            resp = {"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "unknown tool"}}
    else:
        resp = {"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "unknown method"}}

    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`

func setupMockServer(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "mcp_server.py")
	os.WriteFile(script, []byte(mockMCPScript), 0o755)

	return script, func() {}
}

func TestMCP_Connect(t *testing.T) {
	script, cleanup := setupMockServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()
}

func TestMCP_DiscoverTools(t *testing.T) {
	script, cleanup := setupMockServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.DiscoverTools(ctx)
	if err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("First tool: %q", tools[0].Name)
	}
	if tools[1].Name != "add" {
		t.Errorf("Second tool: %q", tools[1].Name)
	}
}

func TestMCP_CallTool(t *testing.T) {
	script, cleanup := setupMockServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	result, err := client.CallTool(ctx, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "hello" {
		t.Errorf("Expected 'hello', got %q", result)
	}
}

func TestMCP_CallToolAdd(t *testing.T) {
	script, cleanup := setupMockServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	result, err := client.CallTool(ctx, "add", json.RawMessage(`{"a":3,"b":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "7" {
		t.Errorf("Expected '7', got %q", result)
	}
}

func TestMCP_RegisterTools(t *testing.T) {
	script, cleanup := setupMockServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	registry := tool.NewRegistry()
	err = mcp.RegisterMCPTools(ctx, registry, client, "test-server")
	if err != nil {
		t.Fatalf("RegisterMCPTools: %v", err)
	}

	// Tools should be registered with prefix
	echoTool, ok := registry.Get("mcp__test-server__echo")
	if !ok {
		t.Fatal("Echo tool not registered")
	}
	if echoTool.Description() != "Echo input" {
		t.Errorf("Description: %q", echoTool.Description())
	}

	// Execute through the registry
	result, err := echoTool.Execute(ctx, json.RawMessage(`{"text":"from registry"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output != "from registry" {
		t.Errorf("Output: %q", result.Output)
	}
}

// TestMCP_ToolDefaultsAreSafe verifies the conservative default for
// MCP tools that don't carry annotations: NOT read-only and NOT
// concurrency-safe. The previous default ("everything is read-only")
// let plan-mode users write files through MCP filesystem servers
// unchecked and serialized parallel batches incorrectly.
func TestMCP_ToolDefaultsAreSafe(t *testing.T) {
	script, cleanup := setupMockServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, _ := mcp.Connect(ctx, "python3", []string{script}, nil)
	defer client.Close()

	registry := tool.NewRegistry()
	mcp.RegisterMCPTools(ctx, registry, client, "srv")

	echoTool, _ := registry.Get("mcp__srv__echo")
	if echoTool.IsReadOnly() {
		t.Error("MCP tool without annotations should default to mutating")
	}
	if echoTool.IsConcurrencySafe() {
		t.Error("MCP tool without annotations should default to sequential")
	}
}

func TestMCP_ConnectBadCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := mcp.Connect(ctx, "nonexistent_binary_xyz", nil, nil)
	if err == nil {
		t.Error("Expected error for bad command")
	}
}
