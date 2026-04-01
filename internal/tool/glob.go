package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type globTool struct{}

// NewGlobTool creates a tool that finds files matching a glob pattern.
func NewGlobTool() Tool { return &globTool{} }

func (t *globTool) Name() string                                  { return "glob" }
func (t *globTool) Description() string {
	return "Find files by name pattern. Use this to locate files before creating new ones — check if similar files already exist. Supports patterns like *.go or *.ts."
}
func (t *globTool) IsConcurrencySafe() bool                       { return true }
func (t *globTool) IsReadOnly() bool                              { return true }
func (t *globTool) PermissionPattern(_ json.RawMessage) string    { return "glob:*" }

func (t *globTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern (e.g. **/*.go)"},
			"path": {"type": "string", "description": "Base directory to search in"}
		},
		"required": ["pattern"]
	}`)
}

func (t *globTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	base := params.Path
	if base == "" {
		base, _ = os.Getwd()
	}

	var matches []string
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		matched, _ := filepath.Match(params.Pattern, filepath.Base(rel))
		if matched {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "glob",
		}, nil
	}

	type fileInfo struct {
		path    string
		modTime int64
	}
	var files []fileInfo
	for _, m := range matches {
		fullPath := filepath.Join(base, m)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: m, modTime: info.ModTime().UnixNano()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	var sb strings.Builder
	for _, f := range files {
		sb.WriteString(f.path)
		sb.WriteByte('\n')
	}

	return &Result{
		Output: sb.String(),
		Title:  fmt.Sprintf("glob %s (%d matches)", params.Pattern, len(files)),
	}, nil
}
