package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthRedirect_SetsStateAndPKCE(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		GitHubClientID: "test-client-id",
		BaseURL:        "https://altfix.example.com",
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	w := httptest.NewRecorder()

	h.HandleOAuthRedirect(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}

	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	q := parsed.Query()

	if got := q.Get("client_id"); got != "test-client-id" {
		t.Errorf("client_id = %q, want %q", got, "test-client-id")
	}
	if got := q.Get("state"); got == "" {
		t.Error("expected non-empty state parameter")
	}
	if got := q.Get("code_challenge"); got == "" {
		t.Error("expected non-empty code_challenge parameter")
	}
	if got := q.Get("code_challenge_method"); got != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", got)
	}
	if got := q.Get("redirect_uri"); got != "https://altfix.example.com/auth/callback" {
		t.Errorf("redirect_uri = %q, want callback URL", got)
	}

	// Verify cookie was set.
	cookies := resp.Cookies()
	var oauthCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "altfix_oauth" {
			oauthCookie = c
			break
		}
	}
	if oauthCookie == nil {
		t.Fatal("expected altfix_oauth cookie")
	}
	if !oauthCookie.HttpOnly {
		t.Error("expected HttpOnly cookie")
	}
	if oauthCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", oauthCookie.SameSite)
	}

	// Verify session has state:verifier stored in OAuthState (not CSRFToken).
	sess, ok := sessions.Get(oauthCookie.Value)
	if !ok {
		t.Fatal("expected session for OAuth cookie")
	}
	parts := strings.SplitN(sess.OAuthState, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("OAuthState = %q, want state:verifier", sess.OAuthState)
	}
	if parts[0] != q.Get("state") {
		t.Errorf("stored state = %q, URL state = %q", parts[0], q.Get("state"))
	}
	if parts[1] == "" {
		t.Error("expected non-empty PKCE verifier")
	}

	// Verify challenge matches verifier.
	expectedChallenge := pkceChallenge(parts[1])
	if q.Get("code_challenge") != expectedChallenge {
		t.Errorf("code_challenge mismatch: got %q, want %q",
			q.Get("code_challenge"), expectedChallenge)
	}
}

func TestIsAuthorized(t *testing.T) {
	tests := []struct {
		name     string
		cfg      WebConfig
		login    string
		orgs     []string
		want     bool
	}{
		{
			name:  "allowed user",
			cfg:   WebConfig{AllowedUsers: []string{"octocat"}},
			login: "octocat",
			orgs:  nil,
			want:  true,
		},
		{
			name:  "allowed user case insensitive",
			cfg:   WebConfig{AllowedUsers: []string{"OctoCat"}},
			login: "octocat",
			orgs:  nil,
			want:  true,
		},
		{
			name:  "allowed org",
			cfg:   WebConfig{AllowedOrgs: []string{"myorg"}},
			login: "someone",
			orgs:  []string{"myorg", "other"},
			want:  true,
		},
		{
			name:  "allowed org case insensitive",
			cfg:   WebConfig{AllowedOrgs: []string{"MyOrg"}},
			login: "someone",
			orgs:  []string{"myorg"},
			want:  true,
		},
		{
			name:  "neither user nor org",
			cfg:   WebConfig{AllowedUsers: []string{"alice"}, AllowedOrgs: []string{"acme"}},
			login: "bob",
			orgs:  []string{"other"},
			want:  false,
		},
		{
			name:  "empty config allows all",
			cfg:   WebConfig{},
			login: "anyone",
			orgs:  []string{"anything"},
			want:  true,
		},
		{
			name:  "empty orgs with allowed orgs",
			cfg:   WebConfig{AllowedOrgs: []string{"acme"}},
			login: "bob",
			orgs:  nil,
			want:  false,
		},
	}

	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewWebHandler(
				tmpl, nil,
				NewSessionStore(time.Hour),
				tt.cfg, NewOrgCache(time.Hour),
			)
			got := h.isAuthorized(tt.login, tt.orgs)
			if got != tt.want {
				t.Errorf("isAuthorized(%q, %v) = %v, want %v",
					tt.login, tt.orgs, got, tt.want)
			}
		})
	}
}

