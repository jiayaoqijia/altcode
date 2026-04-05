package main

import (
	"flag"
	"fmt"
)

func main() {
	// Define flags
	port := flag.Int("port", 8080, "port number to listen on")
	host := flag.String("host", "localhost", "host address to bind to")
	verbose := flag.Bool("verbose", false, "enable verbose output")

	// Parse command-line flags
	flag.Parse()

	// Print the configuration
	fmt.Println("=== Configuration ===")
	fmt.Printf("Host:    %s\n", *host)
	fmt.Printf("Port:    %d\n", *port)
	fmt.Printf("Verbose: %v\n", *verbose)
}
