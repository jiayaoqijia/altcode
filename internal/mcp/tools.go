package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/tool"
)

// ToolInfo is a tool discovered from an MCP server.
//
// Annotations come from the MCP spec's optional ToolAnnotations:
//   - readOnlyHint:    tool does not modify state (default: false)
//   - destructiveHint: tool may perform destructive updates (default: true)
//
// Without parsing these the engine would treat every MCP tool as both
// read-only and concurrency-safe — letting plan-mode users write files
// through an MCP filesystem server unchecked, and serializing parallel
// batches incorrectly.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations *struct {
		Title           string `json:"title,omitempty"`
		ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
		DestructiveHint *bool  `json:"destructiveHint,omitempty"`
		IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	} `json:"annotations,omitempty"`
}

// IsReadOnly reports whether the MCP server marked the tool readOnly.
// Default false (assume mutating) — safer to over-restrict than to
// silently let a write-capable tool through plan-mode permission checks.
func (t ToolInfo) IsReadOnly() bool {
	if t.Annotations == nil || t.Annotations.ReadOnlyHint == nil {
		return false
	}
	return *t.Annotations.ReadOnlyHint
}

// IsConcurrencySafe reports whether the tool is safe to run in parallel
// with other tools. We approximate this as "read-only AND not
// destructive" because the spec doesn't have a dedicated concurrency
// flag — write-capable tools should run sequentially to keep ordering
// deterministic.
func (t ToolInfo) IsConcurrencySafe() bool {
	if !t.IsReadOnly() {
		return false
	}
	if t.Annotations != nil && t.Annotations.DestructiveHint != nil &&
		*t.Annotations.DestructiveHint {
		return false
	}
	return true
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

	// Concatenate ALL content blocks the server returned, not just
	// the first text one. The MCP spec allows multiple content
	// blocks of mixed types in a single result; the previous
	// `resp.Content[0].Text` accessor silently dropped subsequent
	// text, image, and resource blocks. Codex round-R adversarial
	// finding (parity with claude-code 2.1.128 fix for the same
	// class of MCP content drop). For non-text blocks we render a
	// stable placeholder so the caller can detect that media was
	// returned even if it doesn't yet have a renderer.
	var resp struct {
		Content          []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text,omitempty"`
			Data     string          `json:"data,omitempty"`
			MimeType string          `json:"mimeType,omitempty"`
			Resource json.RawMessage `json:"resource,omitempty"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
		IsError           bool            `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}
	var sb strings.Builder
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(c.Text)
		case "image":
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			fmt.Fprintf(&sb, "[image: %s, %d bytes]",
				c.MimeType, len(c.Data))
		case "resource":
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			fmt.Fprintf(&sb, "[resource: %s]", string(c.Resource))
		default:
			// Unknown block — surface its raw text/data if any,
			// rather than silently dropping it.
			if c.Text != "" {
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(c.Text)
			}
		}
	}
	// If there's structured content but no text/image/resource block,
	// surface the JSON as-is so callers can parse it themselves.
	if sb.Len() == 0 && len(resp.StructuredContent) > 0 {
		sb.WriteString(string(resp.StructuredContent))
	}
	if sb.Len() == 0 {
		return string(result), nil
	}
	out := sb.String()
	if resp.IsError {
		return "", fmt.Errorf("mcp tool error: %s", out)
	}
	return out, nil
}

// mcpTool wraps an MCP server tool as a tool.Tool implementation.
type mcpTool struct {
	info   ToolInfo
	client *Client
	prefix string // "mcp__servername__"
}

// RegisterMCPTools discovers tools from a client and registers them.
// Skips registration if the tool name is already taken (e.g. another
// MCP server already claimed it, or the same server returned the
// duplicate). Without this check, the second registration silently
// overwrote the first and the routing was non-deterministic.
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
		if _, exists := registry.Get(mt.Name()); exists {
			continue
		}
		registry.Register(mt)
	}
	return nil
}

func (t *mcpTool) Name() string             { return t.prefix + t.info.Name }
func (t *mcpTool) Description() string       { return t.info.Description }
func (t *mcpTool) Parameters() json.RawMessage { return t.info.InputSchema }
func (t *mcpTool) IsConcurrencySafe() bool { return t.info.IsConcurrencySafe() }
func (t *mcpTool) IsReadOnly() bool        { return t.info.IsReadOnly() }
func (t *mcpTool) PermissionPattern(_ json.RawMessage) string {
	return t.prefix + t.info.Name + ":*"
}

// RegisterSSETools discovers tools from an SSE client and registers them.
// Skips registration on name collision (see RegisterMCPTools).
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
		if _, exists := registry.Get(st.Name()); exists {
			continue
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
func (t *sseMCPTool) IsConcurrencySafe() bool { return t.info.IsConcurrencySafe() }
func (t *sseMCPTool) IsReadOnly() bool        { return t.info.IsReadOnly() }
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
