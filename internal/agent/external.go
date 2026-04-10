// Package agent provides external CLI agent support.
// ExternalAgent spawns codex, claude, or opencode CLI tools as subprocesses
// to execute tasks, streaming their output in real-time.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CLIBackend identifies which external CLI tool to use.
type CLIBackend string

const (
	BackendCodex   CLIBackend = "codex"
	BackendClaude  CLIBackend = "claude"
	BackendOpenCode CLIBackend = "opencode"
)

// ExternalAgentConfig configures an external CLI agent.
type ExternalAgentConfig struct {
	Backend            CLIBackend
	Role               string        // human-readable role name (e.g. "architect", "reviewer")
	Model              string        // model override (e.g. "o3", "claude-sonnet-4-20250514")
	Timeout            time.Duration // 0 = no timeout
	WorkDir            string        // working directory for the CLI
	MaxTurns           int           // max agentic turns (0 = unlimited)
	AppendSystemPrompt string        // appended to the agent's system prompt
	ResumeSessionID    string        // resume a prior session by ID
}

// AgentEventType identifies a typed streaming event.
type AgentEventType string

const (
	EventText       AgentEventType = "text"
	EventThinking   AgentEventType = "thinking"
	EventToolUse    AgentEventType = "tool_use"
	EventToolResult AgentEventType = "tool_result"
	EventStatus     AgentEventType = "status"
	EventError      AgentEventType = "error"
)

// AgentEvent is a typed streaming event from an external agent.
type AgentEvent struct {
	Type    AgentEventType
	Content string
	Tool    string // tool name for tool_use/tool_result
}

// ExternalAgentResult holds the outcome of an external agent run.
type ExternalAgentResult struct {
	Role      string
	Backend   CLIBackend
	Output    string
	Error     error
	Elapsed   time.Duration
	ExitCode  int
	SessionID string
	TimedOut  bool // true when the process was killed by its own ctx deadline
}

// ExternalAgentStream provides real-time output from an external agent.
type ExternalAgentStream struct {
	Lines  <-chan string               // raw streaming lines (for backward compat)
	Events <-chan AgentEvent           // typed streaming events
	Result <-chan ExternalAgentResult  // final result
	Cancel context.CancelFunc
}

// trySend sends v to ch without blocking. Drops if channel is full.
func trySend[T any](ch chan<- T, v T) {
	select {
	case ch <- v:
	default:
	}
}

