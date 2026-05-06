// Package mcp implements a basic Model Context Protocol client over stdio.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jiayaoqijia/altcode/internal/envscrub"
)

// callResult carries either a successful result or an RPC error.
type callResult struct {
	Result json.RawMessage
	Error  *jsonRPCError
}

// Client communicates with an MCP server via JSON-RPC 2.0 over stdio.
type Client struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdinMu   sync.Mutex // serializes JSON-RPC line writes
	scanner   *bufio.Scanner
	nextID    atomic.Int64
	pending   map[int64]chan callResult
	mu        sync.Mutex
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
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
// Performs the JSON-RPC initialize handshake and sends the
// notifications/initialized message before returning, so callers can
// immediately invoke tools/list. MCP servers that enforce the
// initialization sequence (most modelcontextprotocol/server-* impls)
// previously refused tools/list with -32002 "Server not initialized"
// because we never spoke the handshake.
func Connect(ctx context.Context, command string, args []string, env []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	// Build the child env: scrubbed parent env + caller-supplied entries.
	// Caller-supplied env (cfg.Env from config.MCPServerConfig) is layered
	// on AFTER the scrubbed parent so explicit overrides win — this is
	// how a user opts a specific MCP server back into a particular
	// secret (e.g. a GITHUB_TOKEN scoped to one server). Without this
	// scrub, MCP stdio servers inherited the full parent env including
	// OTEL_*, CLAUDE_*, ALTCODE_*, and any provider keys. Codex round-R.
	cmd.Env = append(envscrub.Scrub(os.Environ()), env...)
	// Configure a process group so Close can SIGKILL the entire tree
	// (servers that fork helpers leave them orphaned otherwise).
	configureMCPProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("mcp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		// stdout is closed by cmd on Start failure
		return nil, fmt.Errorf("mcp: start %q: %w", command, err)
	}

	// Default scanner buffer is 64KiB which silently truncates large
	// MCP responses (e.g., grep results, file lists, screenshot blobs).
	// Bump to 4 MiB so realistic tool replies don't drop the stream.
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		scanner: scanner,
		pending: make(map[int64]chan callResult),
		done:    make(chan struct{}),
	}

	go c.readLoop()

	// Run the MCP initialize handshake. Bound by a short timeout so a
	// hung server doesn't block startup forever.
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.initialize(initCtx); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("mcp: initialize handshake: %w", err)
	}
	return c, nil
}

// initialize performs the MCP initialize/initialized handshake.
// Many real MCP servers refuse tools/list and other requests until
// the client has completed this sequence. Servers that don't
// implement initialize at all (older / minimal / mock servers) get
// a -32601 "method not found" — we treat that as a non-fatal signal
// to skip the handshake and proceed to discovery.
func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "altcode",
			"version": "0.1",
		},
	}
	if _, err := c.Call(ctx, "initialize", params); err != nil {
		// Server doesn't implement initialize — skip the notification
		// and let downstream calls proceed. Real spec-compliant servers
		// always accept initialize.
		if isMethodNotFound(err) {
			return nil
		}
		return err
	}
	// notifications/initialized is a notification, not a request — no
	// id, no expected response. Send it as a raw line through stdin.
	notif := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	c.stdinMu.Lock()
	_, err := c.stdin.Write(notif)
	c.stdinMu.Unlock()
	if err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}
	return nil
}

// isMethodNotFound reports whether err carries a JSON-RPC -32601 code.
func isMethodNotFound(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "rpc error -32601") ||
		contains(err.Error(), "method not found") ||
		contains(err.Error(), "unknown method")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
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
			// Non-blocking send: if the original Call() already
			// returned (ctx cancel, timeout) the buffered slot may
			// be full or unattended. Don't deadlock the read loop.
			select {
			case ch <- callResult{Result: resp.Result, Error: resp.Error}:
			default:
			}
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

	ch := make(chan callResult, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	// Always remove the pending entry on the way out so a write failure,
	// ctx cancel, or unexpected return path doesn't leak the channel
	// + map slot. Without this, late responses pile up in readLoop.
	cleanup := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}

	// Serialize stdin writes — concurrent Calls would otherwise
	// interleave JSON-RPC lines mid-message and corrupt the stream.
	c.stdinMu.Lock()
	_, werr := c.stdin.Write(data)
	c.stdinMu.Unlock()
	if werr != nil {
		cleanup()
		return nil, fmt.Errorf("mcp: write: %w", werr)
	}

	select {
	case cr := <-ch:
		if cr.Error != nil {
			return nil, fmt.Errorf("mcp rpc error %d: %s",
				cr.Error.Code, cr.Error.Message)
		}
		return cr.Result, nil
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	case <-c.done:
		cleanup()
		return nil, fmt.Errorf("mcp: connection closed")
	}
}

// Close terminates the MCP server process. Closes stdin first to give
// the server a chance to shut down cleanly, then waits with a timeout
// before SIGKILLing the process group. Without the timeout + kill,
// servers that ignore EOF would hang Close forever.
// Safe to call multiple times concurrently.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.stdin != nil {
			c.stdin.Close()
		}
		if c.cmd == nil || c.cmd.Process == nil {
			return
		}
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case err := <-done:
			c.closeErr = err
		case <-time.After(5 * time.Second):
			// Server didn't exit cleanly — kill the whole group.
			_ = killMCPProcessGroup(c.cmd)
			<-done
			c.closeErr = fmt.Errorf("mcp: server did not exit cleanly, killed")
		}
	})
	return c.closeErr
}
