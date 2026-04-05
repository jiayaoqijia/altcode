package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestExchangeCode_Roundtrip verifies ExchangeCode parses the token
// response correctly when pointed at a mock endpoint.
func TestExchangeCode_Roundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		body, _ := readForm(r)
		if body.Get("grant_type") != "authorization_code" {
			http.Error(w, "wrong grant", 400)
			return
		}
		if body.Get("code") != "test-code" {
			http.Error(w, "wrong code", 400)
			return
		}
		if body.Get("code_verifier") == "" {
			http.Error(w, "missing verifier", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id_token":      "idt-1",
			"access_token":  "at-1",
			"refresh_token": "rt-1",
		})
	}))
	defer srv.Close()

	// Point DefaultIssuer at the mock for this test
	origIssuer := issuerOverride
	issuerOverride = srv.URL
	defer func() { issuerOverride = origIssuer }()

	td, err := ExchangeCode(context.Background(), "test-code", "verifier-123")
	if err != nil {
		t.Fatal(err)
	}
	if td.AccessToken != "at-1" || td.RefreshToken != "rt-1" || td.IDToken != "idt-1" {
		t.Errorf("unexpected tokens: %+v", td)
	}
}

func TestRefreshToken_PreservesOldRefreshIfMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Note: no refresh_token in response (common — servers don't rotate every time)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id_token":     "idt-2",
			"access_token": "at-2",
		})
	}))
	defer srv.Close()

	origIssuer := issuerOverride
	issuerOverride = srv.URL
	defer func() { issuerOverride = origIssuer }()

	td, err := RefreshToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if td.RefreshToken != "old-refresh" {
		t.Errorf("expected old refresh preserved, got %q", td.RefreshToken)
	}
	if td.AccessToken != "at-2" {
		t.Errorf("access = %q", td.AccessToken)
	}
}

func readForm(r *http.Request) (url.Values, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	return r.PostForm, nil
}

// Sanity: ensure the scope string contains what ChatGPT OAuth requires.
func TestScope_ContainsOfflineAccess(t *testing.T) {
	if !strings.Contains(DefaultScope, "offline_access") {
		t.Error("scope must include offline_access for refresh tokens")
	}
}

// Short timeout sanity
func TestRunCallbackServer_Timeout(t *testing.T) {
	_, err := RunCallbackServer(context.Background(), "s", 100*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
}
