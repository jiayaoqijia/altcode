package internal

import (
	"fmt"
	"net/http"
)

// ExampleMain demonstrates middleware usage.
// To use: uncomment and call from main()
func ExampleMain() {
	// Create a simple handler
	helloHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, World!\n")
	})

	// Create a handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("intentional panic for testing")
	})

	// Apply middleware
	mux := http.NewServeMux()
	mux.Handle("/", Middleware(helloHandler))
	mux.Handle("/panic", Middleware(panicHandler))

	// Start server
	http.ListenAndServe(":8080", mux)
}
