package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockIntegrationStore implements DashboardStore, EventStore,
// RepoStore, and IssueStore for integration tests.
type mockIntegrationStore struct {
	tasks  []*TaskView
	repos  []string
	issues map[string][]*IssueView
	events map[string][]*EventView
}

func (m *mockIntegrationStore) ListTasks() ([]*TaskView, error) {
	return m.tasks, nil
}

func (m *mockIntegrationStore) ListRepos() ([]string, error) {
	return m.repos, nil
}

func (m *mockIntegrationStore) ListIssues(
	repo string,
) ([]*IssueView, error) {
	return m.issues[repo], nil
}

func (m *mockIntegrationStore) GetTask(
	id string,
) (*TaskView, error) {
	for _, t := range m.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockIntegrationStore) ListEvents(
	taskID string, afterID int64,
) ([]*EventView, error) {
	var out []*EventView
	for _, ev := range m.events[taskID] {
		if ev.ID > afterID {
			out = append(out, ev)
		}
	}
	return out, nil
}

func TestAllTemplatesRender(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	now := time.Now()

	pages := []struct {
		name string
		data PageData
	}{
		{
			"login",
			PageData{
				Title:   "Login",
				Content: map[string]string{"Error": ""},
			},
		},
		{
			"dashboard",
			PageData{
				Title:     "Dashboard",
				ShowNav:   true,
				User:      &SessionUser{Login: "test"},
				CSRFToken: "tok",
				Content: dashboardData{
					ActiveCount:  2,
					QueueDepth:   1,
					Cost24h:      3.50,
					SuccessRate:  85.0,
					Tasks:        nil,
					StatusFilter: "",
				},
			},
		},
		{
			"detail",
			PageData{
				Title:     "Detail",
				ShowNav:   true,
				User:      &SessionUser{Login: "test"},
				CSRFToken: "tok",
				Content: detailContentData{
					Task: &TaskView{
						ID:              "t1",
						RepoOwner:       "owner",
						RepoName:        "repo",
						TaskDescription: "Fix bug",
						Status:          "implementing",
						APICostUSD:      1.23,
						CreatedAt:       now,
					},
					IsActive: true,
					PhaseData: phaseBarData{
						Phases:       defaultPhases,
						CurrentPhase: "implementing",
					},
					Events: []*EventView{
						{
							ID:        1,
							TaskID:    "t1",
							EventType: "agent_output",
							Data:      "working",
							CreatedAt: now,
						},
					},
					CSRFToken: "tok",
				},
			},
		},
		{
			"new",
			PageData{
				Title:     "New",
				ShowNav:   true,
				User:      &SessionUser{Login: "test"},
				CSRFToken: "tok",
				Content: newTaskData{
					Repos: []string{"acme/api", "acme/cli"},
				},
			},
		},
		{
			"prs",
			PageData{
				Title:     "PRs",
				ShowNav:   true,
				User:      &SessionUser{Login: "test"},
				CSRFToken: "tok",
				Content: prPageData{
					PRs: []*PRView{
						{
							RepoOwner:       "acme",
							RepoName:        "api",
							PRNumber:        42,
							PRURL:           "https://github.com/acme/api/pull/42",
							TaskDescription: "Add endpoint",
							Status:          "merged",
							APICostUSD:      2.50,
							CreatedAt:       now,
						},
					},
					StatusFilter: "",
				},
			},
		},
		{
			"settings",
			PageData{
				Title:     "Settings",
				ShowNav:   true,
				IsAdmin:   true,
				User:      &SessionUser{Login: "test", IsAdmin: true},
				CSRFToken: "tok",
				Content: settingsData{
					GitHubLogin:   "test",
					AvatarURL:     "https://example.com/avatar.png",
					Orgs:          []string{"myorg"},
					DailyCap:      50.0,
					PerTaskCap:    5.0,
					MaxConcurrent: 3,
					DefaultModel:  "claude-sonnet",
					AllowedUsers:  []string{"test"},
					AllowedOrgs:   []string{"myorg"},
				},
			},
		},
		{
			"share",
			PageData{
				Title: "Share",
				Content: shareContentData{
					Task: &TaskView{
						ID:              "t-share",
						RepoOwner:       "owner",
						RepoName:        "repo",
						TaskDescription: "Shared task",
						Status:          "merged",
						CreatedAt:       now,
					},
					PhaseData: phaseBarData{
						Phases:       defaultPhases,
						CurrentPhase: "merged",
					},
					Events: []*EventView{
						{
							ID:        1,
							TaskID:    "t-share",
							EventType: "phase_completed",
							Data:      "planning",
							CreatedAt: now,
						},
					},
				},
			},
		},
		{
			"error-403",
			PageData{Title: "403"},
		},
		{
			"error-404",
			PageData{Title: "404"},
		},
		{
			"error-session_expired",
			PageData{Title: "Expired"},
		},
	}

	for _, tt := range pages {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			err := Render(w, tmpl, tt.name, tt.data)
			if err != nil {
				t.Fatalf("Render(%q): %v", tt.name, err)
			}
			body := w.Body.String()
			if len(body) == 0 {
				t.Errorf("Render(%q) produced empty output", tt.name)
			}
			if !strings.Contains(body, "AltFix") {
				t.Errorf("Render(%q) missing 'AltFix' branding",
					tt.name)
			}
		})
	}
}

