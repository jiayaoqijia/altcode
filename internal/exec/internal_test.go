package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/permission"
)

// --- Unit tests for unexported helpers in exec.go --------------------
// These live in the `exec` package (not `exec_test`) so they can
// reach unexported functions like parsePatchPaths, drainJSONFinal,
// drainJSON, drainDiff, and extractFilePaths.

func TestParsePatchPaths(t *testing.T) {
	cases := []struct {
		name  string
		patch string
		want  []string
	}{
		{
			name:  "single file",
			patch: "--- a/foo.go\n+++ b/foo.go\n@@ -1,1 +1,1 @@\n-x\n+y\n",
			want:  []string{"foo.go"},
		},
		{
			name: "multiple files",
			patch: "--- a/foo.go\n+++ b/foo.go\n@@\n-x\n+y\n" +
				"--- a/bar/baz.go\n+++ b/bar/baz.go\n@@\n-a\n+b\n",
			want: []string{"foo.go", "bar/baz.go"},
		},
		{
			name:  "deleted file (skipped)",
			patch: "--- a/old.go\n+++ /dev/null\n",
			want:  nil,
		},
		{
			name:  "with timestamp suffix",
			patch: "--- a/foo.go\t2024-01-01 00:00:00\n+++ b/foo.go\t2024-01-02 00:00:00\n",
			want:  []string{"foo.go"},
		},
		{
			name:  "no b/ prefix (custom form)",
			patch: "+++ raw/path.go\n",
			want:  []string{"raw/path.go"},
		},
		{
			name:  "empty patch",
			patch: "",
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePatchPaths(tc.patch)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractFilePaths(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		input    string
		wantFile string
		wantLen  int
	}{
		{"write", "write", `{"file_path":"a.go","content":"x"}`, "a.go", 1},
		{"edit", "edit", `{"file_path":"b.go","old_string":"x","new_string":"y"}`, "b.go", 1},
		{"apply_patch single", "apply_patch", `{"patch":"--- a/c.go\n+++ b/c.go\n"}`, "c.go", 1},
		{"unknown tool", "bash", `{"command":"ls"}`, "", 0},
		{"malformed json", "write", `{bad`, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFilePaths(tc.tool, json.RawMessage(tc.input))
			if len(got) != tc.wantLen {
				t.Fatalf("len %d, want %d (got %v)", len(got), tc.wantLen, got)
			}
			if tc.wantLen > 0 && got[0] != tc.wantFile {
				t.Errorf("got %q, want %q", got[0], tc.wantFile)
			}
		})
	}
}

// TestDrainJSON_PermissionAfterEPIPE reproduces the BLOCKER Codex
// caught: if stdout breaks first and a permission request arrives
// afterward, the engine's respCh must still receive a deny so
// askPermission() doesn't block forever.
func TestDrainJSON_PermissionAfterEPIPE(t *testing.T) {
	ch := make(chan event.Event)
	respCh := make(chan event.PermResponse, 1)

	// Send: text event first (succeeds), then broken-pipe writer,
	// then permission event (must still be answered).
	go func() {
		defer close(ch)
		ch <- event.Event{Type: event.TextDelta, Text: "hello"}
		ch <- event.Event{
			Type: event.PermissionRequest,
			Permission: &event.PermReq{
				ToolName: "bash",
				Pattern:  "rm -rf /",
				Response: respCh,
			},
		}
	}()

	// brokenWriter always returns EPIPE after 10 bytes.
	bw := &brokenWriter{limit: 10}
	_ = drainJSON(context.Background(), ch, bw, &Params{})

	// Engine-side: the deny must have arrived on respCh.
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny, got %v", resp.Action)
		}
	default:
		t.Fatal("permission response never sent — engine would deadlock")
	}
}

