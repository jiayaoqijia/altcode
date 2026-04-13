package web

import (
	"sync"
	"testing"
	"time"
)

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := NewSessionStore(time.Hour)
	user := &SessionUser{
		Login:       "octocat",
		AvatarURL:   "https://github.com/octocat.png",
		GitHubToken: "ghp_test123",
		IsAdmin:     true,
		Orgs:        []string{"github", "octoorg"},
	}
	id := store.Create(user)
	if id == "" {
		t.Fatal("expected non-empty session ID")
	}

	sess, ok := store.Get(id)
	if !ok || sess == nil {
		t.Fatal("expected session to exist")
	}
	if sess.User.Login != "octocat" {
		t.Errorf("Login = %q, want %q", sess.User.Login, "octocat")
	}
	if sess.User.GitHubToken != "ghp_test123" {
		t.Errorf("GitHubToken = %q, want %q",
			sess.User.GitHubToken, "ghp_test123")
	}
	if len(sess.User.Orgs) != 2 {
		t.Errorf("Orgs len = %d, want 2", len(sess.User.Orgs))
	}
	if sess.ID != id {
		t.Errorf("session ID = %q, want %q", sess.ID, id)
	}
	if sess.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}

	// Non-existent ID returns false.
	_, ok = store.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent session")
	}
}

func TestSessionStore_Expire(t *testing.T) {
	store := NewSessionStore(1 * time.Millisecond)
	id := store.Create(&SessionUser{Login: "expire-me"})

	time.Sleep(5 * time.Millisecond)

	sess, ok := store.Get(id)
	if ok || sess != nil {
		t.Error("expected session to be expired")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore(time.Hour)
	id := store.Create(&SessionUser{Login: "delete-me"})

	store.Delete(id)

	_, ok := store.Get(id)
	if ok {
		t.Error("expected session to be deleted")
	}

	// Deleting a non-existent ID is a no-op.
	store.Delete("nonexistent")
}

func TestSessionStore_Touch(t *testing.T) {
	store := NewSessionStore(50 * time.Millisecond)
	id := store.Create(&SessionUser{Login: "touchy"})

	time.Sleep(30 * time.Millisecond)
	store.Touch(id)
	time.Sleep(30 * time.Millisecond)

	sess, ok := store.Get(id)
	if !ok || sess == nil {
		t.Error("expected session to survive after Touch")
	}

	// Touching non-existent ID is a no-op.
	store.Touch("nonexistent")
}

func TestSessionStore_CSRFToken(t *testing.T) {
	store := NewSessionStore(time.Hour)
	id := store.Create(&SessionUser{Login: "csrf-user"})

	sess, ok := store.Get(id)
	if !ok {
		t.Fatal("session not found")
	}
	if sess.CSRFToken == "" {
		t.Error("expected non-empty CSRF token")
	}
	if len(sess.CSRFToken) < 32 {
		t.Errorf("CSRF token length = %d, want >= 32",
			len(sess.CSRFToken))
	}
}

func TestSessionStore_SetOAuthState(t *testing.T) {
	store := NewSessionStore(time.Hour)
	id := store.Create(&SessionUser{Login: "oauth-user"})

	// Capture CSRFToken before SetOAuthState.
	sessBefore, _ := store.Get(id)
	origCSRF := sessBefore.CSRFToken

	store.SetOAuthState(id, "state123", "verifier456")

	sess, ok := store.Get(id)
	if !ok {
		t.Fatal("session not found")
	}
	want := "state123:verifier456"
	if sess.OAuthState != want {
		t.Errorf("OAuthState = %q, want %q", sess.OAuthState, want)
	}
	// CSRFToken must NOT be overwritten by SetOAuthState.
	if sess.CSRFToken != origCSRF {
		t.Errorf("CSRFToken changed from %q to %q", origCSRF, sess.CSRFToken)
	}

	// SetOAuthState on non-existent session is a no-op.
	store.SetOAuthState("nonexistent", "s", "v")
}

func TestSessionStore_SetAuthenticated(t *testing.T) {
	store := NewSessionStore(time.Hour)
	id := store.Create(&SessionUser{Login: "auth-user"})

	sess, _ := store.Get(id)
	if sess.Authenticated {
		t.Error("new session should not be authenticated")
	}

	store.SetAuthenticated(id)

	sess, _ = store.Get(id)
	if !sess.Authenticated {
		t.Error("expected session to be authenticated after SetAuthenticated")
	}

	// SetAuthenticated on non-existent session is a no-op.
	store.SetAuthenticated("nonexistent")
}

func TestSessionStore_Concurrent(t *testing.T) {
	store := NewSessionStore(time.Hour)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := store.Create(&SessionUser{Login: "racer"})
			store.Touch(id)
			store.Get(id)
			store.SetOAuthState(id, "s", "v")
			store.Delete(id)
		}()
	}
	wg.Wait()
}
