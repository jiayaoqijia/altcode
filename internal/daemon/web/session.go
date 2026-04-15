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
	usedJTIs map[string]time.Time
}

// NewSessionStore creates a store with the given session TTL.
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
		usedJTIs: make(map[string]time.Time),
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
// It returns a snapshot copy so callers cannot race with Touch.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	if !ok {
		s.mu.RUnlock()
		return nil, false
	}
	cp := *sess
	s.mu.RUnlock()
	if time.Since(cp.TouchedAt) > s.ttl {
		s.Delete(id)
		return nil, false
	}
	return &cp, true
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

// TryUseJTI atomically checks and marks a JTI as used.
// Returns true if the JTI was successfully claimed (first use).
// Returns false if already used. exp is the ticket expiry time
// used by GC to decide when the JTI record can be evicted.
func (s *SessionStore) TryUseJTI(jti string, exp time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.usedJTIs[jti]; ok {
		return false
	}
	s.usedJTIs[jti] = exp
	return true
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

// evictExpired removes all sessions whose TTL has elapsed and
// JTIs whose ticket expiry has passed.
func (s *SessionStore) evictExpired() {
	now := time.Now()
	s.mu.Lock()
	for id, sess := range s.sessions {
		if time.Since(sess.TouchedAt) > s.ttl {
			delete(s.sessions, id)
		}
	}
	for jti, exp := range s.usedJTIs {
		if now.After(exp) {
			delete(s.usedJTIs, jti)
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
