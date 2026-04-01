package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type lsTool struct{}

// NewLsTool creates a tool that lists directory contents.
func NewLsTool() Tool { return &lsTool{} }

func (t *lsTool) Name() string                               { return "ls" }
func (t *lsTool) Description() string                        { return "List directory contents." }
func (t *lsTool) IsConcurrencySafe() bool                    { return true }
func (t *lsTool) IsReadOnly() bool                           { return true }
func (t *lsTool) PermissionPattern(_ json.RawMessage) string { return "ls:*" }

func (t *lsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Directory path to list"}
		},
		"required": ["path"]
	}`)
}

func (t *lsTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	entries, err := os.ReadDir(params.Path)
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "ls",
		}, nil
	}

	var sb strings.Builder
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		typeChar := "-"
		if e.IsDir() {
			typeChar = "d"
		}
		fmt.Fprintf(&sb, "%s %8d %s\n", typeChar, info.Size(), e.Name())
	}

	return &Result{
		Output: sb.String(),
		Title:  fmt.Sprintf("ls %s (%d entries)", params.Path, len(entries)),
	}, nil
}
