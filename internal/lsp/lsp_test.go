package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestWriteMessageFraming(t *testing.T) {
	msg := &rpcMessage{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}
	var buf bytes.Buffer
	if err := writeMessage(&buf, msg); err != nil {
		t.Fatalf("writeMessage: %v", err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "Content-Length: ") {
		t.Fatalf("missing Content-Length header, got: %q", got)
	}
	parts := strings.SplitN(got, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("expected header + body, got: %q", got)
	}
	body := parts[1]
	var decoded rpcMessage
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if decoded.Method != "initialize" {
		t.Errorf("method = %q, want initialize", decoded.Method)
	}
}

func TestReadContentLength(t *testing.T) {
	input := "Content-Length: 42\r\n\r\n"
	r := bufio.NewReader(strings.NewReader(input))
	length, err := readContentLength(r)
	if err != nil {
		t.Fatalf("readContentLength: %v", err)
	}
	if length != 42 {
		t.Errorf("length = %d, want 42", length)
	}
}

func TestReadContentLengthMissing(t *testing.T) {
	input := "Content-Type: application/json\r\n\r\n"
	r := bufio.NewReader(strings.NewReader(input))
	_, err := readContentLength(r)
	if err == nil {
		t.Fatal("expected error for missing Content-Length")
	}
}

func TestRoundtripMessage(t *testing.T) {
	original := &rpcMessage{
		JSONRPC: "2.0",
		ID:      7,
		Method:  "textDocument/hover",
		Params:  json.RawMessage(`{"textDocument":{"uri":"file:///test.go"}}`),
	}
	var buf bytes.Buffer
	if err := writeMessage(&buf, original); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := &Client{stdout: bufio.NewReader(&buf)}
	got, err := c.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if got.ID != original.ID {
		t.Errorf("id = %d, want %d", got.ID, original.ID)
	}
	if got.Method != original.Method {
		t.Errorf("method = %q, want %q", got.Method, original.Method)
	}
}

func TestNewRequest(t *testing.T) {
	msg, err := newRequest(3, "test/method", map[string]int{"x": 1})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if msg.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", msg.JSONRPC)
	}
	if msg.ID != 3 {
		t.Errorf("id = %d, want 3", msg.ID)
	}
	if msg.Method != "test/method" {
		t.Errorf("method = %q", msg.Method)
	}
}

func TestNewNotification(t *testing.T) {
	msg, err := newNotification("initialized", struct{}{})
	if err != nil {
		t.Fatalf("newNotification: %v", err)
	}
	if msg.ID != 0 {
		t.Errorf("notification should have id 0, got %d", msg.ID)
	}
	if msg.Method != "initialized" {
		t.Errorf("method = %q", msg.Method)
	}
}

func TestNewRequestNilParams(t *testing.T) {
	msg, err := newRequest(1, "shutdown", nil)
	if err != nil {
		t.Fatalf("newRequest nil params: %v", err)
	}
	if msg.Params != nil {
		t.Errorf("expected nil params, got %s", msg.Params)
	}
}

func TestDiagnosticsCacheReadWrite(t *testing.T) {
	c := &Client{
		diags: make(map[string][]Diagnostic),
	}
	uri := "file:///test.go"
	c.diagsMu.Lock()
	c.diags[uri] = []Diagnostic{
		{
			Range:    Range{Start: Position{Line: 0, Character: 0}},
			Severity: 1,
			Message:  "undefined: foo",
			Source:   "compiler",
		},
	}
	c.diagsMu.Unlock()

	got := c.GetDiagnostics(uri)
	if len(got) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(got))
	}
	if got[0].Message != "undefined: foo" {
		t.Errorf("message = %q", got[0].Message)
	}
}

func TestHandleNotificationDiagnostics(t *testing.T) {
	c := &Client{diags: make(map[string][]Diagnostic)}
	params := map[string]any{
		"uri": "file:///main.go",
		"diagnostics": []map[string]any{
			{
				"range": map[string]any{
					"start": map[string]int{"line": 5, "character": 0},
					"end":   map[string]int{"line": 5, "character": 10},
				},
				"severity": 2,
				"message":  "unused variable",
				"source":   "go vet",
			},
		},
	}
	raw, _ := json.Marshal(params)
	msg := &rpcMessage{
		JSONRPC: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params:  raw,
	}
	c.handleNotification(msg)

	diags := c.GetDiagnostics("file:///main.go")
	if len(diags) != 1 {
		t.Fatalf("expected 1 diag, got %d", len(diags))
	}
	if diags[0].Severity != 2 {
		t.Errorf("severity = %d, want 2", diags[0].Severity)
	}
}

func TestHandleNotificationIgnoresOther(t *testing.T) {
	c := &Client{diags: make(map[string][]Diagnostic)}
	msg := &rpcMessage{
		JSONRPC: "2.0",
		Method:  "window/logMessage",
		Params:  json.RawMessage(`{"type":3,"message":"info"}`),
	}
	c.handleNotification(msg)
	if len(c.diags) != 0 {
		t.Error("expected no diagnostics stored")
	}
}

func TestLanguageForURI(t *testing.T) {
	tests := []struct {
		uri  string
		want string
	}{
		{"file:///project/main.go", "go"},
		{"file:///src/index.ts", "typescript"},
		{"file:///src/app.tsx", "typescript"},
		{"file:///lib/util.js", "typescript"},
		{"file:///script.py", "python"},
		{"file:///readme.md", ""},
	}
	for _, tt := range tests {
		got := LanguageForURI(tt.uri)
		if got != tt.want {
			t.Errorf("LanguageForURI(%q) = %q, want %q", tt.uri, got, tt.want)
		}
	}
}

