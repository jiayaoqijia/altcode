package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// runHTTPHook sends the hook input as JSON POST to the configured URL.
// The response body is parsed as a Result (decision + message).
//
// Fails CLOSED on network errors and non-2xx responses to match the
// command-hook policy (exec.go fails closed on timeout). An HTTP hook
// is typically a security gate, and a misconfigured webhook or network
// blip should never silently allow a dangerous action.
func runHTTPHook(ctx context.Context, entry EntryConfig, input Input) (*Result, error) {
	if entry.URL == "" {
		return &Result{Decision: "allow"}, nil
	}

	timeout := time.Duration(entry.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(input)
	if err != nil {
		return &Result{
			Decision: "deny",
			Message:  fmt.Sprintf("hook input marshal failed: %v", err),
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, entry.URL, bytes.NewReader(body))
	if err != nil {
		return &Result{
			Decision: "deny",
			Message:  fmt.Sprintf("hook request build failed: %v", err),
		}, fmt.Errorf("http hook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Network error — fail closed. Don't let a network blip
		// bypass a webhook security gate.
		return &Result{
			Decision: "deny",
			Message:  fmt.Sprintf("hook unreachable: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Non-2xx — fail closed for the same reason. A 5xx from a
		// validator should NOT be interpreted as approval.
		return &Result{
			Decision: "deny",
			Message:  fmt.Sprintf("hook returned HTTP %d", resp.StatusCode),
		}, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Result{
			Decision: "deny",
			Message:  fmt.Sprintf("hook response read failed: %v", err),
		}, nil
	}

	// Empty body is treated as a successful allow — the webhook
	// returned 2xx with nothing else to say.
	if len(bytes.TrimSpace(respBody)) == 0 {
		return &Result{Decision: "allow"}, nil
	}

	var result Result
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Malformed JSON from a security gate is suspicious — fail
		// closed and surface the body so the user can debug.
		return &Result{
			Decision: "deny",
			Message:  fmt.Sprintf("hook response not valid JSON: %s", string(respBody)),
		}, nil
	}
	return &result, nil
}
