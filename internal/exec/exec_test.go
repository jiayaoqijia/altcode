package exec_test

import (
	"bytes"
	"context"
	"encoding/json"
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