func TestRegisterRoutes_MountsAllPaths(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{
		Sessions:       sessions,
		GitHubClientID: "test-id",
		GitHubSecret:   "test-secret",
		BaseURL:        "https://altfix.test",
		SigningKey:      []byte("test-key-at-least-32-bytes-long!"),
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, cfg); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	routes := []struct {
		method string
		path   string
		// wantAny accepts any of the listed status codes.
		wantAny []int
	}{
		{"GET", "/auth/github", []int{http.StatusFound}},
		{"GET", "/auth/callback", []int{http.StatusFound}},
		{"GET", "/ui/login", []int{http.StatusOK}},
		// Authenticated routes redirect to login without session.
		{"GET", "/ui/", []int{http.StatusFound}},
		{"GET", "/ui/tasks/new", []int{http.StatusFound}},
		{"GET", "/ui/tasks/test-id", []int{http.StatusFound}},
		{"GET", "/ui/prs", []int{http.StatusFound}},
		{"GET", "/ui/settings", []int{http.StatusFound}},
		// Share route gets 400 because token format is wrong.
		{"GET", "/share/badtoken", []int{http.StatusBadRequest}},
		// Partials redirect to login.
		{"GET", "/ui/partials/task-list", []int{http.StatusFound}},
		{"GET", "/ui/partials/kpi-cards", []int{http.StatusFound}},
		{"GET", "/ui/partials/repo-issues", []int{http.StatusFound}},
		// SSE route redirects to login.
		{"GET", "/ui/tasks/t1/events", []int{http.StatusFound}},
		// POST logout redirects to login (no session).
		{"POST", "/auth/logout", []int{http.StatusFound}},
	}

	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			req := httptest.NewRequest(r.method, r.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			code := w.Code
			ok := false
			for _, want := range r.wantAny {
				if code == want {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%s %s: got %d, want one of %v",
					r.method, r.path, code, r.wantAny)
			}
		})
	}
}

