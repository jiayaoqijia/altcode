package main

import (
	"flag"
	"fmt"
)

func main() {
	port := flag.Int("port", 8080, "Port number")
	host := flag.String("host", "localhost", "Host address")
	verbose := flag.Bool("verbose", false, "Enable verbose output")

	flag.Parse()

	fmt.Println("Configuration:")
	fmt.Printf("  Host:    %s\n", *host)
	fmt.Printf("  Port:    %d\n", *port)
	fmt.Printf("  Verbose: %v\n", *verbose)
}
