package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// writeMessage writes an LSP message with Content-Length header framing.
func writeMessage(w io.Writer, msg *rpcMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(w, header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	_, err = w.Write(data)
	return err
}

// readMessage reads a single Content-Length framed LSP message.
func (c *Client) readMessage() (*rpcMessage, error) {
	length, err := readContentLength(c.stdout)
	if err != nil {
		return nil, err
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &msg, nil
}

// readContentLength parses headers until the blank line, returning
// the Content-Length value.
func readContentLength(r interface{ ReadString(byte) (string, error) }) (int, error) {
	var length int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length: ") {
			val := strings.TrimPrefix(line, "Content-Length: ")
			length, err = strconv.Atoi(val)
			if err != nil {
				return 0, fmt.Errorf("bad Content-Length %q: %w", val, err)
			}
		}
	}
	if length <= 0 {
		return 0, fmt.Errorf("missing Content-Length header")
	}
	return length, nil
}

// readLoop runs in a goroutine, dispatching incoming messages.
func (c *Client) readLoop() {
	defer close(c.done)
	for {
		msg, err := c.readMessage()
		if err != nil {
			return
		}
		c.dispatch(msg)
	}
}

// dispatch routes an incoming message to the right handler.
func (c *Client) dispatch(msg *rpcMessage) {
	// Notification from server (has method, no id).
	if msg.Method != "" && msg.ID == 0 {
		c.handleNotification(msg)
		return
	}
	// Response to our request (has id, no method).
	if msg.ID != 0 && msg.Method == "" {
		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		if ok {
			delete(c.pending, msg.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- msg
		}
		return
	}
	// Server request (has both) -- respond with empty success.
	if msg.Method != "" && msg.ID != 0 {
		c.respondEmpty(msg.ID)
	}
}

// handleNotification processes publishDiagnostics and ignores others.
func (c *Client) handleNotification(msg *rpcMessage) {
	if msg.Method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(msg.Params, &p) != nil {
		return
	}
	c.diagsMu.Lock()
	c.diags[p.URI] = p.Diagnostics
	c.diagsMu.Unlock()
}

// respondEmpty sends a success response with null result.
func (c *Client) respondEmpty(id int64) {
	resp := &rpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage("null"),
	}
	_ = writeMessage(c.stdin, resp)
}

// call sends a JSON-RPC request and waits for the response.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	msg, err := newRequest(id, method, params)
	if err != nil {
		return err
	}

	ch := make(chan *rpcMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := writeMessage(c.stdin, msg); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}

	select {
	case resp := <-ch:
		return handleResponse(resp, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("connection closed")
	}
}

// handleResponse extracts the result or error from a response.
func handleResponse(resp *rpcMessage, result any) error {
	if resp.Error != nil {
		return resp.Error
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, result)
}

// notify sends a JSON-RPC notification (no response expected).
func (c *Client) notify(method string, params any) error {
	msg, err := newNotification(method, params)
	if err != nil {
		return err
	}
	return writeMessage(c.stdin, msg)
}

// newRequest builds a JSON-RPC request message.
func newRequest(id int64, method string, params any) (*rpcMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	return &rpcMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  raw,
	}, nil
}

// newNotification builds a JSON-RPC notification message.
func newNotification(method string, params any) (*rpcMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	return &rpcMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	}, nil
}

// marshalParams marshals params, returning nil for nil input.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	data, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	return data, nil
}