// TestDrainJSON_PermissionFieldOmitsChannel verifies that json.Encode
// of a PermissionRequest event doesn't fail because of the
// `chan PermResponse` field (it's tagged `json:"-"`).
func TestDrainJSON_PermissionFieldOmitsChannel(t *testing.T) {
	ch := make(chan event.Event, 2)
	respCh := make(chan event.PermResponse, 1)
	ch <- event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Pattern:  "ls",
			Response: respCh,
		},
	}
	close(ch)

	var buf bytes.Buffer
	if err := drainJSON(context.Background(), ch, &buf, &Params{}); err != nil {
		t.Fatalf("drainJSON: %v", err)
	}

	// Must be valid JSONL — one line, parseable.
	line := strings.TrimSpace(buf.String())
	var ev map[string]any
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatalf("not valid JSON: %v\nline: %q", err, line)
	}
	if ev["type"] != "permission_request" {
		t.Errorf("expected type=permission_request, got %v", ev["type"])
	}

	// And the permission must have been answered.
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny, got %v", resp.Action)
		}
	default:
		t.Fatal("permission never answered")
	}
}

// TestDrainJSONFinal_ErrorsAccumulate verifies multiple ErrorEvents
// aren't clobbered — previous single-slot behavior lost all but
// the last error.
func TestDrainJSONFinal_ErrorsAccumulate(t *testing.T) {
	ch := make(chan event.Event, 4)
	ch <- event.Event{Type: event.ErrorEvent, Error: "first"}
	ch <- event.Event{Type: event.ErrorEvent, Error: "second"}
	ch <- event.Event{Type: event.ErrorEvent, Error: "third"}
	close(ch)

	var buf bytes.Buffer
	err := drainJSONFinal(context.Background(), ch, &buf, &Params{})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in: %q", want, msg)
		}
	}

	// JSON output should list all three in the "errors" array.
	var result struct {
		Errors []string `json:"errors"`
	}
	if jerr := json.Unmarshal(buf.Bytes(), &result); jerr != nil {
		t.Fatalf("parse: %v", jerr)
	}
	if len(result.Errors) != 3 {
		t.Errorf("expected 3 errors in JSON, got %d: %v", len(result.Errors), result.Errors)
	}
}

// TestDrainJSONFinal_ReconcilesToolInput verifies that Input arriving
// on ToolResultEvent (as engine.go:856 re-sends) updates the tool
// record. Previously only ToolStart input was recorded.
func TestDrainJSONFinal_ReconcilesToolInput(t *testing.T) {
	ch := make(chan event.Event, 4)
	// ToolStart: no input yet (provider quirk)
	ch <- event.Event{
		Type: event.ToolStart,
		ToolCall: &event.ToolCall{
			ID:    "tc1",
			Name:  "write",
			Input: nil,
		},
	}
	// ToolResultEvent: engine re-sends the full Input
	ch <- event.Event{
		Type: event.ToolResultEvent,
		ToolCall: &event.ToolCall{
			ID:    "tc1",
			Name:  "write",
			Input: json.RawMessage(`{"file_path":"foo.go"}`),
		},
		ToolResult: &event.Result{Output: "Wrote 10 bytes"},
	}
	close(ch)

	var buf bytes.Buffer
	if err := drainJSONFinal(context.Background(), ch, &buf, &Params{}); err != nil {
		t.Fatalf("drainJSONFinal: %v", err)
	}

	var result struct {
		Tools []struct {
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if !strings.Contains(string(result.Tools[0].Input), "foo.go") {
		t.Errorf("expected input to contain foo.go, got %s", result.Tools[0].Input)
	}
}

// TestDrainJSONFinal_RecordsPermissionAutoDenies verifies Phase 1's
// auto-deny path surfaces in the final JSON so scripts can detect
// blocked work.
func TestDrainJSONFinal_RecordsPermissionAutoDenies(t *testing.T) {
	ch := make(chan event.Event, 2)
	respCh := make(chan event.PermResponse, 1)
	ch <- event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Pattern:  "rm -rf /",
			Response: respCh,
		},
	}
	close(ch)

	var buf bytes.Buffer
	err := drainJSONFinal(context.Background(), ch, &buf, &Params{})
	if err != nil {
		t.Fatalf("drainJSONFinal: %v", err)
	}

	// Permission must have been auto-denied
	select {
	case resp := <-respCh:
		if resp.Action != event.Deny {
			t.Errorf("expected Deny, got %v", resp.Action)
		}
	default:
		t.Fatal("permission never answered")
	}

	// And the denial must appear in the JSON record
	var result struct {
		Permissions []struct {
			Tool   string `json:"tool"`
			Action string `json:"action"`
		} `json:"permissions"`
	}
	if jerr := json.Unmarshal(buf.Bytes(), &result); jerr != nil {
		t.Fatalf("parse: %v", jerr)
	}
	if len(result.Permissions) != 1 {
		t.Fatalf("expected 1 permission record, got %d", len(result.Permissions))
	}
	if result.Permissions[0].Tool != "bash" {
		t.Errorf("expected tool=bash, got %q", result.Permissions[0].Tool)
	}
	if result.Permissions[0].Action != "auto-deny" {
		t.Errorf("expected action=auto-deny, got %q", result.Permissions[0].Action)
	}
}

