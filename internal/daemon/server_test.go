package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAuthMiddleware_RejectsNoToken(t *testing.T) {
	handler := authMiddleware("secret-token")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_AcceptsValidToken(t *testing.T) {
	handler := authMiddleware("secret-token")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsWrongToken(t *testing.T) {
	handler := authMiddleware("secret-token")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_HealthBypassesAuth(t *testing.T) {
	handler := authMiddleware("secret-token")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("health should bypass auth, got %d", rec.Code)
	}
}

func TestAuthMiddleware_MetricsBypassesAuth(t *testing.T) {
	handler := authMiddleware("secret-token")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("metrics should bypass auth, got %d", rec.Code)
	}
}

func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	handler := recoveryMiddleware(logger)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		}),
	)
	req := httptest.NewRequest("GET", "/boom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 500 {
		t.Errorf("expected 500 on panic, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		// http.Error writes "text/plain" with trailing newline; parse
		// the JSON portion.
		t.Logf("body: %q", rec.Body.String())
	}
}

func TestRecoveryMiddleware_PassesThrough(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	handler := recoveryMiddleware(logger)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/ok", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	handler := requestIDMiddleware()(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Error("expected X-Request-ID to be generated")
	}
	if len(id) != 8 {
		t.Errorf("expected 8-char request ID, got %d chars: %q", len(id), id)
	}
}

func TestRequestIDMiddleware_PreservesExistingID(t *testing.T) {
	handler := requestIDMiddleware()(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-custom-id")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if id != "my-custom-id" {
		t.Errorf("expected preserved request ID, got %q", id)
	}
}

func TestServer_StoreAccessor(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Port:    0,
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer s.Store().Close()

	if s.Store() == nil {
		t.Error("Store() should not return nil")
	}
}

func TestServer_MiddlewareChain(t *testing.T) {
	s, err := NewServer(ServerConfig{
		Port:      0,
		DataDir:   t.TempDir(),
		AuthToken: "tok",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer s.store.Close()

	handler := s.middleware()

	// Unauthenticated request to /tasks should be rejected.
	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	// Should still have X-Request-ID from request ID middleware.
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header from middleware chain")
	}
}
