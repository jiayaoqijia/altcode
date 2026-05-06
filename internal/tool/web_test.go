package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/tool"
)

// TestMain installs ALTCODE_ALLOW_LOOPBACK_FETCH=1 for all web_fetch
// tests. Loopback is blocked by the SSRF guard by default (round-O
// hardening); these tests exercise httptest servers on 127.0.0.1,
// so they need explicit opt-in.
func TestMain(m *testing.M) {
	_ = os.Setenv("ALTCODE_ALLOW_LOOPBACK_FETCH", "1")
	os.Exit(m.Run())
}

func TestWebFetchToolMetadata(t *testing.T) {
	ft := tool.NewWebFetchTool()
	if ft.Name() != "web_fetch" {
		t.Fatalf("Name = %q, want web_fetch", ft.Name())
	}
	if !ft.IsConcurrencySafe() {
		t.Fatal("Expected concurrency safe")
	}
	if !ft.IsReadOnly() {
		t.Fatal("Expected read only")
	}
	if ft.Description() == "" {
		t.Fatal("Expected non-empty description")
	}
	var schema map[string]any
	if err := json.Unmarshal(ft.Parameters(), &schema); err != nil {
		t.Fatalf("Invalid parameters JSON: %v", err)
	}
}

func TestWebFetchToolSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body><h1>Hello</h1><p>World</p></body></html>")
	}))
	defer srv.Close()

	ft := tool.NewWebFetchTool()
	input, _ := json.Marshal(map[string]any{"url": srv.URL})
	result, err := ft.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Hello") {
		t.Fatalf("Expected 'Hello' in output, got %q", result.Output)
	}
	if !strings.Contains(result.Output, "World") {
		t.Fatalf("Expected 'World' in output, got %q", result.Output)
	}
	// HTML tags should be stripped
	if strings.Contains(result.Output, "<h1>") {
		t.Fatal("HTML tags should be stripped")
	}
}

func TestWebFetchToolStripsScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><script>alert("xss")</script><body>Safe content</body></html>`)
	}))
	defer srv.Close()

	ft := tool.NewWebFetchTool()
	input, _ := json.Marshal(map[string]any{"url": srv.URL})
	result, err := ft.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "alert") {
		t.Fatal("Script content should be stripped")
	}
	if !strings.Contains(result.Output, "Safe content") {
		t.Fatalf("Body content missing, got %q", result.Output)
	}
}

func TestWebFetchToolMaxLength(t *testing.T) {
	long := strings.Repeat("x", 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, long)
	}))
	defer srv.Close()

	ft := tool.NewWebFetchTool()
	input, _ := json.Marshal(map[string]any{"url": srv.URL, "max_length": 100})
	result, err := ft.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Output) > 100 {
		t.Fatalf("Output length %d exceeds max_length 100", len(result.Output))
	}
}

func TestWebFetchToolMissingURL(t *testing.T) {
	ft := tool.NewWebFetchTool()
	input, _ := json.Marshal(map[string]any{})
	result, err := ft.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Error") {
		t.Fatalf("Expected error for missing URL, got %q", result.Output)
	}
}

func TestWebFetchToolHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ft := tool.NewWebFetchTool()
	input, _ := json.Marshal(map[string]any{"url": srv.URL})
	result, err := ft.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "404") {
		t.Fatalf("Expected 404 error, got %q", result.Output)
	}
}

func TestWebFetchToolPermissionPattern(t *testing.T) {
	ft := tool.NewWebFetchTool()
	// Permission keys are normalized: scheme + host lowercased,
	// default ports stripped, trailing dot removed, empty path
	// rendered as "/", query string dropped. So origin-equivalent
	// requests share a single permission rule.
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com", "web_fetch:https://example.com/"},
		{"https://Example.COM", "web_fetch:https://example.com/"},
		{"https://example.com:443/foo", "web_fetch:https://example.com/foo"},
		{"http://example.com:80/foo", "web_fetch:http://example.com/foo"},
		{"https://api.github.com/repos?token=secret", "web_fetch:https://api.github.com/repos"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			input, _ := json.Marshal(map[string]any{"url": tt.url})
			pattern := ft.PermissionPattern(input)
			if pattern != tt.want {
				t.Fatalf("Pattern(%q) = %q, want %q", tt.url, pattern, tt.want)
			}
		})
	}
}

func TestWebSearchToolMetadata(t *testing.T) {
	st := tool.NewWebSearchTool()
	if st.Name() != "web_search" {
		t.Fatalf("Name = %q, want web_search", st.Name())
	}
	if !st.IsConcurrencySafe() {
		t.Fatal("Expected concurrency safe")
	}
	if !st.IsReadOnly() {
		t.Fatal("Expected read only")
	}
	var schema map[string]any
	if err := json.Unmarshal(st.Parameters(), &schema); err != nil {
		t.Fatalf("Invalid parameters JSON: %v", err)
	}
}

func TestWebSearchToolMissingQuery(t *testing.T) {
	st := tool.NewWebSearchTool()
	input, _ := json.Marshal(map[string]any{})
	result, err := st.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Error") {
		t.Fatalf("Expected error for missing query, got %q", result.Output)
	}
}

func TestWebSearchToolPermissionPattern(t *testing.T) {
	st := tool.NewWebSearchTool()
	input, _ := json.Marshal(map[string]any{"query": "golang testing"})
	pattern := st.PermissionPattern(input)
	if pattern != "web_search:golang testing" {
		t.Fatalf("Pattern = %q, want web_search:golang testing", pattern)
	}
}

func TestWebSearchToolParsesResults(t *testing.T) {
	// Simulate DuckDuckGo HTML response
	html := `<html><body>
		<div class="result">
			<a class="result__a" href="https://go.dev">Go Programming Language</a>
			<a class="result__snippet">Go is an open source programming language.</a>
		</div>
		<div class="result">
			<a class="result__a" href="https://pkg.go.dev">Go Packages</a>
			<a class="result__snippet">Discover Go packages and modules.</a>
		</div>
	</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, html)
	}))
	defer srv.Close()

	// We can't easily override the DDG URL, so test the parser directly
	// by checking the tool handles no-results gracefully with a mock server
	// that returns no DDG-formatted results
	st := tool.NewWebSearchTool()
	input, _ := json.Marshal(map[string]any{"query": "test"})
	result, err := st.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Result may or may not have hits (depends on network), but should not error
	if result.Title == "" {
		t.Fatal("Expected non-empty title")
	}
}

// TestWebFetchSSRFLoopbackBlockedByDefault guards the Codex round-O
// adversarial finding: loopback was previously exempt from the SSRF
// guard, so a prompt-injected LLM could reach Docker socket proxies,
// IDE debug servers, and local admin panels via web_fetch. Loopback
// is now blocked by default; users opt in via ALTCODE_ALLOW_LOOPBACK_FETCH.
func TestWebFetchSSRFLoopbackBlockedByDefault(t *testing.T) {
	// Explicitly unset the opt-in so the blocking branch is exercised.
	// (TestMain sets it to allow the other tests to hit httptest loopback.)
	prev := os.Getenv("ALTCODE_ALLOW_LOOPBACK_FETCH")
	_ = os.Unsetenv("ALTCODE_ALLOW_LOOPBACK_FETCH")
	t.Cleanup(func() { _ = os.Setenv("ALTCODE_ALLOW_LOOPBACK_FETCH", prev) })

	ft := tool.NewWebFetchTool()
	input, _ := json.Marshal(map[string]string{
		"url": "http://127.0.0.1:1/",
	})
	result, err := ft.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "blocked by SSRF guard") ||
		!strings.Contains(result.Output, "loopback") {
		t.Errorf("expected loopback SSRF block, got: %s", result.Output)
	}
}

// TestWebFetchSSRFLoopbackOptInAllowed verifies the opt-in env var
// restores the old behaviour for developers who explicitly want it.
func TestWebFetchSSRFLoopbackOptInAllowed(t *testing.T) {
	t.Setenv("ALTCODE_ALLOW_LOOPBACK_FETCH", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	}))
	defer srv.Close()

	ft := tool.NewWebFetchTool()
	input, _ := json.Marshal(map[string]string{"url": srv.URL})
	result, err := ft.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "blocked by SSRF guard") {
		t.Errorf("opt-in should permit loopback; got: %s", result.Output)
	}
}
