package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// githubHTTPClient is used for all GitHub API calls instead of
// http.DefaultClient so that a stalled upstream can't block forever.
var githubHTTPClient = &http.Client{Timeout: 10 * time.Second}

// StoreIface is a placeholder for the daemon.Store dependency.
// Task 8 wires the real implementation.
type StoreIface interface{}

// WebConfig holds OAuth and access-control settings.
type WebConfig struct {
	Sessions       *SessionStore
	GitHubClientID string
	GitHubSecret   string
	AllowedOrgs    []string
	AllowedUsers   []string
	AdminUsers     []string
	SigningKey      []byte
	BaseURL        string
	// TrustProxy controls whether X-Forwarded-Proto is trusted for
	// determining TLS status. Only enable behind a known reverse proxy.
	TrustProxy bool
}

// oauthPendingCount tracks in-flight OAuth sessions per source IP
// to prevent session-flood DoS. Value type is *int32.
var oauthPendingCount sync.Map

// maxOAuthPendingPerIP is the cap on concurrent OAuth sessions per IP.
const maxOAuthPendingPerIP int32 = 10

// OrgCache caches GitHub org membership per user with a TTL.
type OrgCache struct {
	mu      sync.RWMutex
	entries map[string]orgEntry
	ttl     time.Duration
}

type orgEntry struct {
	orgs      []string
	expiresAt time.Time
}

// NewOrgCache creates an org cache with the given TTL.
func NewOrgCache(ttl time.Duration) *OrgCache {
	return &OrgCache{
		entries: make(map[string]orgEntry),
		ttl:     ttl,
	}
}

// Get returns cached orgs for the login, if present and not expired.
func (c *OrgCache) Get(login string) ([]string, bool) {
	c.mu.RLock()
	e, ok := c.entries[login]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.orgs, true
}