func TestOAuthFullFlow_Integration(t *testing.T) {
	ghAPI := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login/oauth/access_token":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{
					"access_token": "ghp_integration_token",
				})
			case "/user":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(GitHubUser{
					Login:     "testuser",
					AvatarURL: "https://github.com/testuser.png",
				})
			case "/user/orgs":
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]GitHubOrg{
					{Login: "testorg"},
				})
			default:
				http.NotFound(w, r)
			}
		},
	))
	defer ghAPI.Close()

	origAPI := gitHubAPIBase
	origToken := gitHubTokenURL
	gitHubAPIBase = ghAPI.URL
	gitHubTokenURL = ghAPI.URL + "/login/oauth/access_token"
	defer func() {
		gitHubAPIBase = origAPI
		gitHubTokenURL = origToken
	}()

	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{
		Sessions:       sessions,
		GitHubClientID: "int-cid",
		GitHubSecret:   "int-csec",
		BaseURL:        "https://altfix.test",
		SigningKey:      []byte("integration-key-at-least-32bytes!"),
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, cfg); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	// Step 1: GET /auth/github -> redirect to GitHub with state.
	req := httptest.NewRequest("GET", "/auth/github", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("Step 1: got %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "github.com/login/oauth/authorize") {
		t.Fatalf("Step 1: redirect not to GitHub: %s", loc)
	}

	// Extract oauth cookie and state from redirect URL.
	var oauthCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "altfix_oauth" {
			oauthCookie = c
			break
		}
	}
	if oauthCookie == nil {
		t.Fatal("Step 1: missing altfix_oauth cookie")
	}

	// Parse state from Location.
	parsed, _ := parseQuery(loc)
	state := parsed.Get("state")
	if state == "" {
		t.Fatal("Step 1: missing state in redirect URL")
	}

	// Step 2: GET /auth/callback with correct state and code.
	callbackURL := fmt.Sprintf(
		"/auth/callback?state=%s&code=test-auth-code", state,
	)
	req = httptest.NewRequest("GET", callbackURL, nil)
	req.AddCookie(oauthCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("Step 2: got %d, want 302", w.Code)
	}
	loc = w.Header().Get("Location")
	if loc != "/ui/" {
		t.Fatalf("Step 2: redirect to %q, want /ui/", loc)
	}

	// Extract session cookie.
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "altfix_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("Step 2: missing altfix_session cookie")
	}

	// Verify session is authenticated.
	sess, ok := sessions.Get(sessionCookie.Value)
	if !ok {
		t.Fatal("Step 2: session not found in store")
	}
	if !sess.Authenticated {
		t.Error("Step 2: session not authenticated")
	}
	if sess.User.Login != "testuser" {
		t.Errorf("Step 2: Login = %q, want testuser",
			sess.User.Login)
	}

	// Step 3: GET /ui/ with session cookie -> 200 (dashboard).
	req = httptest.NewRequest("GET", "/ui/", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Step 3: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("Step 3: dashboard page missing 'Dashboard'")
	}
	if !strings.Contains(body, "testuser") {
		t.Error("Step 3: dashboard missing username in nav")
	}
}

// parseQuery extracts query params from a full URL string.
func parseQuery(rawURL string) (values, error) {
	idx := strings.Index(rawURL, "?")
	if idx < 0 {
		return values{}, nil
	}
	return parseQueryString(rawURL[idx+1:])
}

// values wraps a simple map for query parameter access.
type values map[string]string

func (v values) Get(key string) string { return v[key] }

func parseQueryString(qs string) (values, error) {
	v := values{}
	for _, pair := range strings.Split(qs, "&") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			v[kv[0]] = kv[1]
		}
	}
	return v, nil
}

