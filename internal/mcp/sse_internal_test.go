package mcp

import (
	"strings"
	"testing"
)

// TestReadSSEResponse_MultilineData verifies the parser concatenates
// multi-line `data:` fields before dispatching — SSE allows a single
// event to span multiple data lines, joined with newlines. The old
// one-line-at-a-time parser silently dropped these.
func TestReadSSEResponse_MultilineData(t *testing.T) {
	body := strings.NewReader(
		"event: message\n" +
			"data: {\"jsonrpc\":\"2.0\",\n" +
			"data:  \"id\":42,\n" +
			"data:  \"result\":{\"ok\":true}}\n" +
			"\n",
	)
	result, err := readSSEResponse(body, 42)
	if err != nil {
		t.Fatalf("readSSEResponse: %v", err)
	}
	if !strings.Contains(string(result), "\"ok\":true") {
		t.Errorf("result = %s, want ok:true", result)
	}
}

// TestReadSSEResponse_LargePayload verifies the Buffer override —
// the default 64 KiB Scanner buffer truncates large JSON-RPC
// responses (common with tools/list or resources/list). A 256 KiB
// payload must round-trip cleanly now.
func TestReadSSEResponse_LargePayload(t *testing.T) {
	big := strings.Repeat("x", 256*1024)
	body := strings.NewReader(
		"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"data\":\"" +
			big + "\"}}\n\n",
	)
	result, err := readSSEResponse(body, 7)
	if err != nil {
		t.Fatalf("readSSEResponse on 256KiB: %v", err)
	}
	if len(result) < len(big) {
		t.Errorf("result truncated: got %d bytes, want >=%d",
			len(result), len(big))
	}
}

// TestReadSSEResponse_DoesNotMatchSmallerEvent ensures we keep
// scanning past unrelated events until we find the matching id.
func TestReadSSEResponse_DoesNotMatchSmallerEvent(t *testing.T) {
	body := strings.NewReader(
		`data: {"jsonrpc":"2.0","id":1,"result":{"other":true}}` + "\n\n" +
			`data: {"jsonrpc":"2.0","id":2,"result":{"me":true}}` + "\n\n",
	)
	result, err := readSSEResponse(body, 2)
	if err != nil {
		t.Fatalf("readSSEResponse: %v", err)
	}
	if !strings.Contains(string(result), "\"me\":true") {
		t.Errorf("matched wrong event: %s", result)
	}
}
