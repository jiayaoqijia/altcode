package backends

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// CodexRPCClient drives a multi-turn Codex session via the
// `codex app-server --listen stdio://` JSON-RPC 2.0 protocol.
type CodexRPCClient struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	scanner    *bufio.Scanner
	mu         sync.Mutex
	writeMu    sync.Mutex // serializes JSON-RPC writes to stdin
	nextID     int
	pending    map[int]chan rpcResult
	threadID   string
	turnDone   chan bool
	readerDone chan struct{} // closed when readLoop exits
	onEvent    func(CodexEvent)
	output     strings.Builder
}

// CodexEvent is emitted during a turn for streaming to the TUI.
type CodexEvent struct {
	Type    string // "text", "tool_start", "tool_done", "error", "turn_done"
	Content string
	Tool    string
}

// rpcResult carries a JSON-RPC response or error back to the caller.
type rpcResult struct {
	Result json.RawMessage
	Err    error
}

// NewCodexRPCClient spawns `codex app-server --listen stdio://` and
// performs the full handshake (initialize + thread/start). The client
// is ready for SendTurn after this returns.
func NewCodexRPCClient(
	ctx context.Context, workdir string, env []string,
) (*CodexRPCClient, error) {
	cmd := exec.CommandContext(ctx, "codex", "app-server", "--listen", "stdio://")
	cmd.Dir = workdir
	cmd.Env = env

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	c := &CodexRPCClient{
		cmd:      cmd,
		stdin:    stdinPipe,
		stdout:   stdoutPipe,
		pending:  make(map[int]chan rpcResult),
		turnDone: make(chan bool, 1),
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	c.scanner = scanner

	c.readerDone = make(chan struct{})
	go func() {
		defer close(c.readerDone)
		c.readLoop()
	}()

	return c, nil
}

// Start performs the JSON-RPC handshake: initialize, initialized
// notification, and thread/start. Extracts threadID for later turns.
func (c *CodexRPCClient) Start(ctx context.Context, workdir string) error {
	// Match multica's exact initialize params for compatibility
	_, err := c.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "altcode-agent-sdk",
			"title":   "Altcode Agent SDK",
			"version": "0.9.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	c.notify("initialized")

	// Match multica's thread/start params (approvalPolicy prevents
	// interactive prompts even if codex defaults to ask mode)
	res, err := c.request(ctx, "thread/start", map[string]any{
		"cwd":                    workdir,
		"sandbox":                "workspace-write",
		"approvalPolicy":         nil,
		"persistExtendedHistory": true,
		"experimentalRawEvents":  false,
	})
	if err != nil {
		return fmt.Errorf("thread/start: %w", err)
	}

	c.threadID = rpcExtractThreadID(res)
	if c.threadID == "" {
		return fmt.Errorf("thread/start returned no thread ID")
	}
	return nil
}

// SendTurn sends a turn/start request and returns immediately. Call
// Wait to block until the turn completes.
func (c *CodexRPCClient) SendTurn(
	ctx context.Context, prompt string,
) error {
	// Drain any stale signal from a previous turn.
	select {
	case <-c.turnDone:
	default:
	}

	_, err := c.request(ctx, "turn/start", map[string]any{
		"threadId": c.threadID,
		"input": []map[string]any{
			{"type": "text", "text": prompt},
		},
	})
	return err
}

// Wait blocks until the current turn completes (or context cancels)
// and returns accumulated text output.
func (c *CodexRPCClient) Wait(ctx context.Context) (string, error) {
	select {
	case <-c.turnDone:
		c.mu.Lock()
		out := c.output.String()
		c.output.Reset()
		c.mu.Unlock()
		return out, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close shuts down the codex process by closing stdin, waiting for the
// reader goroutine to finish, then waiting for the process to exit.
func (c *CodexRPCClient) Close() error {
	c.stdin.Close()
	<-c.readerDone // wait for readLoop to finish (no use-after-close)
	return c.cmd.Wait()
}

// readLoop is the background goroutine reading JSON-RPC lines from
// codex stdout.
func (c *CodexRPCClient) readLoop() {
	for c.scanner.Scan() {
		line := strings.TrimSpace(c.scanner.Text())
		if line == "" {
			continue
		}
		c.handleLine(line)
	}
	c.closeAllPending(fmt.Errorf("codex process exited"))
}

// handleLine parses one JSON-RPC message from stdout and routes it.
func (c *CodexRPCClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	// Response to our request (has id + result or error).
	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
		// Server request (has id + method) -- approval requests.
		if _, hasMethod := raw["method"]; hasMethod {
			c.handleServerRequest(raw)
			return
		}
	}

	// Notification (no id, has method).
	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

// handleResponse routes a JSON-RPC response to the waiting caller.
func (c *CodexRPCClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if !ok {
		return
	}

	if errData, hasErr := raw["error"]; hasErr {
		var rpcErr struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(errData, &rpcErr)
		ch <- rpcResult{
			Err: fmt.Errorf("rpc error %d: %s", rpcErr.Code, rpcErr.Message),
		}
	} else {
		ch <- rpcResult{Result: raw["result"]}
	}
}

// handleServerRequest responds to approval requests from Codex.
//
// Drops requests where id or method can't be parsed instead of
// silently auto-approving with a synthetic id=0 / method="". The
// previous version would call respond(0, ...) on a malformed
// request, sending an approval to whatever real pending caller
// happened to have id=0 — a real correctness hazard.
func (c *CodexRPCClient) handleServerRequest(
	raw map[string]json.RawMessage,
) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		return
	}
	var method string
	if err := json.Unmarshal(raw["method"], &method); err != nil {
		return
	}

	switch method {
	case "item/commandExecution/requestApproval",
		"execCommandApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	case "item/fileChange/requestApproval",
		"applyPatchApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	default:
		c.respond(id, map[string]any{})
	}
}

// handleNotification processes codex/event and raw v2 notifications.
func (c *CodexRPCClient) handleNotification(
	raw map[string]json.RawMessage,
) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}

	switch {
	case method == "codex/event" ||
		strings.HasPrefix(method, "codex/event/"):
		c.handleLegacyEvent(params)
	case method == "turn/completed":
		c.signalTurnDone()
	case method == "turn/started":
		c.emitEvent(CodexEvent{Type: "text", Content: "[turn started]"})
	case strings.HasPrefix(method, "item/"):
		c.handleItemNotification(method, params)
	}
}

