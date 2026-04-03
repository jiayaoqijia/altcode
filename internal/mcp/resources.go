package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Resource represents a read-only resource from an MCP server.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// DiscoverResources calls resources/list on the MCP server.
func (c *Client) DiscoverResources(ctx context.Context) ([]Resource, error) {
	result, err := c.Call(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp resources/list: %w", err)
	}
	return parseResourceList(result)
}

// ReadResource calls resources/read on the MCP server.
func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	params := map[string]any{"uri": uri}
	result, err := c.Call(ctx, "resources/read", params)
	if err != nil {
		return "", fmt.Errorf("mcp resources/read %s: %w", uri, err)
	}
	return parseResourceContent(result)
}

// DiscoverResources calls resources/list via SSE transport.
func (c *SSEClient) DiscoverResources(ctx context.Context) ([]Resource, error) {
	result, err := c.Call(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp sse resources/list: %w", err)
	}
	return parseResourceList(result)
}

// ReadResource calls resources/read via SSE transport.
func (c *SSEClient) ReadResource(ctx context.Context, uri string) (string, error) {
	params := map[string]any{"uri": uri}
	result, err := c.Call(ctx, "resources/read", params)
	if err != nil {
		return "", fmt.Errorf("mcp sse resources/read %s: %w", uri, err)
	}
	return parseResourceContent(result)
}

func parseResourceList(raw json.RawMessage) ([]Resource, error) {
	var resp struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("mcp parse resources: %w", err)
	}
	return resp.Resources, nil
}

func parseResourceContent(raw json.RawMessage) (string, error) {
	var resp struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType,omitempty"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return string(raw), nil
	}
	if len(resp.Contents) > 0 {
		return resp.Contents[0].Text, nil
	}
	return string(raw), nil
}
