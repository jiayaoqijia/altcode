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
// Matches OpenHarness's HttpHookDefinition pattern.
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
		return &Result{Decision: "allow"}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, entry.URL, bytes.NewReader(body))
	if err != nil {
		return &Result{Decision: "allow"}, fmt.Errorf("http hook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Network errors default to allow (fail-open)
		return &Result{Decision: "allow"}, nil
	}
	defer resp.Body.Close()

	// Non-2xx = allow (fail-open, like command hooks with non-zero exit)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Result{Decision: "allow"}, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Result{Decision: "allow"}, nil
	}

	var result Result
	if err := json.Unmarshal(respBody, &result); err != nil {
		return &Result{Decision: "allow"}, nil
	}
	return &result, nil
}
