package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/sandbox"
)

const maxOutputBytes = 512 * 1024 // 512KB

func truncateOutput(s string) string {
	if len(s) > maxOutputBytes {
		return s[:maxOutputBytes] + "\n[output truncated — exceeded 512KB]"
	}
	return s
}

type bashTool struct {
	sandbox *sandbox.Sandbox
}

// NewBashTool creates a tool that executes bash commands.
func NewBashTool() Tool { return &bashTool{} }

// NewBashToolWithSandbox creates a bash tool with sandbox checking.
func NewBashToolWithSandbox(sb *sandbox.Sandbox) Tool {
	return &bashTool{sandbox: sb}
}

func (t *bashTool) Name() string               { return "bash" }
func (t *bashTool) Description() string {
	return "Execute a bash command. Use for builds, tests, git, and commands without dedicated tools. Do NOT use for file reading (use read), searching (use grep), or file listing (use ls). Prefer short, targeted commands."
}
func (t *bashTool) IsConcurrencySafe() bool     { return false }
func (t *bashTool) IsReadOnly() bool            { return false }
func (t *bashTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ Command string `json:"command"` }
	json.Unmarshal(input, &p)
	return "bash:" + p.Command
}

func (t *bashTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to execute"},
			"timeout": {"type": "integer", "description": "Timeout in milliseconds (default 120000)"}
		},
		"required": ["command"]
	}`)
}

func (t *bashTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	if t.sandbox != nil {
		if err := t.sandbox.Check(params.Command); err != nil {
			return &Result{
				Output: fmt.Sprintf("Error: %v", err),
				Title:  "sandbox:blocked",
				Error:  err,
			}, nil
		}
	}

	timeout := 120 * time.Second
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", params.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	// If the context deadline fired, annotate the output so the agent
	// knows the command was killed by timeout (not a crash) and can
	// retry with an explicit larger `timeout` parameter if needed.
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	if timedOut {
		if output != "" && !strings.HasSuffix(output, "\n") {
			output += "\n"
		}
		output += fmt.Sprintf("[bash: command killed after %s — pass a larger `timeout` (ms) to run longer]", timeout)
	}

	output = truncateOutput(output)

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	title := params.Command
	if len(title) > 60 {
		title = title[:60] + "..."
	}

	return &Result{
		Output:   output,
		Title:    title,
		Metadata: map[string]any{"exit_code": exitCode},
	}, nil
}
