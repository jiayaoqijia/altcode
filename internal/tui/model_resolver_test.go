package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedModelCache writes a fake ~/.altcode/models-cache.json under the
// per-test HOME so resolveModelQuery sees a known set of model IDs.
// Returns the temp HOME the caller should also pass via t.Setenv.
func seedModelCache(t *testing.T, ids ...string) string {
	t.Helper()
	tmpHome := t.TempDir()
	dir := filepath.Join(tmpHome, ".altcode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	type entry struct {
		Info struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"info"`
	}
	cache := map[string]entry{}
	for i, id := range ids {
		var e entry
		e.Info.ID = id
		e.Info.ContextLength = 100000
		cache[string(rune('a'+i))+"-key"] = e
	}
	body, _ := json.MarshalIndent(cache, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "models-cache.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)
	return tmpHome
}

func TestResolveModelQuery_QualifiedPassthrough(t *testing.T) {
	seedModelCache(t,
		"anthropic/claude-haiku-4-5",
		"deepseek/deepseek-v4-pro",
	)
	a := testApp()
	got, err := a.resolveModelQuery("anthropic/claude-haiku-4-5")
	if err != nil {
		t.Fatalf("qualified should pass through, got err=%v", err)
	}
	if got != "anthropic/claude-haiku-4-5" {
		t.Errorf("got %q, want exact passthrough", got)
	}
}

func TestResolveModelQuery_ExactCacheHit(t *testing.T) {
	seedModelCache(t, "anthropic/claude-haiku-4-5", "openai/gpt-5.4")
	a := testApp()
	got, err := a.resolveModelQuery("openai/gpt-5.4")
	if err != nil {
		t.Fatalf("exact qualified hit should not error: %v", err)
	}
	if got != "openai/gpt-5.4" {
		t.Errorf("got %q, want openai/gpt-5.4", got)
	}
}

func TestResolveModelQuery_UniqueSubstring(t *testing.T) {
	seedModelCache(t,
		"anthropic/claude-haiku-4-5",
		"openai/gpt-5.4",
		"deepseek/deepseek-v4-pro",
	)
	a := testApp()
	cases := map[string]string{
		"haiku":    "anthropic/claude-haiku-4-5",
		"gpt-5.4":  "openai/gpt-5.4",
		"v4-pro":   "deepseek/deepseek-v4-pro",
		"deepseek": "deepseek/deepseek-v4-pro",
	}
	for q, want := range cases {
		got, err := a.resolveModelQuery(q)
		if err != nil {
			t.Errorf("query %q: unexpected err=%v", q, err)
			continue
		}
		if got != want {
			t.Errorf("query %q: got %q, want %q", q, got, want)
		}
	}
}

func TestResolveModelQuery_AmbiguousReportsCandidates(t *testing.T) {
	seedModelCache(t,
		"deepseek/deepseek-v4-pro",
		"deepseek/deepseek-v4-flash",
		"deepseek/deepseek-v3.2",
	)
	a := testApp()
	_, err := a.resolveModelQuery("deepseek")
	if err == nil {
		t.Fatal("ambiguous query should return an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ambiguous") {
		t.Errorf("error should say 'ambiguous', got: %s", msg)
	}
	for _, want := range []string{"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v3.2"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should list candidate %q, got: %s", want, msg)
		}
	}
}

func TestResolveModelQuery_NoCacheHitFallsThrough(t *testing.T) {
	// Empty cache (no file written).
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	a := testApp()

	got, err := a.resolveModelQuery("some-bare-name")
	if err != nil {
		t.Fatalf("empty cache should pass through, got err=%v", err)
	}
	if got != "some-bare-name" {
		t.Errorf("got %q, want passthrough 'some-bare-name'", got)
	}
}

func TestKnownModels_ParsesAndDedupes(t *testing.T) {
	seedModelCache(t,
		"anthropic/claude-haiku-4-5",
		"openai/gpt-5.4",
		"deepseek/deepseek-v4-pro",
	)
	a := testApp()
	got := a.knownModels()
	if len(got) != 3 {
		t.Errorf("expected 3 distinct models, got %d: %v", len(got), got)
	}
	// Sorted by Strings(out) — anthropic comes first alphabetically.
	if got[0] != "anthropic/claude-haiku-4-5" {
		t.Errorf("expected sorted output, got %v", got)
	}
}