func TestHandleLogout(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{}, NewOrgCache(time.Hour))

	// Create an authenticated session.
	sessionID := sessions.Create(&SessionUser{Login: "testuser"})
	sessions.SetAuthenticated(sessionID)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  "altfix_session",
		Value: sessionID,
	})
	w := httptest.NewRecorder()

	h.HandleLogout(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Verify redirect to login.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/ui/login" {
		t.Errorf("Location = %q, want /ui/login", loc)
	}

	// Verify cookie is cleared.
	var cleared bool
	for _, c := range resp.Cookies() {
		if c.Name == "altfix_session" && c.MaxAge < 0 {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Error("expected altfix_session cookie to be cleared")
	}

	// Verify session is deleted.
	_, ok := sessions.Get(sessionID)
	if ok {
		t.Error("expected session to be deleted")
	}
}

func TestHandleLogout_NoCookie(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	h := NewWebHandler(
		tmpl, nil,
		NewSessionStore(time.Hour),
		WebConfig{}, NewOrgCache(time.Hour),
	)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	w := httptest.NewRecorder()

	h.HandleLogout(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
}

func TestHandleLoginPage(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	h := NewWebHandler(
		tmpl, nil,
		NewSessionStore(time.Hour),
		WebConfig{}, NewOrgCache(time.Hour),
	)

	// Without error.
	req := httptest.NewRequest(http.MethodGet, "/ui/login", nil)
	w := httptest.NewRecorder()
	h.HandleLoginPage(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "AltFix Control Plane") {
		t.Error("expected 'AltFix Control Plane' in body")
	}
	if !strings.Contains(body, "Sign in with GitHub") {
		t.Error("expected 'Sign in with GitHub' in body")
	}
	if !strings.Contains(body, "Access restricted") {
		t.Error("expected access restricted note in body")
	}

	// With error.
	req = httptest.NewRequest(
		http.MethodGet, "/ui/login?error=access+denied", nil,
	)
	w = httptest.NewRecorder()
	h.HandleLoginPage(w, req)

	body = w.Body.String()
	if !strings.Contains(body, "access denied") {
		t.Error("expected error message in body")
	}
}

func TestOAuthCallback_StateMismatch(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(
		tmpl, nil, sessions,
		WebConfig{GitHubClientID: "cid"},
		NewOrgCache(time.Hour),
	)

	// Create a temp session with known state.
	tempID := sessions.Create(&SessionUser{})
	sessions.SetOAuthState(tempID, "good-state", "verifier")

	req := httptest.NewRequest(
		http.MethodGet,
		"/auth/callback?state=bad-state&code=abc",
		nil,
	)
	req.AddCookie(&http.Cookie{Name: "altfix_oauth", Value: tempID})
	w := httptest.NewRecorder()

	h.HandleOAuthCallback(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want redirect", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("expected error in redirect, got %q", loc)
	}
}

func TestOAuthCallback_MissingCookie(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	h := NewWebHandler(
		tmpl, nil,
		NewSessionStore(time.Hour),
		WebConfig{}, NewOrgCache(time.Hour),
	)

	req := httptest.NewRequest(
		http.MethodGet, "/auth/callback?state=x&code=y", nil,
	)
	w := httptest.NewRecorder()

	h.HandleOAuthCallback(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("expected error redirect, got %q", loc)
	}
}

func TestOAuthCallback_FullFlow(t *testing.T) {
	// Spin up a fake GitHub API server.
	ghAPI := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login/oauth/access_token":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{
					"access_token": "ghp_test",
				})
			case "/user":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(GitHubUser{
					Login:     "octocat",
					AvatarURL: "https://github.com/octocat.png",
				})
			case "/user/orgs":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]GitHubOrg{
					{Login: "myorg"},
				})
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer ghAPI.Close()

	// Override GitHub endpoints.
	origAPI := gitHubAPIBase
	origToken := gitHubTokenURL
	gitHubAPIBase = ghAPI.URL
	gitHubTokenURL = ghAPI.URL + "/login/oauth/access_token"
	defer func() {
		gitHubAPIBase = origAPI
		gitHubTokenURL = origToken
	}()

	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		GitHubClientID: "cid",
		GitHubSecret:   "csec",
		BaseURL:        "https://altfix.example.com",
	}, NewOrgCache(time.Hour))

	// Create a temp session with known state.
	tempID := sessions.Create(&SessionUser{})
	sessions.SetOAuthState(tempID, "the-state", "the-verifier")

	req := httptest.NewRequest(
		http.MethodGet,
		"/auth/callback?state=the-state&code=authcode",
		nil,
	)
	req.AddCookie(&http.Cookie{Name: "altfix_oauth", Value: tempID})
	w := httptest.NewRecorder()

	h.HandleOAuthCallback(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/ui/" {
		t.Errorf("Location = %q, want /ui/", loc)
	}

	// Verify session cookie was set.
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "altfix_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected altfix_session cookie")
	}

	// Verify the session exists with correct user data.
	sess, ok := sessions.Get(sessionCookie.Value)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if !sess.Authenticated {
		t.Error("expected session to be marked Authenticated")
	}
	if sess.User.Login != "octocat" {
		t.Errorf("Login = %q, want octocat", sess.User.Login)
	}
	if len(sess.User.Orgs) != 1 || sess.User.Orgs[0] != "myorg" {
		t.Errorf("Orgs = %v, want [myorg]", sess.User.Orgs)
	}

	// Verify temp session was cleaned up.
	_, ok = sessions.Get(tempID)
	if ok {
		t.Error("expected temp OAuth session to be deleted")
	}
}

