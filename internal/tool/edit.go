package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type editTool struct{}

// NewEditTool creates a tool that performs exact string replacement in files.
func NewEditTool() Tool { return &editTool{} }

func (t *editTool) Name() string               { return "edit" }
func (t *editTool) Description() string {
	return "Perform exact string replacement in a file. You MUST read the file first. Provide enough surrounding context in old_string to make the match unique. The edit will FAIL if old_string matches multiple locations."
}
func (t *editTool) IsConcurrencySafe() bool     { return false }
func (t *editTool) IsReadOnly() bool            { return false }
func (t *editTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ FilePath string `json:"file_path"` }
	json.Unmarshal(input, &p)
	return "edit:" + p.FilePath
}

func (t *editTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to edit"},
			"old_string": {"type": "string", "description": "Exact string to find"},
			"new_string": {"type": "string", "description": "Replacement string"},
			"replace_all": {"type": "boolean", "description": "Replace all occurrences"}
		},
		"required": ["file_path", "old_string", "new_string"]
	}`)
}

func (t *editTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error reading file: %v", err),
			Title:  "edit",
		}, nil
	}

	content := string(data)
	if !strings.Contains(content, params.OldString) {
		return &Result{
			Output: "Error: old_string not found in file.",
			Title:  "edit " + params.FilePath,
			Error:  fmt.Errorf("old_string not found"),
		}, nil
	}

	count := strings.Count(content, params.OldString)
	if count > 1 && !params.ReplaceAll {
		return &Result{
			Output: fmt.Sprintf("Error: old_string found %d times. Use replace_all or provide more context.", count),
			Title:  "edit " + params.FilePath,
			Error:  fmt.Errorf("ambiguous match: %d occurrences", count),
		}, nil
	}

	var newContent string
	if params.ReplaceAll {
		newContent = strings.ReplaceAll(content, params.OldString, params.NewString)
	} else {
		newContent = strings.Replace(content, params.OldString, params.NewString, 1)
	}

	if err := os.WriteFile(params.FilePath, []byte(newContent), 0o644); err != nil {
		return &Result{
			Output: fmt.Sprintf("Error writing file: %v", err),
			Title:  "edit",
		}, nil
	}

	return &Result{
		Output: fmt.Sprintf("Replaced %d occurrence(s) in %s", count, params.FilePath),
		Title:  "edit " + params.FilePath,
	}, nil
}
