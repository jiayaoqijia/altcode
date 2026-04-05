package main

import (
	"flag"
	"fmt"
	"log"
)

func main() {
	// Define command-line flags
	port := flag.Int("port", 8080, "Port number to listen on")
	host := flag.String("host", "localhost", "Host address to bind to")
	verbose := flag.Bool("verbose", false, "Enable verbose output")

	// Parse command-line arguments
	flag.Parse()

	// Validate port number
	if *port < 1 || *port > 65535 {
		log.Fatalf("Error: port must be between 1 and 65535, got %d\n", *port)
	}

	// Print configuration
	fmt.Println("=== Configuration ===")
	fmt.Printf("Host:    %s\n", *host)
	fmt.Printf("Port:    %d\n", *port)
	fmt.Printf("Verbose: %v\n", *verbose)
	fmt.Println("====================")

	// Additional verbose output if enabled
	if *verbose {
		fmt.Println("\n[VERBOSE] Configuration details:")
		fmt.Printf("[VERBOSE] Server will listen on %s:%d\n", *host, *port)
		fmt.Println("[VERBOSE] Ready to start server")
	}
}
