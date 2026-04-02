package main

import (
	"flag"
	"fmt"
)

type Config struct {
	Port    int
	Host    string
	Verbose bool
}

func main() {
	cfg := Config{}

	flag.IntVar(&cfg.Port, "port", 8080, "Server port")
	flag.StringVar(&cfg.Host, "host", "localhost", "Server host")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose output")

	flag.Parse()

	fmt.Printf("Config:\n")
	fmt.Printf("  Host:    %s\n", cfg.Host)
	fmt.Printf("  Port:    %d\n", cfg.Port)
	fmt.Printf("  Verbose: %t\n", cfg.Verbose)
}
