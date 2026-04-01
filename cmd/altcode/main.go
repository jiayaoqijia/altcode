package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
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

	cfg := config.Default()

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: key}
	}

	eng, err := engine.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	theme := tui.GetTheme(cfg.Theme)
	app := tui.New(eng, theme)

	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
