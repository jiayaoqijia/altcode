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

	// Print the configuration
	printConfig(*port, *host, *verbose)
}

func printConfig(port int, host string, verbose bool) {
	fmt.Println("=== Configuration ===")
	fmt.Printf("Host:    %s\n", host)
	fmt.Printf("Port:    %d\n", port)
	fmt.Printf("Verbose: %v\n", verbose)
	fmt.Println("====================")

	if verbose {
		log.Println("Verbose mode enabled - additional logging would go here")
	}
}
