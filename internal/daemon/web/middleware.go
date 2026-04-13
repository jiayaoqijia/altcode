package web

import (
	"context"
	"crypto/subtle"
	"net/http"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

// sessionContextKey stores the authenticated session in the request context.
const sessionContextKey contextKey = iota

// withSession returns a new context carrying the session.
func withSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, s)
}

// GetSession retrieves the authenticated session from the request
// context. Returns nil if no session is present.
func GetSession(r *http.Request) *Session {
	s, _ := r.Context().Value(sessionContextKey).(*Session)
	return s
}

// RequireAuth returns middleware that validates the altfix_session
// cookie. On success it touches the session, stores it in the request
// context, and calls the next handler. On failure it redirects to the
// login page.
func RequireAuth(sessions *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("altfix_session")
			if err != nil {
				http.Redirect(w, r, "/ui/login", http.StatusFound)
				return
			}
			sess, ok := sessions.Get(cookie.Value)
			if !ok || !sess.Authenticated {
				http.Redirect(w, r, "/ui/login", http.StatusFound)
				return
			}
			sessions.Touch(sess.ID)
			ctx := withSession(r.Context(), sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin returns middleware that checks whether the
// authenticated user is an admin. Returns 403 if not.
func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := GetSession(r)
			if sess == nil || sess.User == nil || !sess.User.IsAdmin {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFCheck returns middleware that validates the CSRF token on
// state-changing requests (anything other than GET, HEAD, OPTIONS).
// The token is read from the X-CSRF-Token header or the _csrf form
// field and compared against the session's CSRFToken using
// constant-time comparison.
func CSRFCheck() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			sess := GetSession(r)
			if sess == nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				token = r.FormValue("_csrf")
			}

			expected := sess.CSRFToken
			if len(token) == 0 ||
				subtle.ConstantTimeCompare(
					[]byte(token), []byte(expected),
				) != 1 {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