// handleLegacyEvent processes the legacy codex/event notification
// format.
func (c *CodexRPCClient) handleLegacyEvent(params map[string]any) {
	msgData, ok := params["msg"]
	if !ok {
		return
	}
	msg, ok := msgData.(map[string]any)
	if !ok {
		return
	}
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "agent_message":
		text, _ := msg["message"].(string)
		c.appendOutput(text)
		c.emitEvent(CodexEvent{Type: "text", Content: text})
	case "exec_command_begin":
		cmd, _ := msg["command"].(string)
		c.emitEvent(CodexEvent{
			Type: "tool_start", Tool: "exec_command", Content: cmd,
		})
	case "exec_command_end":
		c.emitEvent(CodexEvent{Type: "tool_done", Tool: "exec_command"})
	case "patch_apply_begin":
		c.emitEvent(CodexEvent{Type: "tool_start", Tool: "patch_apply"})
	case "patch_apply_end":
		c.emitEvent(CodexEvent{Type: "tool_done", Tool: "patch_apply"})
	case "task_complete":
		c.signalTurnDone()
	case "turn_aborted":
		c.signalTurnDone()
	}
}

// handleItemNotification processes raw v2 item/* notifications.
func (c *CodexRPCClient) handleItemNotification(
	method string, params map[string]any,
) {
	item, ok := params["item"].(map[string]any)
	if !ok {
		return
	}
	itemType, _ := item["type"].(string)

	switch {
	case method == "item/started" && itemType == "commandExecution":
		cmd, _ := item["command"].(string)
		c.emitEvent(CodexEvent{
			Type: "tool_start", Tool: "exec_command", Content: cmd,
		})
	case method == "item/completed" && itemType == "commandExecution":
		output, _ := item["aggregatedOutput"].(string)
		c.emitEvent(CodexEvent{
			Type: "tool_done", Tool: "exec_command", Content: output,
		})
	case method == "item/started" && itemType == "fileChange":
		c.emitEvent(CodexEvent{Type: "tool_start", Tool: "patch_apply"})
	case method == "item/completed" && itemType == "fileChange":
		c.emitEvent(CodexEvent{Type: "tool_done", Tool: "patch_apply"})
	case method == "item/completed" && itemType == "agentMessage":
		text, _ := item["text"].(string)
		c.appendOutput(text)
		c.emitEvent(CodexEvent{Type: "text", Content: text})
	}
}

// writeJSON serializes v as JSON + newline to stdin under writeMu.
// All stdin writes go through this method to prevent concurrent
// JSON-RPC messages from interleaving at the byte level.
func (c *CodexRPCClient) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	_, err = c.stdin.Write(data)
	c.writeMu.Unlock()
	return err
}

// request sends a JSON-RPC request and blocks until the response.
func (c *CodexRPCClient) request(
	ctx context.Context, method string, params any,
) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := c.writeJSON(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case res := <-ch:
		return res.Result, res.Err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// notify sends a JSON-RPC notification (no response expected).
func (c *CodexRPCClient) notify(method string) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	_ = c.writeJSON(msg)
}

// respond sends a JSON-RPC response to a server-initiated request.
func (c *CodexRPCClient) respond(id int, result any) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	_ = c.writeJSON(msg)
}

// closeAllPending unblocks all pending request callers with an error.
func (c *CodexRPCClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- rpcResult{Err: err}
		delete(c.pending, id)
	}
}

// signalTurnDone delivers the turn-done signal, non-blocking.
func (c *CodexRPCClient) signalTurnDone() {
	c.emitEvent(CodexEvent{Type: "turn_done"})
	select {
	case c.turnDone <- true:
	default:
	}
}

// emitEvent calls the onEvent callback if set.
func (c *CodexRPCClient) emitEvent(ev CodexEvent) {
	if c.onEvent != nil {
		c.onEvent(ev)
	}
}

// appendOutput adds text to the accumulated output buffer.
func (c *CodexRPCClient) appendOutput(text string) {
	if text == "" {
		return
	}
	c.mu.Lock()
	c.output.WriteString(text)
	c.mu.Unlock()
}

// rpcExtractThreadID extracts the thread ID from a thread/start
// response.
func rpcExtractThreadID(result json.RawMessage) string {
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.Thread.ID
}
