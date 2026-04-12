package exec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
)

func TestNewEventAccumulator_NilWhenUnused(t *testing.T) {
	p := &Params{}
	if acc := newEventAccumulator(p); acc != nil {
		t.Error("expected nil accumulator when no print-tree or save-transcript")
	}
}

func TestNewEventAccumulator_ActiveWhenPrintTree(t *testing.T) {
	p := &Params{PrintTree: true}
	if acc := newEventAccumulator(p); acc == nil {
		t.Error("expected active accumulator for --print-tree")
	}
}

func TestNewEventAccumulator_ActiveWhenSaveTranscript(t *testing.T) {
	p := &Params{SaveTranscript: "/tmp/x.jsonl"}
	if acc := newEventAccumulator(p); acc == nil {
		t.Error("expected active accumulator for --save-transcript")
	}
}

func TestEventAccumulator_ObserveNilSafe(t *testing.T) {
	var acc *eventAccumulator
	// Must not panic on nil receiver.
	acc.observe(event.Event{Type: event.TextDelta, Text: "hi"})
}

func TestEventAccumulator_PairsByToolID(t *testing.T) {
	acc := newEventAccumulator(&Params{PrintTree: true})

	// Two parallel tool calls, results arrive interleaved.
	acc.observe(event.Event{
		Type:     event.ToolStart,
		ToolCall: &event.ToolCall{ID: "a", Name: "read", Input: json.RawMessage(`{"file_path":"foo.go"}`)},
	})
	acc.observe(event.Event{
		Type:     event.ToolStart,
		ToolCall: &event.ToolCall{ID: "b", Name: "grep", Input: json.RawMessage(`{"pattern":"TODO"}`)},
	})
	// Result for b arrives FIRST — must still pair correctly.
	acc.observe(event.Event{
		Type:       event.ToolResultEvent,
		ToolCall:   &event.ToolCall{ID: "b", Name: "grep"},
		ToolResult: &event.Result{Output: "3 matches"},
	})
	acc.observe(event.Event{
		Type:       event.ToolResultEvent,
		ToolCall:   &event.ToolCall{ID: "a", Name: "read"},
		ToolResult: &event.Result{Output: "package foo"},
	})

	if len(acc.tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(acc.tools))
	}
	// Tools keep their start order even though results were
	// out-of-order.
	if acc.tools[0].Name != "read" {
		t.Errorf("tools[0]=%q, want read", acc.tools[0].Name)
	}
	if acc.tools[1].Name != "grep" {
		t.Errorf("tools[1]=%q, want grep", acc.tools[1].Name)
	}
	if acc.tools[0].Output != "package foo" {
		t.Errorf("read output: got %q", acc.tools[0].Output)
	}
	if acc.tools[1].Output != "3 matches" {
		t.Errorf("grep output: got %q", acc.tools[1].Output)
	}
}

func TestEventAccumulator_ResultWithoutStartSynthesizes(t *testing.T) {
	// Edge case: if the drain misses ToolStart (backpressure drop),
	// a later ToolResult should synthesize a record instead of
	// dropping the entry entirely.
	acc := newEventAccumulator(&Params{PrintTree: true})
	acc.observe(event.Event{
		Type:       event.ToolResultEvent,
		ToolCall:   &event.ToolCall{ID: "orphan", Name: "write", Input: json.RawMessage(`{"file_path":"new.go"}`)},
		ToolResult: &event.Result{Output: "wrote 10 bytes"},
	})
	if len(acc.tools) != 1 {
		t.Fatalf("expected 1 synthesized tool, got %d", len(acc.tools))
	}
	if acc.tools[0].Output != "wrote 10 bytes" {
		t.Errorf("synthesized tool missing output: %v", acc.tools[0])
	}
}

func TestEventAccumulator_RenderTreeEmpty(t *testing.T) {
	acc := newEventAccumulator(&Params{PrintTree: true})
	var buf bytes.Buffer
	acc.renderTree(&buf)
	if !strings.Contains(buf.String(), "no tools called") {
		t.Errorf("expected empty-state message, got: %q", buf.String())
	}
}

func TestEventAccumulator_RenderTreeNilSafe(t *testing.T) {
	var acc *eventAccumulator
	var buf bytes.Buffer
	acc.renderTree(&buf)
	// Must emit the empty-state line and not panic.
	if !strings.Contains(buf.String(), "no tools called") {
		t.Errorf("nil accumulator should still emit something, got: %q", buf.String())
	}
}

