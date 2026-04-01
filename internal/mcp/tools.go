package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/altcode-ai/altcode/internal/tool"
)

// ToolInfo is a tool discovered from an MCP server.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// DiscoverTools calls tools/list on the MCP server.
func (c *Client) DiscoverTools(ctx context.Context) ([]ToolInfo, error) {
	result, err := c.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}

	var resp struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp parse tools: %w", err)
	}
	return resp.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	params := map[string]any{
		"name":      name,
		"arguments": json.RawMessage(args),
	}
	result, err := c.Call(ctx, "tools/call", params)
	if err != nil {
		return "", fmt.Errorf("mcp tools/call %s: %w", name, err)
	}

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}
	if len(resp.Content) > 0 {
		return resp.Content[0].Text, nil
	}
	return string(result), nil
}

// mcpTool wraps an MCP server tool as a tool.Tool implementation.
type mcpTool struct {
	info   ToolInfo
	client *Client
	prefix string // "mcp__servername__"
}

// RegisterMCPTools discovers tools from a client and registers them.
func RegisterMCPTools(ctx context.Context, registry *tool.Registry, client *Client, serverName string) error {
	tools, err := client.DiscoverTools(ctx)
	if err != nil {
		return err
	}
	for _, t := range tools {
		mt := &mcpTool{
			info:   t,
			client: client,
			prefix: "mcp__" + serverName + "__",
		}
		registry.Register(mt)
	}
	return nil
}

func (t *mcpTool) Name() string             { return t.prefix + t.info.Name }
func (t *mcpTool) Description() string       { return t.info.Description }
func (t *mcpTool) Parameters() json.RawMessage { return t.info.InputSchema }
func (t *mcpTool) IsConcurrencySafe() bool   { return true }
func (t *mcpTool) IsReadOnly() bool          { return true }
func (t *mcpTool) PermissionPattern(_ json.RawMessage) string {
	return t.prefix + t.info.Name + ":*"
}

// RegisterSSETools discovers tools from an SSE client and registers them.
func RegisterSSETools(ctx context.Context, registry *tool.Registry, client *SSEClient, serverName string) error {
	tools, err := client.DiscoverTools(ctx)
	if err != nil {
		return err
	}
	for _, ti := range tools {
		st := &sseMCPTool{
			info:   ti,
			client: client,
			prefix: "mcp__" + serverName + "__",
		}
		registry.Register(st)
	}
	return nil
}

type sseMCPTool struct {
	info   ToolInfo
	client *SSEClient
	prefix string
}

func (t *sseMCPTool) Name() string             { return t.prefix + t.info.Name }
func (t *sseMCPTool) Description() string       { return t.info.Description }
func (t *sseMCPTool) Parameters() json.RawMessage { return t.info.InputSchema }
func (t *sseMCPTool) IsConcurrencySafe() bool   { return true }
func (t *sseMCPTool) IsReadOnly() bool          { return true }
func (t *sseMCPTool) PermissionPattern(_ json.RawMessage) string {
	return t.prefix + t.info.Name + ":*"
}

func (t *sseMCPTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	output, err := t.client.CallTool(ctx, t.info.Name, input)
	if err != nil {
		return &tool.Result{Output: fmt.Sprintf("Error: %v", err), Title: t.info.Name, Error: err}, nil
	}
	return &tool.Result{Output: output, Title: t.info.Name}, nil
}

func (t *mcpTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	output, err := t.client.CallTool(ctx, t.info.Name, input)
	if err != nil {
		return &tool.Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  t.info.Name,
			Error:  err,
		}, nil
	}
	return &tool.Result{
		Output: output,
		Title:  t.info.Name,
	}, nil
}
