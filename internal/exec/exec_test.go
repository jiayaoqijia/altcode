package exec_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/store"
)

func sse(eventType, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)
}

func textSSE(text string) string {
	return sse("content_block_start", `{"index":0,"content_block":{"type":"text","text":""}}`) +
		sse("content_block_delta", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)) +
		sse("content_block_stop", `{}`) +
		sse("message_stop", `{}`)
}

func mockServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
}

func cfgWith(srv *httptest.Server) *config.Config {
	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{
		APIKey: "test", BaseURL: srv.URL,
	}
	return cfg
}

func TestExec_TextMode(t *testing.T) {
	srv := mockServer(textSSE("Hello from exec!"))
	defer srv.Close()

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgWith(srv)},
		Prompt:       "hi",
		Writer:       &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Hello from exec!") {
		t.Errorf("Expected 'Hello from exec!' in output, got: %q", output)
	}
}

func TestExec_JSONMode(t *testing.T) {
	srv := mockServer(textSSE("JSON test"))
	defer srv.Close()

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgWith(srv)},
		Prompt:       "hi",
		JSON:         true,
		Writer:       &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should be JSONL — each line is a valid JSON event
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("Expected JSONL output")
	}

	var foundTextDelta, foundDone bool
	for _, line := range lines {
		var ev event.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("Invalid JSON line: %q: %v", line, err)
			continue
		}
		if ev.Type == event.TextDelta {
			foundTextDelta = true
		}
		if ev.Type == event.Done {
			foundDone = true
		}
	}
	if !foundTextDelta {
		t.Error("Expected TextDelta event in JSONL")
	}
	if !foundDone {
		t.Error("Expected Done event in JSONL")
	}
}

func TestExec_ErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"fail"}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgWith(srv)},
		Prompt:       "hi",
		Writer:       &buf,
	})
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

func TestExec_WithSessionPersistence(t *testing.T) {
	srv := mockServer(textSSE("Persisted!"))
	defer srv.Close()

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sess, _ := db.CreateSession("test", "exec-test", "claude-test")

	var buf bytes.Buffer
	err = exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{
			Config:    cfgWith(srv),
			Store:     db,
			SessionID: sess.ID,
		},
		Prompt: "hello",
		Writer: &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs, _ := db.ListMessages(sess.ID)
	if len(msgs) < 2 {
		t.Errorf("Expected persisted messages, got %d", len(msgs))
	}
}

func TestExec_DefaultsToStdout(t *testing.T) {
	// Just verify it doesn't panic when Writer is nil
	srv := mockServer(textSSE("stdout test"))
	defer srv.Close()

	// Can't easily capture os.Stdout in test, just verify no error
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgWith(srv)},
		Prompt:       "hi",
		// Writer: nil → defaults to os.Stdout
	})
	if err != nil {
		t.Fatalf("Run with nil Writer: %v", err)
	}
}

// --- Phase 1 tests ---------------------------------------------------
// These cover the new Params extension: Validate(), output-format
// dispatch, --quiet, --verbose, --print-cost, --output-format json.

func TestValidate_InvalidOutputFormat(t *testing.T) {
	p := exec.Params{OutputFormat: "yaml"}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for invalid output format")
	}
	var uerr *exec.UsageError
	if !errors.As(err, &uerr) {
		t.Errorf("expected *UsageError, got %T", err)
	}
	if uerr.ExitCode != 64 {
		t.Errorf("expected exit 64, got %d", uerr.ExitCode)
	}
	if !strings.Contains(uerr.Msg, "invalid --output-format") {
		t.Errorf("expected 'invalid --output-format' in msg, got: %q", uerr.Msg)
	}
}

func TestValidate_ValidFormats(t *testing.T) {
	cases := []string{"", "text", "json", "stream-json", "diff"}
	for _, f := range cases {
		p := exec.Params{OutputFormat: f}
		if err := p.Validate(); err != nil {
			t.Errorf("format %q: unexpected err %v", f, err)
		}
	}
}

func TestValidate_QuietVerboseMutex(t *testing.T) {
	p := exec.Params{Quiet: true, Verbose: true}
	if err := p.Validate(); err == nil {
		t.Fatal("expected mutual-exclusion error")
	}
}

func TestValidate_QuietShowSystemMutex(t *testing.T) {
	p := exec.Params{Quiet: true, ShowSystem: true}
	if err := p.Validate(); err == nil {
		t.Fatal("expected mutual-exclusion error")
	}
}

func TestValidate_DiffVerboseMutex(t *testing.T) {
	p := exec.Params{OutputFormat: "diff", Verbose: true}
	if err := p.Validate(); err == nil {
		t.Fatal("expected mutual-exclusion error")
	}
}

func TestValidate_JSONAndOutputFormatConflict(t *testing.T) {
	// --json + --output-format json (mismatch) → error
	p := exec.Params{JSON: true, OutputFormat: "json"}
	if err := p.Validate(); err == nil {
		t.Fatal("expected conflict error when --json disagrees with --output-format json")
	}
	// --json + --output-format stream-json (agreement) → ok
	p = exec.Params{JSON: true, OutputFormat: "stream-json"}
	if err := p.Validate(); err != nil {
		t.Errorf("--json + --output-format stream-json should be ok, got %v", err)
	}
}

func TestExec_OutputFormatJSON_FinalObject(t *testing.T) {
	srv := mockServer(textSSE("JSON final test"))
	defer srv.Close()

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgWith(srv)},
		Prompt:       "hi",
		OutputFormat: "json",
		Writer:       &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should be a single JSON object, not JSONL
	output := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(output, "{") {
		t.Errorf("expected single JSON object, got: %q", output)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("parse final JSON: %v", err)
	}
	if result.Text != "JSON final test" {
		t.Errorf("expected text %q, got %q", "JSON final test", result.Text)
	}
}

func TestExec_QuietSuppressesText(t *testing.T) {
	srv := mockServer(textSSE("should be hidden"))
	defer srv.Close()

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgWith(srv)},
		Prompt:       "hi",
		Quiet:        true,
		Writer:       &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(buf.String(), "should be hidden") {
		t.Errorf("--quiet should suppress TextDelta, got: %q", buf.String())
	}
}

// Regression tests for Phase 1 review findings.

// extractFilePaths is an unexported helper; test it through the exec
// package internally. This file lives in `exec_test` so we can't
// reach it directly — a sibling internal_test.go file covers that.
// See internal/exec/internal_test.go for the unit coverage.

// TestExec_JSONFinal_ReconcilesInputFromResult verifies that when
// tool Input is only fully populated on ToolResultEvent (engine
// re-sends the original Input there), drainJSONFinal picks it up.
// The previous implementation took Input only from ToolStart.
func TestExec_JSONFinal_ReconcilesInputFromResult(t *testing.T) {
	// Can't easily mock a tool-using turn through the real provider
	// in this test harness; this coverage lives at the integration
	// level. The unit check is in internal_test.go.
	t.Skip("covered by internal_test.go drainJSONFinal test")
}

func TestExec_JSONFieldAliasesStreamJSON(t *testing.T) {
	// Verify the legacy --json flag still produces stream-json
	// output when OutputFormat is empty.
	srv := mockServer(textSSE("legacy"))
	defer srv.Close()

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgWith(srv)},
		Prompt:       "hi",
		JSON:         true,
		Writer:       &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// stream-json is JSONL — expect multiple lines each of which is valid JSON
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Errorf("expected multi-line JSONL, got %d lines", len(lines))
	}
	for _, line := range lines {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("line %q not valid JSON: %v", line, err)
		}
	}
}
