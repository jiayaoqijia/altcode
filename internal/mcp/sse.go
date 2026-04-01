package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SSEClient communicates with an MCP server via HTTP SSE transport.
type SSEClient struct {
	baseURL  string
	headers  map[string]string
	client   *http.Client
	nextID   atomic.Int64
	mu       sync.Mutex
	pending  map[int64]chan json.RawMessage
}

// ConnectSSE creates an MCP client using HTTP SSE transport.
func ConnectSSE(baseURL string, headers map[string]string) *SSEClient {
	return &SSEClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: headers,
		client:  &http.Client{Timeout: 5 * time.Minute},
		pending: make(map[int64]chan json.RawMessage),
	}
}

// Call sends a JSON-RPC request via HTTP POST and reads the SSE response.
func (c *SSEClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp sse request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mcp sse status %d: %s", resp.StatusCode, string(b))
	}

	// Read SSE response — look for data: lines with JSON-RPC response
	return readSSEResponse(resp.Body, id)
}

func readSSEResponse(body io.Reader, expectedID int64) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var resp jsonRPCResponse
		if json.Unmarshal([]byte(data), &resp) != nil {
			continue
		}
		if resp.ID == expectedID {
			if resp.Error != nil {
				return nil, fmt.Errorf("mcp rpc error %d: %s",
					resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}
	return nil, fmt.Errorf("mcp sse: no response for id %d", expectedID)
}

// DiscoverTools calls tools/list via SSE transport.
func (c *SSEClient) DiscoverTools(ctx context.Context) ([]ToolInfo, error) {
	result, err := c.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

// CallTool invokes a tool via SSE transport.
func (c *SSEClient) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	params := map[string]any{
		"name":      name,
		"arguments": json.RawMessage(args),
	}
	result, err := c.Call(ctx, "tools/call", params)
	if err != nil {
		return "", err
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

// Close is a no-op for SSE (stateless HTTP).
func (c *SSEClient) Close() error {
	return nil
}