// Set stores orgs for the login.
func (c *OrgCache) Set(login string, orgs []string) {
	c.mu.Lock()
	c.entries[login] = orgEntry{
		orgs:      orgs,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// WebHandler serves the web UI, handling OAuth and page rendering.
type WebHandler struct {
	tmpl     *Templates
	store    StoreIface
	sessions *SessionStore
	cfg      WebConfig
	orgCache *OrgCache
}

// NewWebHandler creates a handler with the provided dependencies.
func NewWebHandler(
	tmpl *Templates,
	store StoreIface,
	sessions *SessionStore,
	cfg WebConfig,
	orgCache *OrgCache,
) *WebHandler {
	return &WebHandler{
		tmpl:     tmpl,
		store:    store,
		sessions: sessions,
		cfg:      cfg,
		orgCache: orgCache,
	}
}

// HandleLoginPage renders the login template. An optional "error" query
// parameter is displayed to the user.
func (h *WebHandler) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	errMsg := r.URL.Query().Get("error")
	data := PageData{
		Title:   "Login",
		ShowNav: false,
		Content: map[string]string{"Error": errMsg},
	}
	if err := Render(w, h.tmpl, "login", data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// HandleOAuthRedirect generates state + PKCE verifier, stores them in
// the session store, sets an altfix_oauth cookie, and redirects the
// browser to GitHub's authorization endpoint.
func (h *WebHandler) HandleOAuthRedirect(
	w http.ResponseWriter, r *http.Request,
) {
	// Rate-limit pending OAuth sessions per source IP.
	ip := r.RemoteAddr
	if h.cfg.TrustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = strings.Split(fwd, ",")[0]
		}
	}
	countPtr, _ := oauthPendingCount.LoadOrStore(
		ip, new(int32),
	)
	count := atomic.AddInt32(countPtr.(*int32), 1)
	if count > maxOAuthPendingPerIP {
		atomic.AddInt32(countPtr.(*int32), -1)
		http.Error(w, "too many pending OAuth requests", http.StatusTooManyRequests)
		return
	}

	state := randomHex(16)
	verifier := generatePKCEVerifier()
	challenge := pkceChallenge(verifier)

	// Create a temporary session to hold state+verifier.
	tempID := h.sessions.Create(&SessionUser{})
	h.sessions.SetOAuthState(tempID, state, verifier)

	http.SetCookie(w, &http.Cookie{
		Name:     "altfix_oauth",
		Value:    tempID,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		Secure:   isSecure(r, h.cfg.TrustProxy),
		SameSite: http.SameSiteLaxMode,
	})

	params := url.Values{
		"client_id":             {h.cfg.GitHubClientID},
		"redirect_uri":         {h.cfg.BaseURL + "/auth/callback"},
		"scope":                 {"read:org,read:user"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	target := "https://github.com/login/oauth/authorize?" + params.Encode()
	http.Redirect(w, r, target, http.StatusFound)
}

// HandleOAuthCallback verifies the state from the cookie, exchanges the
// authorization code for a token, fetches user + orgs, checks
// authorization, creates a session, and redirects to the dashboard.
func (h *WebHandler) HandleOAuthCallback(
	w http.ResponseWriter, r *http.Request,
) {
	// Decrement the per-IP OAuth pending counter.
	ip := r.RemoteAddr
	if h.cfg.TrustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = strings.Split(fwd, ",")[0]
		}
	}
	if countPtr, ok := oauthPendingCount.Load(ip); ok {
		atomic.AddInt32(countPtr.(*int32), -1)
	}

	cookie, err := r.Cookie("altfix_oauth")
	if err != nil {
		h.loginError(w, r, "missing OAuth cookie")
		return
	}
	sess, ok := h.sessions.Get(cookie.Value)
	if !ok {
		h.loginError(w, r, "expired OAuth session")
		return
	}
	parts := strings.SplitN(sess.OAuthState, ":", 2)
	if len(parts) != 2 {
		h.loginError(w, r, "invalid OAuth state")
		return
	}
	storedState, verifier := parts[0], parts[1]
	h.sessions.Delete(cookie.Value)

	// Clear the OAuth cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "altfix_oauth",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure(r, h.cfg.TrustProxy),
		SameSite: http.SameSiteLaxMode,
	})

	cbState := r.URL.Query().Get("state")
	if cbState == "" || cbState != storedState {
		h.loginError(w, r, "state mismatch")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		h.loginError(w, r, "missing code")
		return
	}

	token, err := h.exchangeCode(code, verifier)
	if err != nil {
		h.loginError(w, r, "token exchange failed")
		return
	}

	ghUser, err := h.fetchGitHubUser(token)
	if err != nil {
		h.loginError(w, r, "failed to fetch user")
		return
	}

	orgs, err := h.fetchGitHubOrgs(token)
	if err != nil {
		h.loginError(w, r, "failed to fetch orgs")
		return
	}
	h.orgCache.Set(ghUser.Login, orgNames(orgs))

	if !h.isAuthorized(ghUser.Login, orgNames(orgs)) {
		h.loginError(w, r, "access denied")
		return
	}

	user := &SessionUser{
		Login:       ghUser.Login,
		AvatarURL:   ghUser.AvatarURL,
		GitHubToken: token,
		IsAdmin:     h.isAdmin(ghUser.Login),
		Orgs:        orgNames(orgs),
	}
	sessionID := h.sessions.Create(user)
	h.sessions.SetAuthenticated(sessionID)

	http.SetCookie(w, &http.Cookie{
		Name:     "altfix_session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400, // 24 hours
		HttpOnly: true,
		Secure:   isSecure(r, h.cfg.TrustProxy),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/ui/", http.StatusFound)
}

// HandleLogout deletes the session, clears the cookie, and redirects
// to the login page.
func (h *WebHandler) HandleLogout(
	w http.ResponseWriter, r *http.Request,
) {
	if cookie, err := r.Cookie("altfix_session"); err == nil {
		h.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "altfix_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure(r, h.cfg.TrustProxy),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/ui/login", http.StatusFound)
}

// isAuthorized returns true if the login is in the allowed users list,
// any of the user's orgs is in the allowed orgs list, or both lists
// are empty (allow-all).
func (h *WebHandler) isAuthorized(login string, orgs []string) bool {
	if len(h.cfg.AllowedUsers) == 0 && len(h.cfg.AllowedOrgs) == 0 {
		// No access restrictions configured — allowing all GitHub users.
		// Set --allowed-orgs or --allowed-users in production.
		log.Printf("warn: no AllowedUsers or AllowedOrgs configured, allowing %q", login)
		return true
	}
	for _, u := range h.cfg.AllowedUsers {
		if strings.EqualFold(u, login) {
			return true
		}
	}
	orgSet := make(map[string]bool, len(h.cfg.AllowedOrgs))
	for _, o := range h.cfg.AllowedOrgs {
		orgSet[strings.ToLower(o)] = true
	}
	for _, o := range orgs {
		if orgSet[strings.ToLower(o)] {
			return true
		}
	}
	return false
}

// isAdmin returns true if the login is in the admin users list.
func (h *WebHandler) isAdmin(login string) bool {
	for _, u := range h.cfg.AdminUsers {
		if strings.EqualFold(u, login) {
			return true
		}
	}
	return false
}

// loginError redirects to the login page with an error message.
func (h *WebHandler) loginError(
	w http.ResponseWriter, r *http.Request, msg string,
) {
	http.Redirect(
		w, r,
		"/ui/login?error="+url.QueryEscape(msg),
		http.StatusFound,
	)
}

// GitHubUser is the subset of the /user response we need.
type GitHubUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubOrg is the subset of the /user/orgs response we need.
type GitHubOrg struct {
	Login string `json:"login"`
}

// orgNames extracts the login strings from a slice of GitHubOrg.
func orgNames(orgs []GitHubOrg) []string {
	names := make([]string, len(orgs))
	for i, o := range orgs {
		names[i] = o.Login
	}
	return names
}

// gitHubAPIBase is the GitHub API root, overridable in tests.
var gitHubAPIBase = "https://api.github.com"

// gitHubTokenURL is the token exchange endpoint, overridable in tests.
var gitHubTokenURL = "https://github.com/login/oauth/access_token"

// exchangeCode exchanges an authorization code for an access token
// using the PKCE verifier.
func (h *WebHandler) exchangeCode(
	code, verifier string,
) (string, error) {
	data := url.Values{
		"client_id":     {h.cfg.GitHubClientID},
		"client_secret": {h.cfg.GitHubSecret},
		"code":          {code},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequest(
		http.MethodPost, gitHubTokenURL, strings.NewReader(data.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "AltFix/1.0")

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token response: %s", resp.Status)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	if tok.Error != "" {
		return "", fmt.Errorf("github: %s", tok.Error)
	}
	return tok.AccessToken, nil
}

// fetchGitHubUser calls GET /user and returns the authenticated user.
func (h *WebHandler) fetchGitHubUser(
	token string,
) (*GitHubUser, error) {
	req, err := http.NewRequest(
		http.MethodGet, gitHubAPIBase+"/user", nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "AltFix/1.0")

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user response %s: %s",
			resp.Status, string(body))
	}
	var u GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &u, nil
}

// fetchGitHubOrgs calls GET /user/orgs and returns the user's orgs.
func (h *WebHandler) fetchGitHubOrgs(
	token string,
) ([]GitHubOrg, error) {
	req, err := http.NewRequest(
		http.MethodGet, gitHubAPIBase+"/user/orgs", nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "AltFix/1.0")

	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orgs request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("orgs response %s: %s",
			resp.Status, string(body))
	}
	var orgs []GitHubOrg
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, fmt.Errorf("decode orgs: %w", err)
	}
	return orgs, nil
}

// generatePKCEVerifier creates a 32-byte random verifier,
// base64url-encoded (43 chars, no padding).
func generatePKCEVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// pkceChallenge computes the S256 code challenge from the verifier.
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// isSecure returns true if the request arrived over TLS.
// X-Forwarded-Proto is only trusted when trustProxy is true,
// preventing cookie secure-flag downgrade via header spoofing.
func isSecure(r *http.Request, trustProxy bool) bool {
	if r.TLS != nil {
		return true
	}
	if trustProxy && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}
