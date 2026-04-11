package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type writeTool struct{}

// NewWriteTool creates a tool that writes content to files.
func NewWriteTool() Tool { return &writeTool{} }

func (t *writeTool) Name() string               { return "write" }
func (t *writeTool) Description() string {
	return "Write content to a file, creating directories if needed. Prefer the edit tool for modifying existing files — use write only for new files or complete rewrites."
}
func (t *writeTool) IsConcurrencySafe() bool     { return false }
func (t *writeTool) IsReadOnly() bool            { return false }
func (t *writeTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ FilePath string `json:"file_path"` }
	json.Unmarshal(input, &p)
	return "write:" + p.FilePath
}

func (t *writeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to write to"},
			"content": {"type": "string", "description": "Content to write"}
		},
		"required": ["file_path", "content"]
	}`)
}

func (t *writeTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	dir := filepath.Dir(params.FilePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Result.Error must be set so the dispatcher renders this as a
		// failed tool call. Without it, the agent loop sees a "success"
		// result with apologetic prose and assumes the file was written.
		return &Result{
			Output: fmt.Sprintf("Error creating directory: %v", err),
			Title:  "write",
			Error:  fmt.Errorf("mkdir %s: %w", dir, err),
		}, nil
	}

	if err := os.WriteFile(params.FilePath, []byte(params.Content), 0o644); err != nil {
		return &Result{
			Output: fmt.Sprintf("Error writing file: %v", err),
			Title:  "write",
			Error:  fmt.Errorf("write %s: %w", params.FilePath, err),
		}, nil
	}

	return &Result{
		Output: fmt.Sprintf("Wrote %d bytes to %s", len(params.Content), params.FilePath),
		Title:  "write " + params.FilePath,
	}, nil
}
