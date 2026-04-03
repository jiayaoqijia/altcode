package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ExampleUsage shows how to use the middleware.
func ExampleUsage() {
	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Hello, World!")
	})

	// Apply middleware in order: Recovery -> Logger -> RequestID
	// (Recovery is outermost to catch panics from any middleware below)
	server := Chain(
		handler,
		Recovery,
		Logger,
		RequestID,
	)

	// Use with http.Server
	_ = &http.Server{
		Addr:    ":8080",
		Handler: server,
	}
}

// TestMiddlewareChain tests the middleware chain.
func TestMiddlewareChain(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	server := Chain(handler, Recovery, Logger, RequestID)

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if rid := w.Header().Get("X-Request-ID"); rid == "" {
		t.Error("X-Request-ID header not set")
	}
}

// TestPanicRecovery tests that panics are caught.
func TestPanicRecovery(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	server := Chain(handler, Recovery)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

// TestStatusCodeCapture tests that status codes are captured correctly.
func TestStatusCodeCapture(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "Not Found")
	})

	server := Chain(handler, Logger)

	req := httptest.NewRequest("GET", "/notfound", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}
