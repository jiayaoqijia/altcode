package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAbsPath_EmptyBasePath(t *testing.T) {
	funcs := templateFuncs("")
	absPath := funcs["absPath"].(func(string) string)

	tests := []struct {
		input string
		want  string
	}{
		{"/ui/", "/ui/"},
		{"/auth/github", "/auth/github"},
		{"/ui/static/app.css", "/ui/static/app.css"},
	}
	for _, tt := range tests {
		got := absPath(tt.input)
		if got != tt.want {
			t.Errorf("absPath(%q) = %q, want %q",
				tt.input, got, tt.want)
		}
	}
}

func TestAbsPath_WithBasePath(t *testing.T) {
	funcs := templateFuncs("/dash/foo")
	absPath := funcs["absPath"].(func(string) string)

	tests := []struct {
		input string
		want  string
	}{
		{"/ui/", "/dash/foo/ui/"},
		{"/auth/github", "/dash/foo/auth/github"},
		{"/ui/static/app.css", "/dash/foo/ui/static/app.css"},
		{"/tasks", "/dash/foo/tasks"},
	}
	for _, tt := range tests {
		got := absPath(tt.input)
		if got != tt.want {
			t.Errorf("absPath(%q) = %q, want %q",
				tt.input, got, tt.want)
		}
	}
}

func TestTemplateRender_BasePathInAllURLs(t *testing.T) {
	bp := "/dash/foo"
	tmpl, err := LoadTemplates(bp)
	if err != nil {
		t.Fatalf("LoadTemplates(%q): %v", bp, err)
	}

	// Render layout+login page (has static assets, nav links).
	w := httptest.NewRecorder()
	data := PageData{
		Title:     "Login",
		ShowNav:   true,
		CSRFToken: "tok",
		User: &SessionUser{
			Login:   "test",
			IsAdmin: true,
		},
	}
	if err := Render(w, tmpl, "login", data); err != nil {
		t.Fatalf("Render login: %v", err)
	}
	body := w.Body.String()

	// All URLs in the rendered page should have the base path.
	mustContain := []string{
		"/dash/foo/ui/static/tailwind.css",
		"/dash/foo/ui/static/app.css",
		"/dash/foo/ui/static/htmx.min.js",
		"/dash/foo/ui/",
		"/dash/foo/ui/prs",
		"/dash/foo/ui/settings",
		"/dash/foo/auth/logout",
		"/dash/foo/auth/github",
	}
	for _, s := range mustContain {
		if !strings.Contains(body, s) {
			t.Errorf("rendered login missing %q", s)
		}
	}

	// Verify no bare /ui/ or /auth/ URL attributes appear
	// without the base path prefix. We check that the attribute
	// value starts with the base path, not a bare slash.
	barePatterns := []string{
		`href="/ui/`,
		`src="/ui/`,
		`action="/ui/`,
		`action="/auth/`,
		`href="/auth/`,
	}
	for _, p := range barePatterns {
		if strings.Contains(body, p) {
			t.Errorf(
				"found bare URL pattern %q without base path prefix",
				p,
			)
		}
	}
}

func TestTemplateRender_EmptyBasePathNoPrefix(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	w := httptest.NewRecorder()
	data := PageData{
		Title: "Login",
	}
	if err := Render(w, tmpl, "login", data); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := w.Body.String()

	// With empty base path, URLs should start with / directly.
	mustContain := []string{
		`"/ui/static/tailwind.css"`,
		`"/ui/static/app.css"`,
		`"/auth/github"`,
	}
	for _, s := range mustContain {
		if !strings.Contains(body, s) {
			t.Errorf("rendered login missing %q", s)
		}
	}
}

func TestBasePathNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/dash/vm1", "/dash/vm1"},
		{"/dash/vm1/", "/dash/vm1"},
		{"/dash/vm1///", "/dash/vm1"},
	}
	for _, tt := range tests {
		got := strings.TrimRight(tt.input, "/")
		if got != tt.want {
			t.Errorf("TrimRight(%q, '/') = %q, want %q",
				tt.input, got, tt.want)
		}
	}
}

