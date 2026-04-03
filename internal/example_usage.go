package internal

import (
	"fmt"
	"net/http"
)

// ExampleHandler is a sample HTTP handler.
func ExampleHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %s!", r.URL.Path)
}

// PanicHandler demonstrates the panic recovery middleware.
func PanicHandler(w http.ResponseWriter, r *http.Request) {
	panic("something went wrong")
}

// ExampleServer shows how to set up HTTP routes with middleware.
func ExampleServer() *http.ServeMux {
	mux := http.NewServeMux()

	// Apply middleware using Chain
	handler := Chain(
		RecoveryMiddleware,    // Outermost - catches panics
		RequestIDMiddleware,   // Adds request ID
		LoggingMiddleware,     // Innermost - logs the request
	)(http.HandlerFunc(ExampleHandler))

	panicHandler := Chain(
		RecoveryMiddleware,
		RequestIDMiddleware,
		LoggingMiddleware,
	)(http.HandlerFunc(PanicHandler))

	mux.Handle("/", handler)
	mux.Handle("/panic", panicHandler)

	return mux
}