// --- Phase 2: permission overrides ----------------------------------

func TestParseToolRuleSpec(t *testing.T) {
	cases := []struct {
		spec        string
		wantName    string
		wantPattern string
		wantErr     bool
	}{
		{"bash", "bash", "*", false},
		{"bash:echo hi", "bash", "echo hi", false},
		{"bash:", "bash", "*", false}, // empty pattern → wildcard
		{"  edit  :  foo  ", "edit", "foo", false},
		{"", "", "", true},
		{":pattern-only", "", "", true}, // empty name → error
		{"   ", "", "", true},           // whitespace only → error
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			n, p, err := parseToolRuleSpec(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tc.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tc.wantName || p != tc.wantPattern {
				t.Errorf("got (%q, %q), want (%q, %q)",
					n, p, tc.wantName, tc.wantPattern)
			}
		})
	}
}

func TestParsePermissionMode(t *testing.T) {
	cases := []struct {
		in      string
		wantSet bool
		wantErr bool
	}{
		{"", false, false},
		{"plan", true, false},
		{"auto", true, false},
		{"default", true, false},
		{"bypass", true, false},
		{"yolo", false, true},
		{"PLAN", false, true}, // case-sensitive
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			_, set, err := parsePermissionMode(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if set != tc.wantSet {
				t.Errorf("set=%v, want %v", set, tc.wantSet)
			}
		})
	}
}