func TestValidateBasePath(t *testing.T) {
	tests := []struct {
		bp      string
		wantErr bool
	}{
		{"", false},
		{"/dash/vm1", false},
		{"/a/b/c", false},
		{"no-slash", true},
		{"/dash/vm1?q=1", true},
		{"/dash/vm%201", true},
		{"/dash;drop", true},
		{"/dash vm1", true},
		{"/dash#frag", true},
	}
	for _, tt := range tests {
		err := ValidateBasePath(tt.bp)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateBasePath(%q) err=%v, wantErr=%v",
				tt.bp, err, tt.wantErr)
		}
	}
}

func TestCookiePath_WithBasePath(t *testing.T) {
	tmpl, _ := LoadTemplates("/dash/foo")
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		BasePath: "/dash/foo",
		TestMode: true,
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest(
		http.MethodGet, "/auth/test-login", nil,
	)
	w := httptest.NewRecorder()
	h.HandleTestLogin(w, req)

	resp := w.Result()
	defer resp.Body.Close()

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
	if sessionCookie.Path != "/dash/foo/" {
		t.Errorf("cookie Path = %q, want /dash/foo/",
			sessionCookie.Path)
	}
}

func TestCookiePath_EmptyBasePath(t *testing.T) {
	tmpl, _ := LoadTemplates("")
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		TestMode: true,
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest(
		http.MethodGet, "/auth/test-login", nil,
	)
	w := httptest.NewRecorder()
	h.HandleTestLogin(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "altfix_session" {
			if c.Path != "/" {
				t.Errorf("cookie Path = %q, want /", c.Path)
			}
			return
		}
	}
	t.Fatal("expected altfix_session cookie")
}

func TestLogout_BasePath_Redirect(t *testing.T) {
	tmpl, _ := LoadTemplates("/dash/vm1")
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		BasePath: "/dash/vm1",
	}, NewOrgCache(time.Hour))

	sid := sessions.Create(&SessionUser{Login: "test"})
	sessions.SetAuthenticated(sid)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{
		Name: "altfix_session", Value: sid,
	})
	w := httptest.NewRecorder()
	h.HandleLogout(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc != "/dash/vm1/ui/login" {
		t.Errorf("logout Location = %q, want /dash/vm1/ui/login",
			loc)
	}
}

