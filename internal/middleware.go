package internal

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Middleware is a function that wraps an HTTP handler
type Middleware func(http.Handler) http.Handler

// Chain applies multiple middleware to a handler in order
func Chain(h http.Handler, middleware ...Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

// Logger logs HTTP requests with method, path, and duration
func Logger(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		h.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		log.Printf("%s %s %d %v",
			r.Method,
			r.RequestURI,
			wrapped.statusCode,
			duration,
		)
	})
}

// RequestID adds a unique X-Request-ID header to each request
func RequestID(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)
		h.ServeHTTP(w, r)
	})
}

// Recover recovers from panics and returns 500
func Recover(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Header().Set("Content-Type", "application/json")
				log.Printf("PANIC: %v", err)
				fmt.Fprintf(w, `{"error":"internal server error"}`)
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == http.StatusOK {
		rw.statusCode = http.StatusOK
	}
	return rw.ResponseWriter.Write(b)
}
