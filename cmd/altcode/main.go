package main

import (
	"fmt"
	"os"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	var modelFlag, configFlag, themeFlag string

	root := &cobra.Command{
		Use:     "altcode",
		Short:   "AI-assisted coding CLI",
		Version: Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(modelFlag, configFlag, themeFlag)
		},
		SilenceUsage: true,
	}

	root.Flags().StringVar(&modelFlag, "model", "", "Model (e.g. anthropic/claude-sonnet-4-20250514)")
	root.Flags().StringVar(&configFlag, "config", "", "Config file path")
	root.Flags().StringVar(&themeFlag, "theme", "", "Theme name")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(modelFlag, configFlag, themeFlag string) error {
	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)

	cfg := config.Default()

	home, _ := os.UserHomeDir()
	if userCfg, err := config.LoadFile(home + "/.config/altcode/config.json"); err == nil {
		mergeConfig(cfg, userCfg)
	}
	if projCfg, err := config.LoadFile(projectRoot + "/.altcode/config.json"); err == nil {
		mergeConfig(cfg, projCfg)
	}
	if configFlag != "" {
		if fileCfg, err := config.LoadFile(configFlag); err == nil {
			mergeConfig(cfg, fileCfg)
		}
	}

	if modelFlag != "" {
		cfg.Model = modelFlag
	}
	if themeFlag != "" {
		cfg.Theme = themeFlag
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		if p, ok := cfg.Provider["anthropic"]; !ok || p.APIKey == "" {
			cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: key}
		}
	}

	eng, err := engine.New(cfg)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	theme := tui.GetTheme(cfg.Theme)
	app := tui.New(eng, theme)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

func mergeConfig(base, overlay *config.Config) {
	if overlay.Model != "" && overlay.Model != config.DefaultModel {
		base.Model = overlay.Model
	}
	if overlay.Theme != "" && overlay.Theme != "default" {
		base.Theme = overlay.Theme
	}
	for k, v := range overlay.Provider {
		base.Provider[k] = v
	}
	base.Permission = append(base.Permission, overlay.Permission...)
	for k, v := range overlay.MCP {
		base.MCP[k] = v
	}
	for k, v := range overlay.Agent {
		base.Agent[k] = v
	}
}
