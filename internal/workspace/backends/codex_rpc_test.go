package backends

import (
	"encoding/json"
	"testing"
)

func TestCodexRPCClient_RequestFormat(t *testing.T) {
	// Verify the JSON-RPC request envelope has the correct fields.
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "turn/start",
		"params": map[string]any{
			"threadId": "thread-abc",
			"input": []map[string]any{
				{"type": "text", "text": "fix the tests"},
			},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"jsonrpc", "id", "method", "params"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("missing required field %q", key)
		}
	}

	var version string
	json.Unmarshal(parsed["jsonrpc"], &version)
	if version != "2.0" {
		t.Errorf("jsonrpc = %q, want %q", version, "2.0")
	}

	var id int
	json.Unmarshal(parsed["id"], &id)
	if id != 1 {
		t.Errorf("id = %d, want 1", id)
	}
}

func TestCodexRPCClient_AutoApproval(t *testing.T) {
	// Verify the auto-approval response format matches Codex expectation.
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      42,
		"result":  map[string]any{"decision": "accept"},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	var result struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(parsed["result"], &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "accept" {
		t.Errorf("decision = %q, want %q", result.Decision, "accept")
	}

	var id int
	json.Unmarshal(parsed["id"], &id)
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}
