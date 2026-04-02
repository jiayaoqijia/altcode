package middleware

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// ExampleUsage demonstrates how to use the middleware.
func ExampleUsage() {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!\n"))
	})

	// Apply middleware in order: recovery -> request ID -> logger
	wrapped := Chain(handler,
		Recovery(logger),
		RequestID("X-Request-ID"),
		Logger(logger),
	)

	// Use the wrapped handler in a server
	server := httptest.NewServer(wrapped)
	defer server.Close()

	// Make a request
	resp, _ := http.Get(server.URL + "/api/users")
	resp.Body.Close()
}

// TestPanicRecovery verifies that panics are recovered.
func TestPanicRecovery(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	wrapped := Recovery(logger)(handler)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

// TestRequestID verifies that X-Request-ID is added.
func TestRequestID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := RequestID("X-Request-ID")(handler)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	id := w.Header().Get("X-Request-ID")
	if id == "" {
		t.Error("expected X-Request-ID header to be set")
	}
}

// TestLogger verifies that requests are logged.
func TestLogger(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := Logger(logger)(handler)

	req := httptest.NewRequest("POST", "/api/users", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// TestChain verifies that middleware are applied in correct order.
func TestChain(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request ID is set
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			t.Error("expected X-Request-ID in request context")
		}
		w.WriteHeader(http.StatusOK)
	})

	wrapped := Chain(handler,
		Recovery(logger),
		RequestID("X-Request-ID"),
		Logger(logger),
	)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if w.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header in response")
	}
}
