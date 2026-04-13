package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SessionUser holds the authenticated GitHub user.
type SessionUser struct {
	Login       string
	AvatarURL   string
	GitHubToken string
	IsAdmin     bool
	Orgs        []string
}

// Session is a single authenticated browser session.
type Session struct {
	ID            string
	User          *SessionUser
	CSRFToken     string
	OAuthState    string
	Authenticated bool
	CreatedAt     time.Time
	TouchedAt     time.Time
}

// SessionStore is a thread-safe in-memory session store with TTL.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore creates a store with the given session TTL.
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

// Create adds a new session for user and returns the session ID.
func (s *SessionStore) Create(user *SessionUser) string {
	id := randomHex(32)
	csrf := randomHex(32)
	now := time.Now()
	sess := &Session{
		ID:        id,
		User:      user,
		CSRFToken: csrf,
		CreatedAt: now,
		TouchedAt: now,
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return id
}

// Get returns the session if it exists and has not expired.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(sess.TouchedAt) > s.ttl {
		s.Delete(id)
		return nil, false
	}
	return sess, true
}

// Touch refreshes the session's TTL.
func (s *SessionStore) Touch(id string) {
	s.mu.Lock()
	if sess, ok := s.sessions[id]; ok {
		sess.TouchedAt = time.Now()
	}
	s.mu.Unlock()
}

// Delete removes a session.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// SetOAuthState atomically stores the OAuth state and PKCE verifier
// into the session's OAuthState field as "state:verifier".
func (s *SessionStore) SetOAuthState(id, state, verifier string) {
	s.mu.Lock()
	if sess, ok := s.sessions[id]; ok {
		sess.OAuthState = state + ":" + verifier
	}
	s.mu.Unlock()
}

// SetAuthenticated marks the session as fully authenticated.
func (s *SessionStore) SetAuthenticated(id string) {
	s.mu.Lock()
	if sess, ok := s.sessions[id]; ok {
		sess.Authenticated = true
	}
	s.mu.Unlock()
}

// StartGC launches a background goroutine that evicts expired sessions
// at the given interval. It runs until the process exits.
func (s *SessionStore) StartGC(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.evictExpired()
		}
	}()
}

// evictExpired removes all sessions whose TTL has elapsed.
func (s *SessionStore) evictExpired() {
	s.mu.Lock()
	for id, sess := range s.sessions {
		if time.Since(sess.TouchedAt) > s.ttl {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
}

// randomHex returns a hex-encoded string of n random bytes (2n chars).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