func TestTestLogin_BasePath_Redirect(t *testing.T) {
	tmpl, _ := LoadTemplates("/dash/vm1")
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		BasePath: "/dash/vm1",
		TestMode: true,
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest(
		http.MethodGet, "/auth/test-login", nil,
	)
	w := httptest.NewRecorder()
	h.HandleTestLogin(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc != "/dash/vm1/ui/" {
		t.Errorf("test-login Location = %q, want /dash/vm1/ui/",
			loc)
	}
}

// --- Session Ticket (Cloud Mode) Tests ---

// mintTicket creates a valid JWT for testing.
func mintTicket(
	key []byte, claims TicketClaims,
) string {
	header := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"alg":"HS256","typ":"JWT"}`),
	)
	claimsJSON, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig
}

func TestSessionTicket_Valid(t *testing.T) {
	key := []byte("test-session-signing-key-32bytes!")
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("/dash/vm1")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode:         true,
		SessionSigningKey: key,
		BasePath:          "/dash/vm1",
	}, NewOrgCache(time.Hour))

	claims := TicketClaims{
		Issuer:       "cloud-claw",
		Subject:      "altcode-ui",
		PortalUserID: "alice",
		VMName:       "vm1",
		JTI:          "unique-jti-1",
		Exp:          time.Now().Add(5 * time.Minute).Unix(),
	}
	token := mintTicket(key, claims)

	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/auth/session?ticket=%s", token),
		nil,
	)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode,
			http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/dash/vm1/ui/" {
		t.Errorf("Location = %q, want /dash/vm1/ui/", loc)
	}

	// Verify cookie.
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
	if sessionCookie.Path != "/dash/vm1/" {
		t.Errorf("cookie Path = %q, want /dash/vm1/",
			sessionCookie.Path)
	}

	// Verify session exists and is authenticated.
	sess, ok := sessions.Get(sessionCookie.Value)
	if !ok {
		t.Fatal("expected session to exist")
	}
	if !sess.Authenticated {
		t.Error("expected session to be authenticated")
	}
	if sess.User.Login != "alice" {
		t.Errorf("Login = %q, want alice", sess.User.Login)
	}
}

func TestSessionTicket_Expired(t *testing.T) {
	key := []byte("test-session-signing-key-32bytes!")
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode:         true,
		SessionSigningKey: key,
	}, NewOrgCache(time.Hour))

	claims := TicketClaims{
		Issuer:       "cloud-claw",
		Subject:      "altcode-ui",
		PortalUserID: "alice",
		VMName:       "vm1",
		JTI:          "expired-jti",
		Exp:          time.Now().Add(-5 * time.Minute).Unix(),
	}
	token := mintTicket(key, claims)

	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/auth/session?ticket=%s", token),
		nil,
	)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusUnauthorized)
	}
}

func TestSessionTicket_Replay(t *testing.T) {
	key := []byte("test-session-signing-key-32bytes!")
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode:         true,
		SessionSigningKey: key,
	}, NewOrgCache(time.Hour))

	claims := TicketClaims{
		Issuer:       "cloud-claw",
		Subject:      "altcode-ui",
		PortalUserID: "alice",
		VMName:       "vm1",
		JTI:          "replay-jti",
		Exp:          time.Now().Add(5 * time.Minute).Unix(),
	}
	token := mintTicket(key, claims)

	// First use: should succeed.
	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/auth/session?ticket=%s", token),
		nil,
	)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("first use: status = %d, want %d",
			w.Code, http.StatusFound)
	}

	// Second use (replay): should be rejected.
	req = httptest.NewRequest(
		"GET",
		fmt.Sprintf("/auth/session?ticket=%s", token),
		nil,
	)
	w = httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("replay: status = %d, want %d",
			w.Code, http.StatusUnauthorized)
	}
}

func TestSessionTicket_InvalidSignature(t *testing.T) {
	key := []byte("test-session-signing-key-32bytes!")
	wrongKey := []byte("wrong-key-that-is-32-bytes-long!")
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode:         true,
		SessionSigningKey: key,
	}, NewOrgCache(time.Hour))

	claims := TicketClaims{
		Issuer:       "cloud-claw",
		Subject:      "altcode-ui",
		PortalUserID: "alice",
		VMName:       "vm1",
		JTI:          "bad-sig-jti",
		Exp:          time.Now().Add(5 * time.Minute).Unix(),
	}
	token := mintTicket(wrongKey, claims)

	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/auth/session?ticket=%s", token),
		nil,
	)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusUnauthorized)
	}
}

func TestSessionTicket_MissingTicket(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode: true,
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest("GET", "/auth/session", nil)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusBadRequest)
	}
}

func TestSessionTicket_CloudModeDisabled(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode: false,
	}, NewOrgCache(time.Hour))

	req := httptest.NewRequest(
		"GET", "/auth/session?ticket=xxx", nil,
	)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusNotFound)
	}
}

func TestSessionTicket_WrongIssuer(t *testing.T) {
	key := []byte("test-session-signing-key-32bytes!")
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode:         true,
		SessionSigningKey: key,
	}, NewOrgCache(time.Hour))

	claims := TicketClaims{
		Issuer:       "wrong-issuer",
		Subject:      "altcode-ui",
		PortalUserID: "alice",
		VMName:       "vm1",
		JTI:          "wrong-iss-jti",
		Exp:          time.Now().Add(5 * time.Minute).Unix(),
	}
	token := mintTicket(key, claims)

	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/auth/session?ticket=%s", token),
		nil,
	)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusUnauthorized)
	}
}

func TestSessionTicket_WrongSubject(t *testing.T) {
	key := []byte("test-session-signing-key-32bytes!")
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode:         true,
		SessionSigningKey: key,
	}, NewOrgCache(time.Hour))

	claims := TicketClaims{
		Issuer:       "cloud-claw",
		Subject:      "wrong-subject",
		PortalUserID: "alice",
		VMName:       "vm1",
		JTI:          "wrong-sub-jti",
		Exp:          time.Now().Add(5 * time.Minute).Unix(),
	}
	token := mintTicket(key, claims)

	req := httptest.NewRequest(
		"GET",
		fmt.Sprintf("/auth/session?ticket=%s", token),
		nil,
	)
	w := httptest.NewRecorder()
	h.HandleSessionTicket(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d",
			w.Code, http.StatusUnauthorized)
	}
}

func TestSessionTicket_MalformedToken(t *testing.T) {
	key := []byte("test-session-signing-key-32bytes!")
	sessions := NewSessionStore(time.Hour)
	tmpl, _ := LoadTemplates("")
	h := NewWebHandler(tmpl, nil, sessions, WebConfig{
		CloudMode:         true,
		SessionSigningKey: key,
	}, NewOrgCache(time.Hour))

	tests := []struct {
		name   string
		ticket string
	}{
		{"no dots", "nodots"},
		{"one dot", "one.dot"},
		{"empty parts", ".."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				"GET",
				"/auth/session?ticket="+tt.ticket,
				nil,
			)
			w := httptest.NewRecorder()
			h.HandleSessionTicket(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d",
					w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestGenerateShareLink_WithBasePath(t *testing.T) {
	secret := []byte("share-secret-key")
	link := GenerateShareLink(
		"/dash/vm1", "task-123", secret, time.Hour,
	)

	if !strings.HasPrefix(link, "/dash/vm1/share/task-123.") {
		t.Errorf("link = %q, want prefix /dash/vm1/share/task-123.",
			link)
	}
	if !strings.Contains(link, "?exp=") {
		t.Errorf("link missing exp param: %s", link)
	}
}

func TestGenerateShareLink_EmptyBasePath(t *testing.T) {
	secret := []byte("share-secret-key")
	link := GenerateShareLink("", "task-456", secret, time.Hour)

	if !strings.HasPrefix(link, "/share/task-456.") {
		t.Errorf("link = %q, want prefix /share/task-456.", link)
	}
}

func TestVerifySessionTicket_ValidToken(t *testing.T) {
	key := []byte("test-key-that-is-32-bytes-long!!")
	claims := TicketClaims{
		Issuer:       "cloud-claw",
		Subject:      "altcode-ui",
		PortalUserID: "bob",
		VMName:       "vm2",
		JTI:          "verify-jti",
		Exp:          time.Now().Add(5 * time.Minute).Unix(),
	}
	token := mintTicket(key, claims)

	got, err := verifySessionTicket(token, key)
	if err != nil {
		t.Fatalf("verifySessionTicket: %v", err)
	}
	if got.Issuer != "cloud-claw" {
		t.Errorf("Issuer = %q, want cloud-claw", got.Issuer)
	}
	if got.PortalUserID != "bob" {
		t.Errorf("PortalUserID = %q, want bob", got.PortalUserID)
	}
	if got.JTI != "verify-jti" {
		t.Errorf("JTI = %q, want verify-jti", got.JTI)
	}
}

func TestJTI_UsedTracking(t *testing.T) {
	store := NewSessionStore(time.Hour)

	if store.IsJTIUsed("jti-1") {
		t.Error("expected jti-1 to not be used")
	}

	store.MarkJTIUsed("jti-1")

	if !store.IsJTIUsed("jti-1") {
		t.Error("expected jti-1 to be used after marking")
	}

	if store.IsJTIUsed("jti-2") {
		t.Error("expected jti-2 to not be used")
	}
}

func TestRequireAuth_BasePath_Redirect(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	auth := RequireAuth(sessions, "/dash/vm1")
	handler := auth(http.HandlerFunc(func(
		w http.ResponseWriter, r *http.Request,
	) {
		w.WriteHeader(http.StatusOK)
	}))

	// No cookie: should redirect to basePath + /ui/login.
	req := httptest.NewRequest("GET", "/ui/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d",
			resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/dash/vm1/ui/login" {
		t.Errorf("Location = %q, want /dash/vm1/ui/login", loc)
	}
}

func TestCloudMode_GithubAuth_Returns404(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	mux := http.NewServeMux()

	err := RegisterRoutes(mux, WebConfig{
		Sessions:  sessions,
		CloudMode: true,
		TestMode:  true,
	})
	if err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	req := httptest.NewRequest("GET", "/auth/github", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("/auth/github in cloud mode: status = %d, want 404",
			w.Code)
	}
}
