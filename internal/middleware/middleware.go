package middleware

import (
	"context"
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/google/uuid"
)

// Context key for request ID
type contextKey string

const requestIDKey contextKey = "request-id"

// LogRecoverMiddleware logs requests and recovers from panics
func LogRecoverMiddleware(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Generate request ID
			requestID := uuid.NewString()
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)

			// Add X-Request-ID header
			w.Header().Set("X-Request-ID", requestID)

			// Start timer
			start := time.Now()

			// Recover from panics
			defer func() {
				if rec := recover(); rec != nil {
					logger.Printf("PANIC recovered: %v\nStack: %s\n", rec, string(debugStack()))
				}
			}()

			// Create response writer that captures status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			defer func() {
				duration := time.Since(start)
				logger.Printf("%s %s %d %v\n", r.Method, r.URL.Path, wrapped.statusCode, duration)
			}()

			next.ServeHTTP(wrapped, r.WithContext(ctx))
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// debugStack returns the current stack trace
func debugStack() []byte {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	return buf[:n]
}

// RequestIDFromContext gets the request ID from context
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}
