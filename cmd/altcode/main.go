package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/altcode-ai/altcode/internal/auth"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/mcp"
	"github.com/altcode-ai/altcode/internal/memory"
	"github.com/altcode-ai/altcode/internal/orchestrator"
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/altcode-ai/altcode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	var modelFlag, configFlag, themeFlag, sessionFlag string
	var jsonFlag, lastFlag, debugFlag bool

	root := &cobra.Command{
		Use:   "altcode [prompt]",
		Short: "AI-assisted coding CLI",
		Long: `altcode — AI-assisted coding CLI.

  altcode                    Interactive TUI mode
  altcode "prompt"           Run prompt headlessly, print response
  altcode --json "prompt"    Run prompt, emit JSONL events
  altcode --last             Resume last session in TUI
  altcode --last "prompt"    Resume last session with new prompt`,
		Version:      Version,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(modelFlag, configFlag, themeFlag)
			prompt := strings.Join(args, " ")
			if debugFlag {
				os.Setenv("ALTCODE_DEBUG", "1")
			}
			return run(cfg, prompt, jsonFlag, lastFlag, sessionFlag)
		},
	}

	root.PersistentFlags().StringVar(&modelFlag, "model", "", "Model override")
	root.PersistentFlags().StringVar(&configFlag, "config", "", "Config file path")
	root.PersistentFlags().StringVar(&themeFlag, "theme", "", "Theme name")
	root.Flags().BoolVar(&jsonFlag, "json", false, "Emit JSONL events (exec mode)")
	root.Flags().BoolVar(&lastFlag, "last", false, "Resume last session")
	root.Flags().StringVar(&sessionFlag, "session", "", "Resume session by ID")
	root.Flags().BoolVar(&debugFlag, "debug", false, "Print events to stderr for debugging")

	sessCmd := &cobra.Command{
		Use:   "sessions",
		Short: "List recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return listSessions()
		},
	}
	root.AddCommand(sessCmd)

	teamCmd := &cobra.Command{
		Use:   "team [prompt]",
		Short: "Run multi-AI orchestration — multiple models design/review/challenge together",
		Long: `Run a prompt through a team of AI models, each playing a role.
Configure your team in config.json under the "team" key.

Example:
  altcode team "Add rate limiting to the API"
  altcode team "Review the auth module for security issues"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(modelFlag, configFlag, themeFlag)
			prompt := strings.Join(args, " ")
			return runTeam(cfg, prompt)
		},
	}
	root.AddCommand(teamCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cfg *config.Config, prompt string, jsonMode, last bool, sessionID string) error {
	db, _ := store.Open("")
	defer func() {
		if db != nil {
			db.Close()
		}
	}()

	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)
	instructions, _ := config.LoadInstructions(projectRoot)

	// Load persistent memory (check both altcode and Claude Code dirs)
	memDir := memory.DefaultDir(projectRoot)
	if _, err := os.Stat(memDir); os.IsNotExist(err) {
		memDir = memory.ClaudeCodeDir(projectRoot)
	}
	memStore := memory.NewStore(memDir)

	params := engine.EngineParams{Config: cfg, Instructions: instructions, Memory: memStore}
	if err := loadSession(db, &params, last, sessionID); err != nil {
		return err
	}

	if prompt != "" {
		return runExec(params, prompt, jsonMode)
	}
	return runTUI(params)
}

func runExec(params engine.EngineParams, prompt string, jsonMode bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return exec.Run(ctx, exec.Params{
		EngineParams: params,
		Prompt:       prompt,
		JSON:         jsonMode,
	})
}

func runTUI(params engine.EngineParams) error {
	eng, err := engine.New(params)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	mcpCleanup := connectMCP(params.Config, eng)
	defer mcpCleanup()

	cmds := discoverCommands()

	theme := tui.GetTheme(params.Config.Theme)
	app := tui.New(eng, theme, Version, auth.MissingCredentialPrompt(params.Config), cmds...)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func connectMCP(cfg *config.Config, eng *engine.Engine) func() {
	if len(cfg.MCP) == 0 {
		return func() {}
	}
	ctx := context.Background()
	mgr := mcp.NewManager(ctx, cfg.MCP)
	mgr.RegisterAll(ctx, eng.Registry())
	return mgr.Close
}

func discoverCommands() []*command.Command {
	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)
	home, _ := os.UserHomeDir()

	dirs := []string{
		filepath.Join(home, ".claude", "commands"),
		filepath.Join(projectRoot, ".claude", "commands"),
	}
	cmds, _ := command.Discover(dirs...)
	return cmds
}

func loadSession(db *store.DB, params *engine.EngineParams, last bool, sessionID string) error {
	if db == nil {
		return nil
	}

	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)

	if last {
		sess, err := db.LatestSession(projectRoot)
		if err != nil {
			return fmt.Errorf("no previous session found")
		}
		sessionID = sess.ID
	}

	if sessionID != "" {
		msgs, err := db.ListMessages(sessionID)
		if err != nil {
			return fmt.Errorf("load session %s: %w", sessionID, err)
		}
		params.Store = db
		params.SessionID = sessionID
		params.Messages = store.ToProviderMessages(msgs)
	} else {
		sess, err := db.CreateSession(projectRoot, "", params.Config.Model)
		if err == nil {
			params.Store = db
			params.SessionID = sess.ID
		}
	}
	return nil
}

func runTeam(cfg *config.Config, prompt string) error {
	if cfg.Team == nil || len(cfg.Team.Models) == 0 {
		return fmt.Errorf("no team configured. Add a 'team' section to your config.json:\n\n" +
			"  {\"team\": {\"models\": {\n" +
			"    \"architect\": {\"model\": \"anthropic/claude-sonnet-4-20250514\"},\n" +
			"    \"reviewer\":  {\"model\": \"openai/gpt-5.4\"}\n" +
			"  }}}")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	session := orchestrator.NewSessionFromConfig(cfg.Team, cfg)
	fmt.Printf("Running team '%s' with %d models...\n\n", teamName(cfg.Team), len(cfg.Team.Models))

	// Phase 1: parallel execution
	findings, err := session.RunParallel(ctx, prompt)
	if err != nil {
		return err
	}
	for _, f := range findings {
		fmt.Printf("[%s / %s] %s\n", f.Model, f.Role, truncateMain(f.Content, 200))
		fmt.Println()
	}

	// Phase 2: cross-check
	fmt.Println("--- Cross-checking findings ---\n")
	crossFindings, _ := session.CrossCheck(ctx)
	for _, f := range crossFindings {
		fmt.Printf("[%s / %s cross-check] %s\n", f.Model, f.Role, truncateMain(f.Content, 200))
		fmt.Println()
	}

	// Phase 3: synthesize verdict
	verdict := session.Synthesize()
	fmt.Printf("=== VERDICT: %s (%.0f%% agreement) ===\n", verdict.Decision, verdict.Agreement*100)
	return nil
}

func teamName(t *config.TeamConfig) string {
	if t.Name != "" {
		return t.Name
	}
	return "default"
}

func truncateMain(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func listSessions() error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	sessions, err := db.ListSessions()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	for _, s := range sessions {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Printf("%-28s  %-20s  %s  %s\n",
			s.ID, title, s.Model, s.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func loadConfig(modelFlag, configFlag, themeFlag string) *config.Config {
	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)

	cfg := config.Default()

	for _, path := range userConfigPaths() {
		tryMerge(cfg, path)
	}
	tryMerge(cfg, filepath.Join(projectRoot, ".altcode", "config.json"))
	if configFlag != "" {
		tryMerge(cfg, configFlag)
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
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		if p, ok := cfg.Provider["openai"]; !ok || p.APIKey == "" {
			cfg.Provider["openai"] = config.ProviderConfig{APIKey: key}
		}
	}

	// Auto-detect credentials from Claude Code and Codex CLI installs
	auth.LoadFromCLIs(cfg)

	return cfg
}

func userConfigPaths() []string {
	paths := []string{auth.UserConfigPath()}

	for _, legacyPath := range auth.LegacyUserConfigPaths() {
		if legacyPath != paths[0] {
			paths = append(paths, legacyPath)
		}
	}

	return paths
}

func tryMerge(base *config.Config, path string) {
	if overlay, err := config.LoadFile(path); err == nil {
		mergeConfig(base, overlay)
	}
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
	if overlay.Team != nil {
		base.Team = overlay.Team
	}
}
