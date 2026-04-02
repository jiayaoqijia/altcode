package internal

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Middleware wraps an HTTP handler with logging, request ID, and panic recovery.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate and set request ID
		requestID := uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Record start time
		start := time.Now()

		// Recover from panics
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[%s] PANIC: %v", requestID, err)
				wrapped.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(wrapped, "Internal Server Error")
			}
		}()

		// Log request details
		log.Printf("[%s] %s %s", requestID, r.Method, r.RequestURI)

		// Call next handler
		next.ServeHTTP(wrapped, r)

		// Log response with duration
		duration := time.Since(start)
		log.Printf("[%s] %s %s %d %v", requestID, r.Method, r.RequestURI, wrapped.statusCode, duration)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before writing headers.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write ensures headers are written if not already done.
func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}
