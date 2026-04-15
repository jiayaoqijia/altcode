package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifyShareURL(t *testing.T) {
	secret := []byte("test-secret-key")
	taskID := "task-123"
	expiry := time.Now().Add(time.Hour).Unix()

	sig := SignShareURL(taskID, expiry, secret)
	if sig == "" {
		t.Fatal("SignShareURL returned empty signature")
	}

	err := VerifyShareURL(taskID, sig, expiry, secret)
	if err != nil {
		t.Fatalf("VerifyShareURL failed on valid signature: %v", err)
	}
}

func TestVerifyShareURL_Expired(t *testing.T) {
	secret := []byte("test-secret-key")
	taskID := "task-expired"
	// Set expiry far enough in the past to exceed clock skew grace.
	expiry := time.Now().Add(-5 * time.Minute).Unix()

	sig := SignShareURL(taskID, expiry, secret)
	err := VerifyShareURL(taskID, sig, expiry, secret)
	if err != ErrShareExpired {
		t.Fatalf("expected ErrShareExpired, got %v", err)
	}
}

func TestVerifyShareURL_WrongToken(t *testing.T) {
	secret := []byte("test-secret-key")
	taskID := "task-wrong"
	expiry := time.Now().Add(time.Hour).Unix()

	err := VerifyShareURL(taskID, "deadbeef", expiry, secret)
	if err != ErrShareInvalidHMAC {
		t.Fatalf("expected ErrShareInvalidHMAC, got %v", err)
	}
}

func TestVerifyShareURL_NULSeparator(t *testing.T) {
	secret := []byte("nul-separator-key")
	expiry := time.Now().Add(time.Hour).Unix()

	// "abc" + "123" must produce a different HMAC than "ab" + "c123".
	sig1 := SignShareURL("abc", expiry, secret)
	sig2 := SignShareURL("ab", expiry, secret)

	if sig1 == sig2 {
		t.Fatal("NUL separator failed: abc+123 == ab+c123")
	}

	// Verify the first signature does not validate the second ID.
	err := VerifyShareURL("ab", sig1, expiry, secret)
	if err != ErrShareInvalidHMAC {
		t.Fatalf("expected ErrShareInvalidHMAC for cross-ID, got %v", err)
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "api_key pattern",
			input: "config api_key=sk-abc123def456ghi789",
			want:  "config [REDACTED]",
		},
		{
			name:  "token colon pattern",
			input: "token: ghp_abc123def456",
			want:  "[REDACTED]",
		},
		{
			name:  "openai key",
			input: "key is sk-abcdefghijklmnopqrstuvwxyz",
			want:  "key is [REDACTED]",
		},
		{
			name:  "github pat",
			input: "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij",
			want:  "[REDACTED]",
		},
		{
			name:  "aws access key",
			input: "AKIAIOSFODNN7EXAMPLE",
			want:  "[REDACTED]",
		},
		{
			name:  "pem key header",
			input: "-----BEGIN RSA PRIVATE KEY-----",
			want:  "[REDACTED]",
		},
		{
			name:  "no match passthrough",
			input: "normal log output with no secrets",
			want:  "normal log output with no secrets",
		},
		{
			name:  "password pattern",
			input: "password: hunter2",
			want:  "[REDACTED]",
		},
		{
			name:  "secret with equals",
			input: "SECRET=mysupersecretvalue",
			want:  "[REDACTED]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecrets(tt.input)
			if got != tt.want {
				t.Errorf("RedactSecrets(%q)\n got: %q\nwant: %q",
					tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateShareLink(t *testing.T) {
	secret := []byte("link-secret")
	taskID := "task-link-test"
	ttl := 2 * time.Hour

	link := GenerateShareLink("", taskID, secret, ttl)

	if !strings.HasPrefix(link, "/share/task-link-test.") {
		t.Errorf("unexpected link prefix: %s", link)
	}
	if !strings.Contains(link, "?exp=") {
		t.Errorf("link missing exp query param: %s", link)
	}

	// The HMAC portion should be 64 hex chars (SHA-256).
	parts := strings.SplitN(link, "?", 2)
	pathPart := parts[0]
	dot := strings.LastIndex(pathPart, ".")
	if dot < 0 {
		t.Fatalf("no dot in path: %s", pathPart)
	}
	hmacPart := pathPart[dot+1:]
	if len(hmacPart) != 64 {
		t.Errorf("HMAC hex length = %d, want 64", len(hmacPart))
	}
}

func TestHandleShareView_InvalidFormat(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{SigningKey: []byte("test")}
	h := NewWebHandler(
		tmpl, &mockEventStore{}, sessions, cfg,
		NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /share/{token}", h.HandleShareView)

	// No dot in the token path.
	req := httptest.NewRequest("GET", "/share/nodot?exp=999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestHandleShareView_ExpiredLink(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("expired-secret")
	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{SigningKey: secret}
	h := NewWebHandler(
		tmpl, &mockEventStore{}, sessions, cfg,
		NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /share/{token}", h.HandleShareView)

	// Generate a link with an expiry far in the past.
	taskID := "task-old"
	expiry := time.Now().Add(-10 * time.Minute).Unix()
	sig := SignShareURL(taskID, expiry, secret)
	url := fmt.Sprintf("/share/%s.%s?exp=%d", taskID, sig, expiry)

	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", w.Code)
	}
}

func TestHandleShareView_ValidLink(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("valid-secret")
	now := time.Now()
	store := &mockEventStore{
		task: &TaskView{
			ID:              "task-share",
			RepoOwner:       "owner",
			RepoName:        "repo",
			TaskDescription: "Fix it",
			Status:          "implementing",
			CreatedAt:       now,
		},
		events: []*EventView{
			{
				ID:        1,
				TaskID:    "task-share",
				EventType: "agent_output",
				Data:      "api_key=sk-supersecretkey12345678",
				CreatedAt: now,
			},
		},
	}
	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{SigningKey: secret}
	h := NewWebHandler(
		tmpl, store, sessions, cfg,
		NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /share/{token}", h.HandleShareView)

	taskID := "task-share"
	expiry := time.Now().Add(time.Hour).Unix()
	sig := SignShareURL(taskID, expiry, secret)
	url := fmt.Sprintf("/share/%s.%s?exp=%d", taskID, sig, expiry)

	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Verify page content renders.
	checks := []string{
		"owner/repo",
		"Fix it",
		"implementing",
		"read-only",
		"activity-feed",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("share page missing %q", want)
		}
	}

	// Verify secrets are redacted.
	if strings.Contains(body, "sk-supersecretkey") {
		t.Error("share page should redact secret keys")
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Error("share page should contain [REDACTED] markers")
	}

	// Verify no nav bar.
	if strings.Contains(body, "<nav") {
		t.Error("share page should not render nav bar")
	}

	// Verify no steering form or stop button.
	if strings.Contains(body, "Steer the agent") {
		t.Error("share page should not show steering form")
	}
	if strings.Contains(body, "Cancel") {
		t.Error("share page should not show cancel button")
	}
}
