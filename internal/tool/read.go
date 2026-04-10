package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type readTool struct{}

// NewReadTool creates a tool that reads file contents with optional line range.
func NewReadTool() Tool { return &readTool{} }

func (t *readTool) Name() string                                  { return "read" }
func (t *readTool) Description() string {
	return "Read a file from the local filesystem. You MUST read a file before editing it. Use offset and limit for large files to read specific portions."
}
func (t *readTool) IsConcurrencySafe() bool                       { return true }
func (t *readTool) IsReadOnly() bool                              { return true }
func (t *readTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ FilePath string `json:"file_path"` }
	json.Unmarshal(input, &p)
	return "read:" + p.FilePath
}

func (t *readTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Absolute path to the file"},
			"offset": {"type": "integer", "description": "Line to start from (0-indexed)"},
			"limit": {"type": "integer", "description": "Max lines to read"}
		},
		"required": ["file_path"]
	}`)
}

func (t *readTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	// Safety cap: refuse to load files larger than 2MB without explicit limit.
	// At 4 chars per token, a 2MB file eats ~500k tokens and blows the request
	// budget long before it reaches the API's 20MB payload ceiling.
	const maxReadBytes = 2 * 1024 * 1024
	if params.Limit == 0 {
		if fi, err := os.Stat(params.FilePath); err == nil && fi.Size() > maxReadBytes {
			return &Result{
				Output: fmt.Sprintf("Error: file is %d bytes (>2MB). Pass limit=<lines> to read a slice, or use grep/glob instead.", fi.Size()),
				Title:  fmt.Sprintf("%s (too large: %.1fMB)", params.FilePath, float64(fi.Size())/1024/1024),
				Error:  fmt.Errorf("file too large for full read"),
			}, nil
		}
	}

	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  params.FilePath,
			Error:  err,
		}, nil
	}

	lines := strings.Split(string(data), "\n")

	if params.Offset > 0 || params.Limit > 0 {
		start := params.Offset
		if start >= len(lines) {
			return &Result{Output: "", Title: params.FilePath}, nil
		}
		end := len(lines)
		if params.Limit > 0 && start+params.Limit < end {
			end = start + params.Limit
		}
		lines = lines[start:end]
	}

	var sb strings.Builder
	for i, line := range lines {
		lineNum := params.Offset + i + 1
		fmt.Fprintf(&sb, "%4d\t%s\n", lineNum, line)
	}

	return &Result{
		Output: sb.String(),
		Title:  fmt.Sprintf("%s (%d lines)", params.FilePath, len(lines)),
	}, nil
}