func TestCSRFProtection_Integration(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{
		Sessions:       sessions,
		GitHubClientID: "csrf-cid",
		GitHubSecret:   "csrf-csec",
		BaseURL:        "https://altfix.test",
		SigningKey:      []byte("csrf-key-needs-at-least-32-bytes!"),
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, cfg); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	// Create an authenticated session.
	sid := sessions.Create(&SessionUser{Login: "csrf-user"})
	sessions.SetAuthenticated(sid)
	sess, _ := sessions.Get(sid)
	sessionCookie := &http.Cookie{
		Name:  "altfix_session",
		Value: sid,
	}

	// POST /auth/logout without CSRF token -> 403.
	req := httptest.NewRequest("POST", "/auth/logout", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF: got %d, want 403", w.Code)
	}

	// POST /auth/logout with correct CSRF token -> redirect.
	req = httptest.NewRequest("POST", "/auth/logout", nil)
	req.AddCookie(sessionCookie)
	req.Header.Set("X-CSRF-Token", sess.CSRFToken)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("POST with CSRF: got %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/ui/login" {
		t.Errorf("logout redirect to %q, want /ui/login", loc)
	}
}

func TestShareLink_Integration(t *testing.T) {
	secret := []byte("share-integration-key")
	now := time.Now()

	store := &mockIntegrationStore{
		tasks: []*TaskView{
			{
				ID:              "task-share-int",
				RepoOwner:       "acme",
				RepoName:        "app",
				TaskDescription: "Deploy fix",
				Status:          "merged",
				CreatedAt:       now,
			},
		},
		events: map[string][]*EventView{
			"task-share-int": {
				{
					ID:        1,
					TaskID:    "task-share-int",
					EventType: "phase_completed",
					Data:      "planning",
					CreatedAt: now,
				},
			},
		},
	}

	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{
		Sessions:  sessions,
		SigningKey: secret,
	}
	h := NewWebHandler(tmpl, store, sessions, cfg, NewOrgCache(time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /share/{token}", h.HandleShareView)

	// Generate a valid share link.
	link := GenerateShareLink("task-share-int", secret, time.Hour)
	req := httptest.NewRequest("GET", link, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("valid share link: got %d, want 200; body: %s",
			w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Deploy fix") {
		t.Error("share page missing task description")
	}
	if !strings.Contains(body, "acme/app") {
		t.Error("share page missing repo name")
	}
	if !strings.Contains(body, "read-only") {
		t.Error("share page missing read-only notice")
	}

	// Expired share link -> 403.
	expiredExpiry := time.Now().Add(-10 * time.Minute).Unix()
	sig := SignShareURL("task-share-int", expiredExpiry, secret)
	expiredURL := fmt.Sprintf(
		"/share/task-share-int.%s?exp=%d", sig, expiredExpiry,
	)
	req = httptest.NewRequest("GET", expiredURL, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expired share link: got %d, want 403", w.Code)
	}
}

func TestDashboard_EmptyState(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	store := &mockIntegrationStore{tasks: nil}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "empty-user"})
	sessions.SetAuthenticated(sid)
	sess, _ := sessions.Get(sid)

	h := NewWebHandler(
		tmpl, store, sessions,
		WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/", h.HandleDashboard)

	req := httptest.NewRequest("GET", "/ui/", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "No tasks yet") {
		t.Error("empty dashboard missing 'No tasks yet' message")
	}
	// KPI cards should still render with zero values.
	if !strings.Contains(body, "Active") {
		t.Error("empty dashboard missing KPI cards")
	}
}

func TestMiddlewareChain(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	cfg := WebConfig{
		Sessions:       sessions,
		GitHubClientID: "mw-cid",
		GitHubSecret:   "mw-csec",
		BaseURL:        "https://altfix.test",
		SigningKey:      []byte("middleware-key-at-least-32-bytes!"),
	}

	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, cfg); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}

	// Unauthenticated request to /ui/ -> redirect to /ui/login.
	req := httptest.NewRequest("GET", "/ui/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("unauth: got %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/ui/login" {
		t.Errorf("unauth redirect to %q, want /ui/login", loc)
	}

	// Authenticated request to /ui/ -> 200.
	sid := sessions.Create(&SessionUser{Login: "mw-user"})
	sessions.SetAuthenticated(sid)
	sessionCookie := &http.Cookie{
		Name:  "altfix_session",
		Value: sid,
	}

	req = httptest.NewRequest("GET", "/ui/", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("auth: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("authenticated /ui/ missing 'Dashboard'")
	}
}
