package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchModelInfo_HandlesBaseURLWithV1Suffix is the regression for
// the OpenRouter URL-doubling bug: baseURL "https://openrouter.ai/api/v1"
// previously produced "/api/v1/v1/models" and 404'd, falling back to
// the heuristic that hardcoded deepseek to 64K (real v4 is 1M).
func TestFetchModelInfo_HandlesBaseURLWithV1Suffix(t *testing.T) {
	var calledURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledURL = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []ModelInfo{
				{ID: "deepseek/deepseek-v4-pro", ContextLength: 1048576},
			},
		})
	}))
	defer srv.Close()

	// Simulate OpenRouter-style baseURL.
	got := FetchModelInfo(srv.URL+"/api/v1", "test-key", "deepseek/deepseek-v4-pro")
	if got == nil {
		t.Fatalf("expected ModelInfo, got nil — URL was %q", calledURL)
	}
	if got.ContextSize() != 1048576 {
		t.Errorf("ContextSize = %d, want 1048576", got.ContextSize())
	}
	if !strings.HasSuffix(calledURL, "/api/v1/models") {
		t.Errorf("URL = %q, want suffix /api/v1/models (no double /v1)", calledURL)
	}
}

// TestFetchModelInfo_HandlesBaseURLWithoutV1 keeps the original
// behaviour for providers whose baseURL does NOT include /v1.
func TestFetchModelInfo_HandlesBaseURLWithoutV1(t *testing.T) {
	var calledURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledURL = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []ModelInfo{
				{ID: "claude-sonnet-4-6", ContextLength: 200000},
			},
		})
	}))
	defer srv.Close()

	got := FetchModelInfo(srv.URL, "key", "claude-sonnet-4-6")
	if got == nil {
		t.Fatal("expected ModelInfo")
	}
	if calledURL != "/v1/models" {
		t.Errorf("URL = %q, want /v1/models", calledURL)
	}
}

// TestFetchModelInfo_TrailingSlashStripped — `https://api.foo.com/`
// must become `…/v1/models`, not `…//v1/models`.
func TestFetchModelInfo_TrailingSlashStripped(t *testing.T) {
	var calledURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledURL = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []ModelInfo{}})
	}))
	defer srv.Close()

	_ = FetchModelInfo(srv.URL+"/", "k", "m")
	if strings.HasPrefix(calledURL, "//") {
		t.Errorf("got double-slash URL: %q", calledURL)
	}
}

// TestFetchModelInfo_EmptyArgsReturnNil
func TestFetchModelInfo_EmptyArgsReturnNil(t *testing.T) {
	if got := FetchModelInfo("", "key", "model"); got != nil {
		t.Errorf("empty baseURL should return nil, got %+v", got)
	}
	if got := FetchModelInfo("https://x", "", "model"); got != nil {
		t.Errorf("empty apiKey should return nil, got %+v", got)
	}
}