func TestManagerNewManager(t *testing.T) {
	m := NewManager("/tmp/project")
	if _, ok := m.configs["go"]; !ok {
		t.Error("missing go config")
	}
	if _, ok := m.configs["typescript"]; !ok {
		t.Error("missing typescript config")
	}
	if _, ok := m.configs["python"]; !ok {
		t.Error("missing python config")
	}
}

func TestManagerDiagnosticsNoServer(t *testing.T) {
	m := NewManager("/tmp")
	diags := m.DiagnosticsForFile("file:///tmp/main.go")
	if diags != nil {
		t.Error("expected nil diagnostics when no server running")
	}
}

func TestFormatDiagnostics(t *testing.T) {
	diags := []Diagnostic{
		{
			Range:    Range{Start: Position{Line: 9, Character: 4}},
			Severity: 1,
			Message:  "undeclared name",
			Source:   "gopls",
		},
	}
	got := formatDiagnostics(diags)
	if !strings.Contains(got, "10:5") {
		t.Errorf("expected 1-indexed position, got: %s", got)
	}
	if !strings.Contains(got, "error") {
		t.Errorf("expected severity string, got: %s", got)
	}
}

func TestFormatLocations(t *testing.T) {
	locs := []Location{
		{URI: "file:///src/main.go", Range: Range{Start: Position{Line: 0, Character: 0}}},
	}
	got := formatLocations(locs)
	if !strings.Contains(got, "/src/main.go:1:1") {
		t.Errorf("unexpected output: %s", got)
	}
}

func TestFormatLocationsEmpty(t *testing.T) {
	got := formatLocations(nil)
	if got != "No definition found." {
		t.Errorf("expected empty message, got: %s", got)
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  int
		want string
	}{
		{1, "error"}, {2, "warning"}, {3, "info"}, {4, "hint"}, {99, "unknown"},
	}
	for _, tt := range tests {
		got := severityString(tt.sev)
		if got != tt.want {
			t.Errorf("severityString(%d) = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestInitializeParams(t *testing.T) {
	p := initializeParams("/project")
	root, ok := p["rootUri"].(string)
	if !ok || root != "file:///project" {
		t.Errorf("rootUri = %v", p["rootUri"])
	}
}

func TestDidOpenParams(t *testing.T) {
	p := didOpenParams("file:///test.go", "go", "package main")
	td, ok := p["textDocument"].(map[string]any)
	if !ok {
		t.Fatal("missing textDocument")
	}
	if td["languageId"] != "go" {
		t.Errorf("languageId = %v", td["languageId"])
	}
}

func TestPositionParams(t *testing.T) {
	p := positionParams("file:///test.go", 10, 5)
	pos, ok := p["position"].(map[string]int)
	if !ok {
		t.Fatal("missing position")
	}
	if pos["line"] != 10 || pos["character"] != 5 {
		t.Errorf("position = %v", pos)
	}
}

func TestContentLengthFramingExact(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"test"}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)

	c := &Client{stdout: bufio.NewReader(strings.NewReader(frame))}
	msg, err := c.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if msg.Method != "test" {
		t.Errorf("method = %q", msg.Method)
	}
}

func TestDispatchResponse(t *testing.T) {
	c := &Client{
		pending: make(map[int64]chan *rpcMessage),
		diags:   make(map[string][]Diagnostic),
	}
	ch := make(chan *rpcMessage, 1)
	c.pending[5] = ch

	resp := &rpcMessage{JSONRPC: "2.0", ID: 5, Result: json.RawMessage(`null`)}
	c.dispatch(resp)

	select {
	case got := <-ch:
		if got.ID != 5 {
			t.Errorf("id = %d, want 5", got.ID)
		}
	default:
		t.Error("expected response on channel")
	}
}

func TestDispatchNotification(t *testing.T) {
	c := &Client{
		pending: make(map[int64]chan *rpcMessage),
		diags:   make(map[string][]Diagnostic),
	}
	params, _ := json.Marshal(map[string]any{
		"uri":         "file:///x.go",
		"diagnostics": []any{},
	})
	msg := &rpcMessage{
		JSONRPC: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params:  params,
	}
	c.dispatch(msg)

	diags := c.GetDiagnostics("file:///x.go")
	if diags == nil {
		t.Error("expected empty slice, got nil")
	}
}

func TestRPCErrorFormat(t *testing.T) {
	e := &rpcError{Code: -32600, Message: "invalid request"}
	got := e.Error()
	if !strings.Contains(got, "-32600") {
		t.Errorf("error = %q, want code included", got)
	}
}

func TestHandleResponseError(t *testing.T) {
	resp := &rpcMessage{
		Error: &rpcError{Code: -32601, Message: "method not found"},
	}
	err := handleResponse(resp, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "method not found") {
		t.Errorf("error = %v", err)
	}
}

func TestHandleResponseNilResult(t *testing.T) {
	resp := &rpcMessage{Result: json.RawMessage(`null`)}
	err := handleResponse(resp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultConfigs(t *testing.T) {
	cfgs := DefaultConfigs()
	if len(cfgs) != 3 {
		t.Fatalf("expected 3 default configs, got %d", len(cfgs))
	}
	found := map[string]bool{}
	for _, c := range cfgs {
		found[c.Language] = true
	}
	for _, lang := range []string{"go", "typescript", "python"} {
		if !found[lang] {
			t.Errorf("missing default config for %s", lang)
		}
	}
}
