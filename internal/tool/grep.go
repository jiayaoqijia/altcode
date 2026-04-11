package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type grepTool struct{}

// NewGrepTool creates a tool that searches file contents using ripgrep or grep.
func NewGrepTool() Tool { return &grepTool{} }

func (t *grepTool) Name() string                               { return "grep" }
func (t *grepTool) Description() string {
	return "Search file contents using regex patterns. Use this instead of bash grep — it's faster and provides better output. Results capped at 200 lines."
}
func (t *grepTool) IsConcurrencySafe() bool                    { return true }
func (t *grepTool) IsReadOnly() bool                           { return true }
func (t *grepTool) PermissionPattern(_ json.RawMessage) string { return "grep:*" }

func (t *grepTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Regex pattern to search for"},
			"path": {"type": "string", "description": "Directory or file to search in"},
			"glob": {"type": "string", "description": "File glob filter (e.g. *.go)"},
			"case_insensitive": {"type": "boolean", "description": "Case insensitive search"}
		},
		"required": ["pattern"]
	}`)
}

func (t *grepTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		Glob            string `json:"glob"`
		CaseInsensitive bool   `json:"case_insensitive"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	searchPath := params.Path
	if searchPath == "" {
		searchPath = "."
	}

	bin, args := buildGrepArgs(params.Pattern, searchPath, params.Glob, params.CaseInsensitive)

	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	output := string(out)

	// Distinguish 'no matches' (rg/grep exit code 1) from real errors
	// (exit code 2 = malformed regex, missing path, permission denied,
	// etc.). Previously any error with empty output was reported as
	// 'No matches found.', so a typo'd regex looked like a clean miss.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && output == "" {
			return &Result{
				Output: "No matches found.",
				Title:  "grep " + params.Pattern,
			}, nil
		}
		stderr := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return &Result{
			Output: fmt.Sprintf("Error: %s", msg),
			Title:  "grep " + params.Pattern,
			Error:  fmt.Errorf("grep failed: %s", msg),
		}, nil
	}

	lines := strings.Split(output, "\n")
	if len(lines) > 200 {
		output = strings.Join(lines[:200], "\n") +
			fmt.Sprintf("\n... (%d more lines)", len(lines)-200)
	}

	return &Result{
		Output: output,
		Title:  fmt.Sprintf("grep %s (%d matches)", params.Pattern, len(lines)-1),
	}, nil
}

func buildGrepArgs(pattern, path, glob string, caseInsensitive bool) (string, []string) {
	if _, err := exec.LookPath("rg"); err == nil {
		args := []string{"--no-heading", "--line-number", "--color=never"}
		if caseInsensitive {
			args = append(args, "-i")
		}
		if glob != "" {
			args = append(args, "--glob", glob)
		}
		args = append(args, pattern, path)
		return "rg", args
	}

	args := []string{"-rn"}
	if caseInsensitive {
		args = append(args, "-i")
	}
	args = append(args, pattern, path)
	return "grep", args
}