func TestEventAccumulator_RenderTreeWithTools(t *testing.T) {
	acc := newEventAccumulator(&Params{PrintTree: true})
	acc.observe(event.Event{
		Type:     event.ToolStart,
		ToolCall: &event.ToolCall{ID: "1", Name: "read", Input: json.RawMessage(`{"file_path":"foo.go"}`)},
	})
	acc.observe(event.Event{
		Type:       event.ToolResultEvent,
		ToolCall:   &event.ToolCall{ID: "1", Name: "read", Input: json.RawMessage(`{"file_path":"foo.go"}`)},
		ToolResult: &event.Result{Output: "content"},
	})
	acc.observe(event.Event{
		Type:     event.ToolStart,
		ToolCall: &event.ToolCall{ID: "2", Name: "bash", Input: json.RawMessage(`{"command":"go test"}`)},
	})
	acc.observe(event.Event{
		Type:       event.ToolResultEvent,
		ToolCall:   &event.ToolCall{ID: "2", Name: "bash", Input: json.RawMessage(`{"command":"go test"}`)},
		ToolResult: &event.Result{Error: "test failed"},
	})

	var buf bytes.Buffer
	acc.renderTree(&buf)
	out := buf.String()

	// Header
	if !strings.Contains(out, "[tool tree]") {
		t.Error("expected [tool tree] header")
	}
	// Middle item uses ├─, last uses └─
	if !strings.Contains(out, "├─") {
		t.Error("expected middle tree glyph ├─")
	}
	if !strings.Contains(out, "└─") {
		t.Error("expected last tree glyph └─")
	}
	// Success + failure icons
	if !strings.Contains(out, "✓ read") {
		t.Error("expected success icon for read")
	}
	if !strings.Contains(out, "✗ bash") {
		t.Error("expected failure icon for bash")
	}
	// Summaries pulled from input JSON
	if !strings.Contains(out, "foo.go") {
		t.Error("expected file_path summary for read")
	}
	if !strings.Contains(out, "$ go test") {
		t.Error("expected command summary for bash")
	}
	// Error line for the failed tool
	if !strings.Contains(out, "error: test failed") {
		t.Error("expected error detail for failed tool")
	}
}

func TestEventAccumulator_WriteTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	acc := newEventAccumulator(&Params{SaveTranscript: path})

	acc.observe(event.Event{Type: event.TextDelta, Text: "hello "})
	acc.observe(event.Event{Type: event.TextDelta, Text: "world"})
	acc.observe(event.Event{
		Type:     event.ToolStart,
		ToolCall: &event.ToolCall{ID: "1", Name: "read"},
	})
	acc.observe(event.Event{Type: event.Done})

	if err := acc.writeTranscript(path); err != nil {
		t.Fatalf("writeTranscript: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 JSONL lines, got %d", len(lines))
	}
	// Each line must be parseable as JSON
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d not valid JSON: %v", i, err)
		}
		if _, ok := rec["type"]; !ok {
			t.Errorf("line %d missing 'type' field: %q", i, line)
		}
		if _, ok := rec["ts"]; !ok {
			t.Errorf("line %d missing 'ts' field: %q", i, line)
		}
	}
}

func TestEventAccumulator_WriteTranscriptStripsPermissionChannel(t *testing.T) {
	// Ensure the channel field in event.PermReq is NEVER written
	// to the transcript (would cause json: unsupported type: chan).
	acc := newEventAccumulator(&Params{SaveTranscript: "/tmp/x"})
	respCh := make(chan event.PermResponse, 1)
	acc.observe(event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: "bash",
			Pattern:  "rm -rf",
			Response: respCh,
		},
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := acc.writeTranscript(path); err != nil {
		t.Fatalf("writeTranscript with PermissionRequest: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"tool_name":"bash"`) {
		t.Errorf("expected permission info in transcript, got: %q", data)
	}
	if strings.Contains(string(data), "Response") {
		t.Errorf("Response channel should be stripped: %q", data)
	}
}

func TestSummarizeToolInput(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		input  string
		wants  []string
		wantEq string
	}{
		{
			name:  "file_path",
			tool:  "read",
			input: `{"file_path":"foo.go"}`,
			wants: []string{"foo.go"},
		},
		{
			name:  "command",
			tool:  "bash",
			input: `{"command":"ls -la"}`,
			wants: []string{"$ ls -la"},
		},
		{
			name:  "pattern",
			tool:  "grep",
			input: `{"pattern":"TODO"}`,
			wants: []string{"/TODO/"},
		},
		{
			name:  "empty input",
			tool:  "unknown",
			input: "",
			wants: nil,
		},
		{
			name:  "malformed json",
			tool:  "read",
			input: `{bad`,
			// Falls back to raw truncation
			wants: []string{"{bad"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeToolInput(tc.tool, []byte(tc.input))
			for _, w := range tc.wants {
				if !strings.Contains(got, w) {
					t.Errorf("summarizeToolInput(%q, %q): want %q in %q",
						tc.tool, tc.input, w, got)
				}
			}
		})
	}
}

func TestShortDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Microsecond, "<1ms"},
		{50 * time.Millisecond, "50ms"},
		{1500 * time.Millisecond, "1.5s"},
		{65 * time.Second, "1m5s"},
	}
	for _, tc := range cases {
		got := shortDuration(tc.in)
		if got != tc.want {
			t.Errorf("shortDuration(%v): got %q, want %q", tc.in, got, tc.want)
		}
	}
}
