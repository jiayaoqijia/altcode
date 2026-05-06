package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
)

// TestPlural covers the trivial English-pluralization helper used in
// turn summaries. The 0 case is also "s" to match natural phrasing
// ("0 files changed" not "0 file changed").
func TestPlural(t *testing.T) {
	cases := map[int]string{
		0:   "s",
		1:   "",
		2:   "s",
		100: "s",
	}
	for n, want := range cases {
		if got := plural(n); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestBuildTurnSummary_NoToolsReturnsEmpty covers the early-out branch.
func TestBuildTurnSummary_NoToolsReturnsEmpty(t *testing.T) {
	a := testApp()
	a.turnToolCount = 0
	if got := a.buildTurnSummary(); got != "" {
		t.Errorf("got %q, want empty for tool-free turn", got)
	}
}

// TestBuildTurnSummary_PluralizesAndJoins covers the format string for
// each tool category and the bullet-separated join.
func TestBuildTurnSummary_PluralizesAndJoins(t *testing.T) {
	a := testApp()
	a.turnToolCount = 5
	a.turnWrites = 3
	a.turnReads = 1
	a.turnBashes = 2

	got := a.buildTurnSummary()
	for _, want := range []string{"3 files changed", "1 file read", "2 commands"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in summary: %q", want, got)
		}
	}
	if !strings.HasPrefix(got, "✓") {
		t.Errorf("summary should start with ✓, got: %q", got)
	}
}

// TestBuildTurnSummary_AppendsCostAndTokens covers the cost-delta and
// token-delta branches.
func TestBuildTurnSummary_AppendsCostAndTokens(t *testing.T) {
	a := testApp()
	a.turnToolCount = 1
	a.turnWrites = 1
	a.costUSD = 0.50
	a.turnCostStart = 0.30
	a.tokensIn = 1000
	a.tokensOut = 500
	a.turnTokenStart = 200

	got := a.buildTurnSummary()
	if !strings.Contains(got, "$0.20") {
		t.Errorf("cost delta missing: %q", got)
	}
	// 1000+500-200 = 1300 tokens → "1.3K" (formatTokens uses uppercase K)
	if !strings.Contains(strings.ToLower(got), "1.3k") && !strings.Contains(got, "1300") {
		t.Errorf("token delta missing: %q", got)
	}
}

// TestBuildTurnMeta_NilEngineReturnsEmpty covers the engine guard.
func TestBuildTurnMeta_NilEngineReturnsEmpty(t *testing.T) {
	a := testApp()
	if got := a.buildTurnMeta(); got != "" {
		t.Errorf("nil-engine buildTurnMeta = %q, want empty", got)
	}
}

// TestBuildTurnMeta_ZeroTurnStartReturnsEmpty covers the zero-time guard.
func TestBuildTurnMeta_ZeroTurnStartReturnsEmpty(t *testing.T) {
	a := testApp()
	a.turnStart = time.Time{} // zero value
	if got := a.buildTurnMeta(); got != "" {
		t.Errorf("zero turnStart buildTurnMeta = %q, want empty", got)
	}
}

// TestExtractToolOutput_NilReturnsEmpty covers the nil-event guard.
func TestExtractToolOutput_NilReturnsEmpty(t *testing.T) {
	title, output := extractToolOutput(event.Event{ToolResult: nil})
	if title != "" || output != "" {
		t.Errorf("got (%q, %q), want both empty", title, output)
	}
}

// TestExtractToolOutput_NormalReturnsBoth covers the success branch.
func TestExtractToolOutput_NormalReturnsBoth(t *testing.T) {
	ev := event.Event{ToolResult: &event.Result{
		Title:  "edit /tmp/foo.go",
		Output: "patched 1 hunk",
	}}
	title, output := extractToolOutput(ev)
	if title != "edit /tmp/foo.go" {
		t.Errorf("title = %q", title)
	}
	if output != "patched 1 hunk" {
		t.Errorf("output = %q", output)
	}
}

// TestExtractToolOutput_ErrorOverridesOutput verifies that when the
// tool reports an error, the Error field replaces Output (so the user
// sees the failure, not stale partial success).
func TestExtractToolOutput_ErrorOverridesOutput(t *testing.T) {
	ev := event.Event{ToolResult: &event.Result{
		Title:  "edit /tmp/foo.go",
		Output: "should not show",
		Error:  "permission denied",
	}}
	_, output := extractToolOutput(ev)
	if output != "permission denied" {
		t.Errorf("output = %q, want error message", output)
	}
}

// TestToolFilePath_NilCallReturnsEmpty covers the nil guard.
func TestToolFilePath_NilCallReturnsEmpty(t *testing.T) {
	if got := toolFilePath(nil); got != "" {
		t.Errorf("nil call = %q, want empty", got)
	}
}

// TestToolFilePath_HappyPath extracts the file_path key from JSON input.
func TestToolFilePath_HappyPath(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"file_path": "/tmp/hello.go",
		"content":   "package main",
	})
	tc := &event.ToolCall{Input: input}
	if got := toolFilePath(tc); got != "/tmp/hello.go" {
		t.Errorf("got %q, want /tmp/hello.go", got)
	}
}

