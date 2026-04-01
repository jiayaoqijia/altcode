package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type bashTool struct{}

// NewBashTool creates a tool that executes bash commands.
func NewBashTool() Tool { return &bashTool{} }

func (t *bashTool) Name() string               { return "bash" }
func (t *bashTool) Description() string         { return "Execute a bash command and return its output." }
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

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
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