func TestApplyPermissionOverrides_SetsMode(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	p := &Params{PermissionMode: "plan"}
	got, err := ApplyPermissionOverrides(eval, p, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != eval {
		t.Fatal("expected same evaluator back")
	}
	// Check that write tool is now denied (plan mode = read-only)
	action := got.CheckWithReadOnly("write", "file_path:foo.go", false)
	if action != permission.ActionDeny {
		t.Errorf("plan mode should deny write, got %v", action)
	}
	// Read tools still allowed
	action = got.CheckWithReadOnly("read", "file_path:foo.go", true)
	if action != permission.ActionAllow {
		t.Errorf("plan mode should allow read, got %v", action)
	}
}

func TestApplyPermissionOverrides_DryRunAliasesPlan(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	p := &Params{DryRun: true}
	got, err := ApplyPermissionOverrides(eval, p, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	action := got.CheckWithReadOnly("write", "foo", false)
	if action != permission.ActionDeny {
		t.Errorf("--dry-run should alias to plan (deny writes), got %v", action)
	}
}

func TestApplyPermissionOverrides_ExplicitModeWinsOverDryRun(t *testing.T) {
	// --permission-mode bypass + --dry-run → bypass wins (explicit mode)
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	p := &Params{PermissionMode: "bypass", DryRun: true}
	got, err := ApplyPermissionOverrides(eval, p, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	action := got.CheckWithReadOnly("write", "foo", false)
	if action != permission.ActionAllow {
		t.Errorf("bypass should win over dry-run, got %v", action)
	}
}

func TestApplyPermissionOverrides_AllowDenySessionRules(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeAuto, "", nil)
	p := &Params{
		AllowTools: []string{"bash:git *", "write"},
		DenyTools:  []string{"bash:rm -rf *"},
	}
	got, err := ApplyPermissionOverrides(eval, p, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// Session allow
	if action := got.CheckWithReadOnly("bash", "git status", false); action != permission.ActionAllow {
		t.Errorf("bash git * should be allowed, got %v", action)
	}
	// Session deny (deny beats allow)
	if action := got.CheckWithReadOnly("bash", "rm -rf /", false); action != permission.ActionDeny {
		t.Errorf("bash rm should be denied, got %v", action)
	}
	// Unmatched in auto mode → deny
	if action := got.CheckWithReadOnly("bash", "curl http://x", false); action != permission.ActionDeny {
		t.Errorf("unmatched in auto mode should be denied, got %v", action)
	}
}

func TestApplyPermissionOverrides_NilEvalBuildsOneWhenNeeded(t *testing.T) {
	// No config → nil evaluator. Allow rule should still work.
	p := &Params{AllowTools: []string{"bash:echo *"}}
	got, err := ApplyPermissionOverrides(nil, p, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got == nil {
		t.Fatal("expected evaluator to be built")
	}
	if action := got.CheckWithReadOnly("bash", "echo hi", false); action != permission.ActionAllow {
		t.Errorf("session rule should take effect, got %v", action)
	}
}

func TestApplyPermissionOverrides_NilEvalNoOp(t *testing.T) {
	// No config, no overrides → nil out
	p := &Params{}
	got, err := ApplyPermissionOverrides(nil, p, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil evaluator, got %v", got)
	}
}

// TestApplyPermissionOverrides_ConfigDenyShadowsSessionAllow
// documents the current behavior: a config-level deny rule wins
// over a CLI --allow-tool. See the LIMITATION comment on
// ApplyPermissionOverrides. If this test fails because someone
// changed permission.Check's rule-iteration order to let session
// allows beat global denies, update the doc comment too.
func TestApplyPermissionOverrides_ConfigDenyShadowsSessionAllow(t *testing.T) {
	globalDeny := []permission.Rule{
		{Tool: "bash", Pattern: "*", Action: permission.ActionDeny, Source: "config"},
	}
	eval := permission.NewEvaluator(permission.ModeDefault, "", globalDeny)
	p := &Params{AllowTools: []string{"bash:git status"}}
	got, err := ApplyPermissionOverrides(eval, p, "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// Config deny wins — this is the current semantics.
	action := got.CheckWithReadOnly("bash", "git status", false)
	if action != permission.ActionDeny {
		t.Errorf("expected Deny (config wins), got %v — permission "+
			"evaluator rule order changed? update doc comment on "+
			"ApplyPermissionOverrides", action)
	}
}

// TestApplyPermissionOverrides_MultiColonPattern verifies that
// parseToolRuleSpec splits on the FIRST colon only, preserving
// multi-colon patterns in the tail. Referenced by the doc comment
// on ApplyPermissionOverrides so future readers don't trip.
func TestApplyPermissionOverrides_MultiColonPattern(t *testing.T) {
	name, pattern, err := parseToolRuleSpec("bash:echo hi:bye")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if name != "bash" || pattern != "echo hi:bye" {
		t.Errorf("got (%q, %q), want (bash, echo hi:bye)", name, pattern)
	}
}

func TestApplyPermissionOverrides_BadRuleReturnsUsageError(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	p := &Params{AllowTools: []string{":no-name"}}
	_, err := ApplyPermissionOverrides(eval, p, "")
	if err == nil {
		t.Fatal("expected error")
	}
	var uerr *UsageError
	if !errors.As(err, &uerr) {
		t.Errorf("expected UsageError, got %T", err)
	}
}

// --- test helpers ----------------------------------------------------

// brokenWriter returns io.ErrShortWrite (and eventually EPIPE-ish
// errors) after `limit` bytes. Used to verify drainJSON keeps
// answering permission requests even after stdout breaks.
type brokenWriter struct {
	mu      sync.Mutex
	written int
	limit   int
}

func (b *brokenWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.written >= b.limit {
		return 0, errors.New("broken pipe")
	}
	n := len(p)
	if b.written+n > b.limit {
		n = b.limit - b.written
	}
	b.written += n
	if n < len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}
