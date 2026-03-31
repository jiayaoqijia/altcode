package main

import (
	"flag"
	"fmt"
	"os"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("altcode %s\n", Version)
		os.Exit(0)
	}

	fmt.Println("altcode — AI-assisted coding CLI")
}