// SpawnExternal launches an external CLI tool to execute a task.
// Returns a stream for real-time output monitoring.
func SpawnExternal(ctx context.Context, cfg ExternalAgentConfig, task string) *ExternalAgentStream {
	lines := make(chan string, 100)
	events := make(chan AgentEvent, 256)
	result := make(chan ExternalAgentResult, 1)

	runCtx := ctx
	var cancel context.CancelFunc
	if cfg.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}

	stream := &ExternalAgentStream{
		Lines:  lines,
		Events: events,
		Result: result,
		Cancel: cancel,
	}

	go func() {
		defer cancel()
		defer close(lines)
		defer close(events)
		defer close(result)

		start := time.Now()
		cmd := buildCLICommand(runCtx, cfg, task)
		if cfg.WorkDir != "" {
			cmd.Dir = cfg.WorkDir
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			result <- ExternalAgentResult{
				Role: cfg.Role, Backend: cfg.Backend,
				Error: fmt.Errorf("stdout pipe: %w", err), Elapsed: time.Since(start),
			}
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			result <- ExternalAgentResult{
				Role: cfg.Role, Backend: cfg.Backend,
				Error: fmt.Errorf("stderr pipe: %w", err), Elapsed: time.Since(start),
			}
			return
		}

		// For claude backend, open stdin pipe for control_request responses
		var stdin io.WriteCloser
		if cfg.Backend == BackendClaude {
			var stdinErr error
			stdin, stdinErr = cmd.StdinPipe()
			if stdinErr != nil {
				result <- ExternalAgentResult{
					Role: cfg.Role, Backend: cfg.Backend,
					Error: fmt.Errorf("stdin pipe: %w", stdinErr), Elapsed: time.Since(start),
				}
				return
			}
		}

		if err := cmd.Start(); err != nil {
			result <- ExternalAgentResult{
				Role: cfg.Role, Backend: cfg.Backend,
				Error: fmt.Errorf("start %s: %w", cfg.Backend, err), Elapsed: time.Since(start),
			}
			return
		}
		if stdin != nil {
			defer stdin.Close()
		}

		// Drain stderr in background
		go func() {
			sc := bufio.NewScanner(stderr)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for sc.Scan() {
				trySend(events, AgentEvent{Type: EventError, Content: sc.Text()})
			}
		}()

		// Stream stdout line by line
		var output strings.Builder
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			output.WriteString(line + "\n")

			// For claude: parse stream-json and handle control_request
			if cfg.Backend == BackendClaude && strings.HasPrefix(strings.TrimSpace(line), "{") {
				if handled := handleClaudeLine(line, stdin, events); handled {
					trySend(lines, line)
					continue
				}
			}

			// Raw line for backward compat
			trySend(lines, line)
			// Typed event
			trySend(events, AgentEvent{Type: EventText, Content: line})
		}

		exitCode := 0
		waitErr := cmd.Wait()
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		// Detect timeout kill: if the per-run context's deadline
		// fired, the process was SIGKILLed by us, not by a crash.
		// The caller (workspace TUI) uses this to show a clear
		// '[timed out after <d>]' message instead of 'exit -1'.
		timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)

		result <- ExternalAgentResult{
			Role:     cfg.Role,
			Backend:  cfg.Backend,
			Output:   output.String(),
			Error:    waitErr,
			Elapsed:  time.Since(start),
			ExitCode: exitCode,
			TimedOut: timedOut,
		}
	}()

	return stream
}

// SpawnTeam launches multiple external agents in parallel and collects results.
func SpawnTeam(ctx context.Context, agents []ExternalAgentConfig, task string) map[string]*ExternalAgentStream {
	streams := make(map[string]*ExternalAgentStream, len(agents))
	for _, cfg := range agents {
		streams[cfg.Role] = SpawnExternal(ctx, cfg, task)
	}
	return streams
}

