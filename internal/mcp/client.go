// Package mcp implements a basic Model Context Protocol client over stdio.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// Client communicates with an MCP server via JSON-RPC 2.0 over stdio.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	nextID  atomic.Int64
	pending map[int64]chan json.RawMessage
	mu      sync.Mutex
	done    chan struct{}
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Connect spawns an MCP server process and establishes communication.
func Connect(ctx context.Context, command string, args []string, env []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	if len(env) > 0 {
		cmd.Env = env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp: start %q: %w", command, err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
		pending: make(map[int64]chan json.RawMessage),
		done:    make(chan struct{}),
	}

	go c.readLoop()
	return c, nil
}

func (c *Client) readLoop() {
	defer close(c.done)
	for c.scanner.Scan() {
		line := c.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp jsonRPCResponse
		if json.Unmarshal(line, &resp) != nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp.Result
		}
	}
}

// Call sends a JSON-RPC request and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal: %w", err)
	}
	data = append(data, '\n')

	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("mcp: write: %w", err)
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("mcp: connection closed")
	}
}

// Close terminates the MCP server process.
func (c *Client) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}
