package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/tool"
)

type lspTool struct {
	manager *Manager
}

// NewLSPTool creates a tool the AI can call for LSP operations.
func NewLSPTool(manager *Manager) tool.Tool {
	return &lspTool{manager: manager}
}

func (t *lspTool) Name() string { return "lsp_diagnostics" }

func (t *lspTool) Description() string {
	return "Get LSP diagnostics, go-to-definition, or hover info for a file. " +
		"Actions: diagnostics, definition, hover."
}

func (t *lspTool) IsConcurrencySafe() bool { return true }
func (t *lspTool) IsReadOnly() bool        { return true }

func (t *lspTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ FilePath string `json:"file_path"` }
	_ = json.Unmarshal(input, &p)
	return "lsp:" + p.FilePath
}

func (t *lspTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Absolute path to the file"},
			"action": {"type": "string", "enum": ["diagnostics", "definition", "hover"]},
			"line": {"type": "integer", "description": "Line number (0-indexed)"},
			"character": {"type": "integer", "description": "Column (0-indexed)"}
		},
		"required": ["file_path", "action"]
	}`)
}

func (t *lspTool) Execute(ctx context.Context, input json.RawMessage) (*tool.Result, error) {
	var p lspParams
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	uri := "file://" + p.FilePath
	lang := LanguageForURI(uri)
	if lang == "" {
		return errorResult(p, "unsupported language"), nil
	}

	client, err := t.manager.GetOrStart(ctx, lang)
	if err != nil {
		return errorResult(p, err.Error()), nil
	}

	switch p.Action {
	case "diagnostics":
		return t.execDiagnostics(client, p, uri)
	case "definition":
		return t.execDefinition(ctx, client, p, uri)
	case "hover":
		return t.execHover(ctx, client, p, uri)
	default:
		return errorResult(p, "unknown action: "+p.Action), nil
	}
}

type lspParams struct {
	FilePath  string `json:"file_path"`
	Action    string `json:"action"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

func (t *lspTool) execDiagnostics(c *Client, p lspParams, uri string) (*tool.Result, error) {
	diags := c.GetDiagnostics(uri)
	if len(diags) == 0 {
		return &tool.Result{
			Output: "No diagnostics.",
			Title:  "lsp diagnostics " + p.FilePath,
		}, nil
	}
	return &tool.Result{
		Output: formatDiagnostics(diags),
		Title:  fmt.Sprintf("lsp diagnostics %s (%d)", p.FilePath, len(diags)),
	}, nil
}

func (t *lspTool) execDefinition(ctx context.Context, c *Client, p lspParams, uri string) (*tool.Result, error) {
	locs, err := c.Definition(ctx, uri, p.Line, p.Character)
	if err != nil {
		return errorResult(p, err.Error()), nil
	}
	return &tool.Result{
		Output: formatLocations(locs),
		Title:  fmt.Sprintf("lsp definition %s:%d:%d", p.FilePath, p.Line, p.Character),
	}, nil
}

func (t *lspTool) execHover(ctx context.Context, c *Client, p lspParams, uri string) (*tool.Result, error) {
	hover, err := c.Hover(ctx, uri, p.Line, p.Character)
	if err != nil {
		return errorResult(p, err.Error()), nil
	}
	return &tool.Result{
		Output: string(hover.Contents),
		Title:  fmt.Sprintf("lsp hover %s:%d:%d", p.FilePath, p.Line, p.Character),
	}, nil
}

func errorResult(p lspParams, msg string) *tool.Result {
	return &tool.Result{
		Output: "Error: " + msg,
		Title:  "lsp " + p.Action + " " + p.FilePath,
	}
}

func formatDiagnostics(diags []Diagnostic) string {
	var sb strings.Builder
	for _, d := range diags {
		sev := severityString(d.Severity)
		fmt.Fprintf(&sb, "%s:%d:%d %s: %s",
			d.Source, d.Range.Start.Line+1, d.Range.Start.Character+1,
			sev, d.Message)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func severityString(s int) string {
	switch s {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "unknown"
	}
}

func formatLocations(locs []Location) string {
	if len(locs) == 0 {
		return "No definition found."
	}
	var sb strings.Builder
	for _, loc := range locs {
		path := strings.TrimPrefix(loc.URI, "file://")
		fmt.Fprintf(&sb, "%s:%d:%d\n",
			path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return sb.String()
}