func TestOrgCache(t *testing.T) {
	c := NewOrgCache(50 * time.Millisecond)

	// Miss on empty cache.
	_, ok := c.Get("alice")
	if ok {
		t.Error("expected cache miss")
	}

	// Set and hit.
	c.Set("alice", []string{"org1", "org2"})
	orgs, ok := c.Get("alice")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(orgs) != 2 {
		t.Errorf("orgs len = %d, want 2", len(orgs))
	}

	// Expire.
	time.Sleep(60 * time.Millisecond)
	_, ok = c.Get("alice")
	if ok {
		t.Error("expected cache miss after TTL")
	}
}

func TestPKCE(t *testing.T) {
	v := generatePKCEVerifier()
	if len(v) < 40 {
		t.Errorf("verifier length = %d, want >= 40", len(v))
	}
	c := pkceChallenge(v)
	if c == "" {
		t.Error("expected non-empty challenge")
	}
	if c == v {
		t.Error("challenge should differ from verifier")
	}

	// Same verifier produces same challenge.
	c2 := pkceChallenge(v)
	if c != c2 {
		t.Error("same verifier should produce same challenge")
	}
}

func TestHandleTestLogin_Enabled(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		TestMode: true,
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/auth/test-login", nil)
	w := httptest.NewRecorder()
	h.HandleTestLogin(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/ui/" {
		t.Errorf("Location = %q, want /ui/", loc)
	}

	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "altfix_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected altfix_session cookie")
	}
	if !sessionCookie.HttpOnly {
		t.Error("expected HttpOnly cookie")
	}

	sess, ok := sessions.Get(sessionCookie.Value)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if !sess.Authenticated {
		t.Error("expected session to be authenticated")
	}
	if sess.User.Login != "test-user" {
		t.Errorf("Login = %q, want test-user", sess.User.Login)
	}
	if !sess.User.IsAdmin {
		t.Error("expected test user to be admin")
	}
}

func TestHandleTestLogin_Disabled(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	h := NewWebHandler(tmpl, nil, NewSessionStore(time.Hour), WebConfig{
		TestMode: false,
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest(http.MethodGet, "/auth/test-login", nil)
	w := httptest.NewRecorder()
	h.HandleTestLogin(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestIsAdmin(t *testing.T) {
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(
		tmpl, nil,
		NewSessionStore(time.Hour),
		WebConfig{AdminUsers: []string{"admin1", "Admin2"}},
		NewOrgCache(time.Hour),
	)

	if !h.isAdmin("admin1") {
		t.Error("expected admin1 to be admin")
	}
	if !h.isAdmin("ADMIN2") {
		t.Error("expected case-insensitive admin match")
	}
	if h.isAdmin("nobody") {
		t.Error("expected nobody to not be admin")
	}
}
