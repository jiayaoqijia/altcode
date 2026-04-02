package middleware

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RequestIDKey is the context key for the request ID
type RequestIDKey struct{}

// RequestIDMiddleware adds X-Request-ID header, logs request details, and recovers panics
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate and add request ID
		requestID := uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)

		// Store request ID in context
		ctx := context.WithValue(r.Context(), RequestIDKey{}, requestID)
		r = r.WithContext(ctx)

		// Log request start
		start := time.Now()
		log.Printf("Request started: %s %s", r.Method, r.URL.Path)

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{w: w}

		defer func() {
			// Recover from panics
			if rec := recover(); rec != nil {
				log.Printf("Panic recovered: %v", rec)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}

			// Log request completion
			duration := time.Since(start)
			log.Printf("Request completed: %s %s - Status: %d - Duration: %v",
				r.Method, r.URL.Path, wrapped.statusCode, duration)
		}()

		next.ServeHTTP(wrapped, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	w          http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.w.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	return rw.w.Write(b)
}

func (rw *responseWriter) Header() http.Header {
	return rw.w.Header()
}

// GetRequestID retrieves the request ID from context
func GetRequestID(ctx context.Context) string {
	if rid, ok := ctx.Value(RequestIDKey{}).(string); ok {
		return rid
	}
	return ""
}
