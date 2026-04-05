package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// CallbackResult carries the authorization code returned by the OAuth
// authorization server via the local callback URL.
type CallbackResult struct {
	Code  string
	State string
	Err   error
}

// RunCallbackServer starts an HTTP listener on 127.0.0.1:1455, waits for
// the /auth/callback hit, and returns the code + state. Times out after
// the given duration.
func RunCallbackServer(ctx context.Context, expectedState string, timeout time.Duration) (*CallbackResult, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", DefaultPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}

	resultCh := make(chan *CallbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			desc := q.Get("error_description")
			fmt.Fprintf(w, "<html><body><h1>Login failed</h1><p>%s: %s</p></body></html>", errParam, desc)
			resultCh <- &CallbackResult{Err: fmt.Errorf("%s: %s", errParam, desc)}
			return
		}
		code := q.Get("code")
		state := q.Get("state")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			resultCh <- &CallbackResult{Err: fmt.Errorf("missing code in callback")}
			return
		}
		if state != expectedState {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			resultCh <- &CallbackResult{Err: fmt.Errorf("state mismatch")}
			return
		}
		fmt.Fprint(w, `<html><body style="font-family:sans-serif;text-align:center;padding:40px">
<h1>altcode login successful</h1>
<p>You can close this tab and return to the terminal.</p>
</body></html>`)
		resultCh <- &CallbackResult{Code: code, State: state}
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(lis) }()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	select {
	case r := <-resultCh:
		if r.Err != nil {
			return nil, r.Err
		}
		return r, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("login timed out after %s", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
