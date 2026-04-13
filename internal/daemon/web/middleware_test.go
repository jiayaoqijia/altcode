package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequireAuth_RedirectsUnauthenticated(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	mw := RequireAuth(sessions)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard", nil)
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/ui/login" {
		t.Errorf("Location = %q, want /ui/login", loc)
	}
}

func TestRequireAuth_AllowsAuthenticated(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	id := sessions.Create(&SessionUser{Login: "alice", IsAdmin: true})

	mw := RequireAuth(sessions)

	var captured *Session
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetSession(r)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if captured == nil {
		t.Fatal("expected session in context")
	}
	if captured.User.Login != "alice" {
		t.Errorf("Login = %q, want alice", captured.User.Login)
	}
}

func TestRequireAuth_ExpiredSession(t *testing.T) {
	sessions := NewSessionStore(1 * time.Millisecond)
	id := sessions.Create(&SessionUser{Login: "bob"})

	time.Sleep(5 * time.Millisecond)

	mw := RequireAuth(sessions)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if loc != "/ui/login" {
		t.Errorf("Location = %q, want /ui/login", loc)
	}
}

func TestCSRFCheck_SkipsGET(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	id := sessions.Create(&SessionUser{Login: "carol"})

	authMW := RequireAuth(sessions)
	csrfMW := CSRFCheck()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap: RequireAuth -> CSRFCheck -> handler
	handler := authMW(csrfMW(inner))

	req := httptest.NewRequest(http.MethodGet, "/ui/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestCSRFCheck_RejectsInvalidToken(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	id := sessions.Create(&SessionUser{Login: "dave"})

	authMW := RequireAuth(sessions)
	csrfMW := CSRFCheck()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMW(csrfMW(inner))

	req := httptest.NewRequest(http.MethodPost, "/ui/action", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	req.Header.Set("X-CSRF-Token", "wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d",
			resp.StatusCode, http.StatusForbidden)
	}
}

func TestCSRFCheck_AcceptsValidToken(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	id := sessions.Create(&SessionUser{Login: "eve"})

	sess, ok := sessions.Get(id)
	if !ok {
		t.Fatal("session not found")
	}

	authMW := RequireAuth(sessions)
	csrfMW := CSRFCheck()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMW(csrfMW(inner))

	req := httptest.NewRequest(http.MethodPost, "/ui/action", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	req.Header.Set("X-CSRF-Token", sess.CSRFToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestCSRFCheck_AcceptsFormField(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	id := sessions.Create(&SessionUser{Login: "frank"})

	sess, ok := sessions.Get(id)
	if !ok {
		t.Fatal("session not found")
	}

	authMW := RequireAuth(sessions)
	csrfMW := CSRFCheck()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMW(csrfMW(inner))

	body := "_csrf=" + sess.CSRFToken
	req := httptest.NewRequest(
		http.MethodPost, "/ui/action", strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestRequireAdmin_RejectsNonAdmin(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	id := sessions.Create(&SessionUser{
		Login:   "normie",
		IsAdmin: false,
	})

	authMW := RequireAuth(sessions)
	adminMW := RequireAdmin()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMW(adminMW(inner))

	req := httptest.NewRequest(http.MethodGet, "/ui/settings", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d",
			resp.StatusCode, http.StatusForbidden)
	}
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	sessions := NewSessionStore(time.Hour)
	id := sessions.Create(&SessionUser{
		Login:   "boss",
		IsAdmin: true,
	})

	authMW := RequireAuth(sessions)
	adminMW := RequireAdmin()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMW(adminMW(inner))

	req := httptest.NewRequest(http.MethodGet, "/ui/settings", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: id})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
