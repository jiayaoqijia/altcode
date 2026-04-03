package middleware

import (
	"fmt"
	"net/http"
)

// NewExampleServer creates an example HTTP server with middleware.
func NewExampleServer() *http.Server {
	mux := http.NewServeMux()

	// Define routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Welcome! Request ID: %s\n", w.Header().Get("X-Request-ID"))
	})

	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`)
	})

	mux.HandleFunc("/api/error", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Bad Request")
	})

	mux.HandleFunc("/api/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("intentional panic for testing")
	})

	// Apply middleware: Recovery -> Logger -> RequestID
	handler := Chain(
		mux,
		Recovery,
		Logger,
		RequestID,
	)

	return &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}
}