// TestToolFilePath_MissingKeyReturnsEmpty
func TestToolFilePath_MissingKeyReturnsEmpty(t *testing.T) {
	input, _ := json.Marshal(map[string]string{"some_other_key": "x"})
	tc := &event.ToolCall{Input: input}
	if got := toolFilePath(tc); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestToolFilePath_BadJSONReturnsEmpty exercises the unmarshal-error
// branch.
func TestToolFilePath_BadJSONReturnsEmpty(t *testing.T) {
	tc := &event.ToolCall{Input: json.RawMessage("not valid json {{{")}
	if got := toolFilePath(tc); got != "" {
		t.Errorf("got %q, want empty for bad JSON", got)
	}
}

// TestTruncateLines_ShorterThanLimit returns text unchanged.
func TestTruncateLines_ShorterThanLimit(t *testing.T) {
	in := "a\nb\nc"
	if got := truncateLines(in, 5); got != in {
		t.Errorf("got %q, want unchanged", got)
	}
}

// TestTruncateLines_AppendsRemainingMarker covers the truncation branch.
func TestTruncateLines_AppendsRemainingMarker(t *testing.T) {
	in := "a\nb\nc\nd\ne"
	got := truncateLines(in, 2)
	if !strings.Contains(got, "+3 lines") {
		t.Errorf("missing remaining-marker: %q", got)
	}
	if !strings.HasPrefix(got, "a\nb\n") {
		t.Errorf("first 2 lines should be preserved: %q", got)
	}
}

// TestTruncateLines_EmptyInputReturnsEmpty
func TestTruncateLines_EmptyInputReturnsEmpty(t *testing.T) {
	if got := truncateLines("", 3); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestBase64Enc covers the OSC 52 clipboard helper.
func TestBase64Enc(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"hello":     "aGVsbG8=",
		"AB\nC":     "QUIKQw==",
		"中":        "5Lit",
	}
	for in, want := range cases {
		if got := base64Enc(in); got != want {
			t.Errorf("base64Enc(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestApp_LastUserMessage_EmptyHistoryReturnsEmpty
func TestApp_LastUserMessage_EmptyHistoryReturnsEmpty(t *testing.T) {
	a := testApp()
	a.messages = nil
	if got := a.lastUserMessage(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestApp_LastUserMessage_FindsLatestUser ignores assistant/info entries
// when scanning back.
func TestApp_LastUserMessage_FindsLatestUser(t *testing.T) {
	a := testApp()
	a.messages = []chatMessage{
		{role: roleUser, content: "first"},
		{role: roleAssistant, content: "reply"},
		{role: roleUser, content: "second"},
		{role: roleInfo, content: "[info] xyz"},
	}
	if got := a.lastUserMessage(); got != "second" {
		t.Errorf("got %q, want 'second'", got)
	}
}

// TestApp_LastAssistantMessage_FindsLatest analogue for assistant
// turns — used by the /copy command.
func TestApp_LastAssistantMessage_FindsLatest(t *testing.T) {
	a := testApp()
	a.messages = []chatMessage{
		{role: roleAssistant, content: "old"},
		{role: roleUser, content: "follow-up"},
		{role: roleAssistant, content: "new"},
	}
	if got := a.lastAssistantMessage(); got != "new" {
		t.Errorf("got %q, want 'new'", got)
	}
}
