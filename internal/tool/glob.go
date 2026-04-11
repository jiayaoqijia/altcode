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

// matchGlob matches `rel` against `pattern` with support for `**`
// (recursive segment wildcard). Without **, falls back to filepath.Match
// against both the basename and the full relative path so a pattern like
// `*.go` matches every file regardless of depth.
//
// filepath.Match alone returned false for `**/*.go` against any path,
// so the tool description (which advertises `**/*.go`) was a lie.
func matchGlob(pattern, rel string) bool {
	// Normalize Windows separators so patterns are portable.
	rel = filepath.ToSlash(rel)
	pattern = filepath.ToSlash(pattern)

	if strings.Contains(pattern, "**") {
		// Split on `**` and match the prefix/suffix segments. The
		// middle is "any number of path segments". Two-segment split
		// covers `**/*.go`, `pkg/**/*.go`, `**/test/**`, etc.
		parts := strings.Split(pattern, "**")
		if len(parts) > 0 {
			// Trim leading/trailing slashes that flank the **.
			prefix := strings.TrimSuffix(parts[0], "/")
			if prefix != "" && !strings.HasPrefix(rel, prefix) {
				return false
			}
			rest := rel
			if prefix != "" {
				rest = strings.TrimPrefix(rel, prefix+"/")
			}
			// For each remaining segment-pattern, ensure it can match
			// some suffix of `rest`. The simplest correct approach for
			// the common single-** case is to match the final segment
			// against the basename.
			tail := strings.TrimPrefix(parts[len(parts)-1], "/")
			if tail == "" {
				return true
			}
			// Try to match `tail` against any suffix of rest (per
			// segment). Cheap walk that handles `**/*.go` and similar.
			segs := strings.Split(rest, "/")
			for i := 0; i < len(segs); i++ {
				suffix := strings.Join(segs[i:], "/")
				if ok, _ := filepath.Match(tail, suffix); ok {
					return true
				}
				if ok, _ := filepath.Match(tail, segs[len(segs)-1]); ok {
					return true
				}
			}
			return false
		}
	}

	// Without **, try the full path first (for patterns like `pkg/*.go`)
	// then fall back to the basename (for plain `*.go`).
	if ok, _ := filepath.Match(pattern, rel); ok {
		return true
	}
	if ok, _ := filepath.Match(pattern, filepath.Base(rel)); ok {
		return true
	}
	return false
}

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

func (t *globTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
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
		// Honor cancellation so a long walk doesn't outlive the user
		// (Ctrl+C, session shutdown).
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		if matchGlob(params.Pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "glob",
			Error:  err,
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
