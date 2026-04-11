package lsp

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
)

// Diagnostic represents an LSP diagnostic (error, warning, etc.) for a file.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=Error, 2=Warning, 3=Info, 4=Hint
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// Range is a span within a text document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position is a line/character offset in a document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// HoverResult holds the response from a textDocument/hover request.
type HoverResult struct {
	Contents json.RawMessage `json:"contents"`
}

// Location represents an LSP Location returned by definition/references.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Client manages a connection to a single LSP language server process.
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	nextID  atomic.Int64
	pending map[int64]chan *rpcMessage
	mu      sync.Mutex
	diags   map[string][]Diagnostic
	diagsMu sync.RWMutex
	done    chan struct{}
}

// rpcMessage is the JSON-RPC 2.0 wire format used by LSP.
//
// ID is a pointer so we can distinguish "id field absent" (notification)
// from "id field present and zero" (a valid request/response). The
// previous int64+omitempty form treated id=0 as "no id" and routed
// any server-originated request with id 0 as a notification, dropping
// it. Client requests start their ids at 1 via nextID.Add so the
// client side is unaffected.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// idValue safely dereferences msg.ID, returning 0 when nil. Use this
// only when you've already established the message has an id (e.g.
// inside the response branch of dispatch).
func (m *rpcMessage) idValue() int64 {
	if m == nil || m.ID == nil {
		return 0
	}
	return *m.ID
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc %d: %s", e.Code, e.Message)
}

// Connect starts an external language server process and returns a Client.
func Connect(ctx context.Context, command string, args []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 64*1024),
		pending: make(map[int64]chan *rpcMessage),
		diags:   make(map[string][]Diagnostic),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Initialize sends the initialize/initialized handshake.
func (c *Client) Initialize(ctx context.Context, rootPath string) error {
	params := initializeParams(rootPath)
	var result json.RawMessage
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return c.notify("initialized", struct{}{})
}

// DidOpen notifies the server that a document was opened.
func (c *Client) DidOpen(ctx context.Context, uri, languageID, text string) error {
	params := didOpenParams(uri, languageID, text)
	return c.notify("textDocument/didOpen", params)
}

// DidChange notifies the server of a full document replacement.
func (c *Client) DidChange(ctx context.Context, uri, text string) error {
	params := didChangeParams(uri, text)
	return c.notify("textDocument/didChange", params)
}

// GetDiagnostics returns a copy of cached diagnostics for the given URI.
// Previously this returned the backing slice directly, so callers
// could mutate shared state and race with readLoop's diags update.
// Returns a non-nil empty slice if the URI is known but currently has
// no diagnostics — distinct from "URI not tracked at all" (nil).
func (c *Client) GetDiagnostics(uri string) []Diagnostic {
	c.diagsMu.RLock()
	defer c.diagsMu.RUnlock()
	src, ok := c.diags[uri]
	if !ok {
		return nil
	}
	out := make([]Diagnostic, len(src))
	copy(out, src)
	return out
}

// Definition sends a textDocument/definition request.
func (c *Client) Definition(ctx context.Context, uri string, line, char int) ([]Location, error) {
	params := positionParams(uri, line, char)
	var locs []Location
	if err := c.call(ctx, "textDocument/definition", params, &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

// Hover sends a textDocument/hover request.
func (c *Client) Hover(ctx context.Context, uri string, line, char int) (*HoverResult, error) {
	params := positionParams(uri, line, char)
	var hover HoverResult
	if err := c.call(ctx, "textDocument/hover", params, &hover); err != nil {
		return nil, err
	}
	return &hover, nil
}

// Close performs the LSP shutdown/exit sequence and kills the process.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_ = c.call(ctx, "shutdown", nil, nil)
	_ = c.notify("exit", nil)
	_ = c.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		return fmt.Errorf("lsp server killed after timeout")
	}
}
