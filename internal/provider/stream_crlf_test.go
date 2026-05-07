package provider

import (
	"io"
	"strings"
	"testing"
)

// TestSSEDecoder_CRLF verifies SSEDecoder handles \r\n-terminated
// streams, which is what the SSE spec requires and what most real
// servers emit (Anthropic, OpenAI both send \r\n in practice).
// Codex round-J caught that the old decoder buffered every event
// until EOF because it never saw an empty separator line (each
// blank arrived as "\r") and left a trailing \r in the JSON payload.
func TestSSEDecoder_CRLF(t *testing.T) {
	body := "event: message\r\n" +
		"data: {\"hello\":\"world\"}\r\n" +
		"\r\n" +
		"event: done\r\n" +
		"data: {}\r\n" +
		"\r\n"
	d := NewSSEDecoder(strings.NewReader(body))

	ev, data, err := d.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if ev != "message" {
		t.Errorf("event = %q, want %q", ev, "message")
	}
	if data != `{"hello":"world"}` {
		t.Errorf("data = %q, want %q (no trailing \\r)",
			data, `{"hello":"world"}`)
	}

	ev, _, err = d.Next()
	if err != nil {
		t.Fatalf("second Next: %v", err)
	}
	if ev != "done" {
		t.Errorf("second event = %q, want done", ev)
	}

	if _, _, err := d.Next(); err != io.EOF {
		t.Errorf("third Next err = %v, want EOF", err)
	}
}

// TestSSEDecoder_LF keeps working — regression guard so the CRLF fix
// doesn't break LF-only streams (what test fixtures typically use).
func TestSSEDecoder_LF(t *testing.T) {
	body := "event: message\n" +
		"data: {\"ok\":true}\n" +
		"\n"
	d := NewSSEDecoder(strings.NewReader(body))
	ev, data, err := d.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if ev != "message" || data != `{"ok":true}` {
		t.Errorf("got ev=%q data=%q", ev, data)
	}
}
