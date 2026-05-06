package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/mcp"
)

const mockResourceScript = `#!/usr/bin/env python3
import json, sys

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    method = req.get("method", "")
    rid = req.get("id", 0)

    if method == "resources/list":
        resp = {"jsonrpc": "2.0", "id": rid, "result": {
            "resources": [
                {"uri": "file:///README.md", "name": "README", "description": "Project readme", "mimeType": "text/markdown"},
                {"uri": "config://app", "name": "App Config", "description": "Application config"}
            ]
        }}
    elif method == "resources/read":
        params = req.get("params", {})
        uri = params.get("uri", "")
        if uri == "file:///README.md":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {
                "contents": [{"uri": uri, "mimeType": "text/markdown", "text": "# Hello World"}]
            }}
        elif uri == "config://app":
            resp = {"jsonrpc": "2.0", "id": rid, "result": {
                "contents": [{"uri": uri, "text": "{\"debug\": true}"}]
            }}
        else:
            resp = {"jsonrpc": "2.0", "id": rid, "error": {"code": -32602, "message": "resource not found"}}
    elif method == "tools/list":
        resp = {"jsonrpc": "2.0", "id": rid, "result": {"tools": []}}
    else:
        resp = {"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "unknown method"}}

    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`

func setupResourceServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "resource_server.py")
	os.WriteFile(script, []byte(mockResourceScript), 0o755)
	return script
}

func TestMCP_DiscoverResources(t *testing.T) {
	script := setupResourceServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	resources, err := client.DiscoverResources(ctx)
	if err != nil {
		t.Fatalf("DiscoverResources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0].URI != "file:///README.md" {
		t.Errorf("first URI: %q", resources[0].URI)
	}
	if resources[0].Name != "README" {
		t.Errorf("first Name: %q", resources[0].Name)
	}
	if resources[0].MimeType != "text/markdown" {
		t.Errorf("first MimeType: %q", resources[0].MimeType)
	}
	if resources[1].URI != "config://app" {
		t.Errorf("second URI: %q", resources[1].URI)
	}
}

func TestMCP_ReadResource(t *testing.T) {
	script := setupResourceServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	content, err := client.ReadResource(ctx, "file:///README.md")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if content != "# Hello World" {
		t.Errorf("expected '# Hello World', got %q", content)
	}
}

func TestMCP_ReadResourceConfig(t *testing.T) {
	script := setupResourceServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	content, err := client.ReadResource(ctx, "config://app")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if content != `{"debug": true}` {
		t.Errorf("expected JSON config, got %q", content)
	}
}

func TestMCP_ReadResourceNotFound(t *testing.T) {
	script := setupResourceServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, err = client.ReadResource(ctx, "file:///nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent resource")
	}
}

func TestMCP_DiscoverResourcesEmpty(t *testing.T) {
	// Use the original mock server that has no resource handlers
	script, cleanup := setupMockServer(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// The original mock returns an error for unknown methods
	_, err = client.DiscoverResources(ctx)
	if err == nil {
		t.Error("expected error from server without resource support")
	}
}
