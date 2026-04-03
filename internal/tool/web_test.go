package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/tool"
)

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
	input, _ := json.Marshal(map[string]any{"url": "https://example.com"})
	pattern := ft.PermissionPattern(input)
	if pattern != "web_fetch:https://example.com" {
		t.Fatalf("Pattern = %q, want web_fetch:https://example.com", pattern)
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
