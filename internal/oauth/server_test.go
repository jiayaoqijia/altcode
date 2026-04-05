package oauth

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestCallbackServer_SuccessfulCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh := make(chan *CallbackResult, 1)
	errCh := make(chan error, 1)
	go func() {
		r, err := RunCallbackServer(ctx, "expected-state", 5*time.Second)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- r
	}()

	// Give the server a moment to bind
	time.Sleep(150 * time.Millisecond)

	// Hit the callback
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:1455/auth/callback?code=testcode&state=expected-state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	select {
	case r := <-resultCh:
		if r.Code != "testcode" {
			t.Errorf("code = %q, want testcode", r.Code)
		}
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestCallbackServer_StateMismatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := RunCallbackServer(ctx, "expected", 5*time.Second)
		errCh <- err
	}()

	time.Sleep(150 * time.Millisecond)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, _ := client.Get("http://127.0.0.1:1455/auth/callback?code=x&state=wrong")
	if resp != nil {
		resp.Body.Close()
	}

	select {
	case err := <-errCh:
		if err == nil || !contains(err.Error(), "state") {
			t.Errorf("expected state mismatch error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