// WaitAll blocks until all streams complete and returns results.
func WaitAll(streams map[string]*ExternalAgentStream, timeout time.Duration) map[string]ExternalAgentResult {
	results := make(map[string]ExternalAgentResult, len(streams))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for role, stream := range streams {
		wg.Add(1)
		go func(role string, s *ExternalAgentStream) {
			defer wg.Done()
			// Drain lines (they're consumed by TUI, but drain here as fallback)
			for range s.Lines {
			}
			r := <-s.Result
			mu.Lock()
			results[role] = r
			mu.Unlock()
		}(role, stream)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	if timeout > 0 {
		select {
		case <-done:
		case <-time.After(timeout):
			// Cancel remaining agents
			for _, s := range streams {
				s.Cancel()
			}
			<-done
		}
	} else {
		<-done
	}

	return results
}

// buildCLICommand constructs the exec.Cmd for the given backend.
func buildCLICommand(ctx context.Context, cfg ExternalAgentConfig, task string) *exec.Cmd {
	switch cfg.Backend {
	case BackendCodex:
		args := []string{"exec", "--dangerously-bypass-approvals-and-sandbox"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, task)
		return exec.CommandContext(ctx, "codex", args...)

	case BackendClaude:
		// claude --print is incompatible with --output-format=stream-json.
		// Use -p (print mode) without stream-json for one-shot tasks.
		args := []string{
			"--permission-mode", "bypassPermissions",
		}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		if cfg.MaxTurns > 0 {
			args = append(args, "--max-turns",
				fmt.Sprintf("%d", cfg.MaxTurns))
		}
		if cfg.AppendSystemPrompt != "" {
			args = append(args,
				"--append-system-prompt", cfg.AppendSystemPrompt)
		}
		if cfg.ResumeSessionID != "" {
			args = append(args, "--resume", cfg.ResumeSessionID)
		}
		args = append(args, "-p", task)
		return exec.CommandContext(ctx, "claude", args...)

	case BackendOpenCode:
		args := []string{"exec"}
		if cfg.Model != "" {
			args = append(args, "--model", cfg.Model)
		}
		args = append(args, task)
		return exec.CommandContext(ctx, "opencode", args...)

	default:
		// Fallback: try as generic command
		return exec.CommandContext(ctx, string(cfg.Backend), "exec", task)
	}
}

// DetectAvailableBackends returns which CLI tools are available on PATH.
func DetectAvailableBackends() []CLIBackend {
	var available []CLIBackend
	for _, b := range []CLIBackend{BackendCodex, BackendClaude, BackendOpenCode} {
		if _, err := exec.LookPath(string(b)); err == nil {
			available = append(available, b)
		}
	}
	return available
}

// handleClaudeLine parses a claude stream-json line and emits typed events.
// Handles control_request by auto-approving via stdin (multica pattern A4).
// Returns true if the line was handled (caller should not emit raw text).
func handleClaudeLine(line string, stdin io.Writer, events chan<- AgentEvent) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &raw) != nil {
		return false
	}

	var msgType string
	if t, ok := raw["type"]; ok {
		json.Unmarshal(t, &msgType)
	}

	switch msgType {
	case "control_request":
		// Auto-approve all tool use (like multica claude.go:224-260)
		handleClaudeControlRequest(raw, stdin)
		return true

	case "assistant":
		// Parse content blocks for text/thinking/tool_use
		ev := parseClaudeAssistant(line)
		if ev != nil {
			trySend(events, *ev)
			return true
		}

	case "result":
		var r struct {
			ResultText string `json:"result_text"`
			SessionID  string `json:"session_id"`
			IsError    bool   `json:"is_error"`
		}
		json.Unmarshal([]byte(line), &r)
		if r.IsError {
			trySend(events, AgentEvent{Type: EventError, Content: r.ResultText})
		} else {
			trySend(events, AgentEvent{Type: EventStatus, Content: r.ResultText})
		}
		return true

	case "system":
		trySend(events, AgentEvent{Type: EventStatus, Content: "system"})
		return true
	}

	return false
}

// handleClaudeControlRequest responds to a control_request with auto-approve.
func handleClaudeControlRequest(raw map[string]json.RawMessage, stdin io.Writer) {
	if stdin == nil {
		return
	}
	// Extract request_id from the control request
	var req struct {
		RequestID string `json:"request_id"`
	}
	if data, ok := raw["request"]; ok {
		json.Unmarshal(data, &req)
	}
	// If no nested request, try top-level
	if req.RequestID == "" {
		json.Unmarshal([]byte(raw["request_id"]), &req.RequestID)
	}

	// Send approval response via stdin (multica pattern)
	resp := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": req.RequestID,
			"response": map[string]any{
				"behavior":     "allow",
				"updatedInput": map[string]any{},
			},
		},
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	stdin.Write(data)
}

// parseClaudeAssistant extracts typed events from a claude assistant message.
func parseClaudeAssistant(line string) *AgentEvent {
	var msg struct {
		Message struct {
			Content []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Name  string `json:"name"`
				ID    string `json:"id"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &msg) != nil {
		return nil
	}
	for _, block := range msg.Message.Content {
		switch block.Type {
		case "text":
			return &AgentEvent{Type: EventText, Content: block.Text}
		case "thinking":
			return &AgentEvent{Type: EventThinking, Content: block.Text}
		case "tool_use":
			return &AgentEvent{Type: EventToolUse, Content: block.ID, Tool: block.Name}
		case "tool_result":
			return &AgentEvent{Type: EventToolResult, Tool: block.Name}
		}
	}
	return nil
}
