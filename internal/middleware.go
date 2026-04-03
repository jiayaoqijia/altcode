package internal

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// LoggingMiddleware logs HTTP request method, path, and duration.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		duration := time.Since(start)

		next.ServeHTTP(w, r)

		duration = time.Since(start)
		log.Printf("%s %s %v", r.Method, r.URL.Path, duration)
	})
}

// RequestIDMiddleware adds a unique X-Request-ID header to each request.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

// RecoveryMiddleware recovers from panics and returns a 500 error.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("PANIC: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "Internal Server Error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Chain combines multiple middlewares in order.
// Usage: Chain(LoggingMiddleware, RequestIDMiddleware, RecoveryMiddleware)(handler)
func Chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}
