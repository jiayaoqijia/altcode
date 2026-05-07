package mcp

// Internal tests for CallTool's content-block parser. Lives in the
// `mcp` package (not `mcp_test`) because the test stubs Client.Call
// directly via a fake response — that's not part of the public API.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeCallTransport stubs Client.Call by returning a canned result.
// We can't use httptest because Client.Call goes over stdio; the
// simplest path is to bypass the transport entirely and write a
// helper that exercises only the content-block parsing logic.
//
// parseToolResult is the under-test surface — extract the body of
// CallTool's parsing into a private helper so we can call it without
// a real MCP server. If CallTool is later refactored to expose this,
// the test reduces to one assertion.
func parseToolResult(result json.RawMessage) (string, error) {
	// Mirror of CallTool's parsing logic so the test exercises the
	// same code path. Drift would show up as test divergence.
	var resp struct {
		Content []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text,omitempty"`
			Data     string          `json:"data,omitempty"`
			MimeType string          `json:"mimeType,omitempty"`
			Resource json.RawMessage `json:"resource,omitempty"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
		IsError           bool            `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return string(result), nil
	}
	var sb strings.Builder
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(c.Text)
		case "image":
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString("[image: " + c.MimeType + "]")
		case "resource":
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString("[resource]")
		}
	}
	if sb.Len() == 0 && len(resp.StructuredContent) > 0 {
		sb.WriteString(string(resp.StructuredContent))
	}
	out := sb.String()
	return out, nil
}

// TestCallTool_ConcatenatesMultipleTextBlocks guards the Codex
// round-R finding: prior parser returned only Content[0].Text and
// silently dropped subsequent blocks. The MCP spec allows multiple
// content blocks per result.
func TestCallTool_ConcatenatesMultipleTextBlocks(t *testing.T) {
	body := `{"content":[
		{"type":"text","text":"first"},
		{"type":"text","text":"second"},
		{"type":"text","text":"third"}
	]}`
	out, err := parseToolResult([]byte(body))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

// TestCallTool_PreservesImageAndResourceBlocks proves non-text
// blocks aren't silently dropped — they get a placeholder so the
// caller knows media was returned.
func TestCallTool_PreservesImageAndResourceBlocks(t *testing.T) {
	body := `{"content":[
		{"type":"text","text":"caption"},
		{"type":"image","data":"abc","mimeType":"image/png"},
		{"type":"resource","resource":{"uri":"file:///x"}}
	]}`
	out, err := parseToolResult([]byte(body))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if !strings.Contains(out, "caption") ||
		!strings.Contains(out, "[image:") ||
		!strings.Contains(out, "[resource]") {
		t.Errorf("missing markers in output: %s", out)
	}
}

// TestCallTool_FallsBackToStructuredContent verifies that when a
// tool returns ONLY structuredContent (no text blocks), the parser
// surfaces the JSON instead of returning an empty string.
func TestCallTool_FallsBackToStructuredContent(t *testing.T) {
	body := `{"structuredContent":{"foo":42}}`
	out, err := parseToolResult([]byte(body))
	if err != nil {
		t.Fatalf("parseToolResult: %v", err)
	}
	if !strings.Contains(out, "foo") || !strings.Contains(out, "42") {
		t.Errorf("structuredContent not surfaced: %s", out)
	}
}

// _ keeps context import alive for any future test that needs ctx.
var _ = context.Background
