package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/auth"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/mcp"
	"github.com/altcode-ai/altcode/internal/memory"
	"github.com/altcode-ai/altcode/internal/orchestrator"
	"github.com/altcode-ai/altcode/internal/plugin"
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/altcode-ai/altcode/internal/tui"
	"github.com/altcode-ai/altcode/internal/workflow"
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
	root.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Print events to stderr for debugging")

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

	var wfMode string
	var wfMaxIter int
	workflowCmd := &cobra.Command{
		Use:   "workflow [prompt]",
		Short: "Structured workflow mode — interview, plan, or persistent execution",
		Long: `Run a structured workflow pipeline inspired by oh-my-codex patterns.

  altcode workflow "add auth"                      Auto-detect mode from keywords
  altcode workflow --mode interview "add auth"     Socratic clarification first
  altcode workflow --mode plan "add auth"           Consensus planning only
  altcode workflow --mode ralph "add auth"          Persistent loop until complete
  altcode workflow --mode ralph --max-iter 5 "fix"  Limit iterations

Keywords auto-route: "$interview", "clarify", "$plan", "$ralph", "don't stop"

Classic "altcode" behavior is completely unaffected by this subcommand.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(modelFlag, configFlag, themeFlag)
			prompt := strings.Join(args, " ")
			if debugFlag {
				os.Setenv("ALTCODE_DEBUG", "1")
			}
			return runWorkflow(cfg, prompt, wfMode, wfMaxIter)
		},
	}
	workflowCmd.Flags().StringVar(&wfMode, "mode", "", "Workflow mode: interview, plan, ralph, execute")
	workflowCmd.Flags().IntVar(&wfMaxIter, "max-iter", 10, "Max iterations for ralph mode")
	root.AddCommand(workflowCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cfg *config.Config, prompt string, jsonMode, last bool, sessionID string) error {
	// Skip SQLite in exec mode when no session resume needed — saves ~5-10ms
	needsDB := last || sessionID != "" || prompt == ""
	var db *store.DB
	if needsDB {
		db, _ = store.Open("")
	}
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

	hooksRunner := buildHooksRunner(cfg)
	skills := discoverSkills()
	agents := discoverAgents()

	// Merge agent descriptions into skills list so the model knows about them
	for _, a := range agents {
		skills = append(skills, engine.Skill{
			Name:        a.Name + " (agent)",
			Description: a.Description,
			Path:        a.Path,
		})
	}

	params := engine.EngineParams{
		Config:       cfg,
		Instructions: instructions,
		Memory:       memStore,
		Hooks:        hooksRunner,
		Skills:       skills,
	}
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

	eng, err := engine.New(params)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	// Only start MCP servers if prompt likely needs them.
	// MCP startup adds 1-5s of blocking latency per server.
	var mcpCleanup func()
	if needsMCP(prompt) {
		mcpCleanup = connectMCP(params.Config, eng)
	}
	if mcpCleanup != nil {
		defer mcpCleanup()
	}

	return exec.Run(ctx, exec.Params{
		Engine: eng,
		Prompt: prompt,
		JSON:   jsonMode,
	})
}

// needsMCP returns true if the prompt likely requires MCP tools.
func needsMCP(prompt string) bool {
	lower := strings.ToLower(prompt)
	return strings.Contains(lower, "mcp") ||
		strings.Contains(lower, "playwright") ||
		strings.Contains(lower, "browser") ||
		strings.Contains(lower, "language-server") ||
		strings.Contains(lower, "lsp") ||
		strings.Contains(lower, "chrome")
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

func discoverAgents() []*agent.Agent {
	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)
	home, _ := os.UserHomeDir()

	dirs := []string{
		filepath.Join(projectRoot, ".agents", "skills"),
		filepath.Join(projectRoot, ".claude", "agents"),
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".claude", "agents"),
		)
	}
	agents, _ := agent.Discover(dirs...)
	return agents
}

func discoverSkills() []engine.Skill {
	cmds := discoverCommands()
	skills := make([]engine.Skill, len(cmds))
	for i, c := range cmds {
		skills[i] = engine.Skill{Name: c.Name, Description: c.Description, Path: c.Path}
	}
	return skills
}

func discoverCommands() []*command.Command {
	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)
	home, _ := os.UserHomeDir()

	dirs := []string{
		// Claude Code commands (flat .md files)
		filepath.Join(home, ".claude", "commands"),
		filepath.Join(projectRoot, ".claude", "commands"),
		// Claude Code skills (nested SKILL.md dirs)
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(projectRoot, ".claude", "skills"),
		// Agent skills (nested SKILL.md dirs)
		filepath.Join(projectRoot, ".agents", "skills"),
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

func runWorkflow(cfg *config.Config, prompt, modeFlag string, maxIter int) error {
	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)
	instructions, _ := config.LoadInstructions(projectRoot)

	memDir := memory.DefaultDir(projectRoot)
	if _, err := os.Stat(memDir); os.IsNotExist(err) {
		memDir = memory.ClaudeCodeDir(projectRoot)
	}
	memStore := memory.NewStore(memDir)

	hooksRunner := buildHooksRunner(cfg)
	skills := discoverSkills()

	params := engine.EngineParams{
		Config:       cfg,
		Instructions: instructions,
		Memory:       memStore,
		Hooks:        hooksRunner,
		Skills:       skills,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var mode workflow.Mode
	if modeFlag != "" {
		mode = workflow.Mode(modeFlag)
	}

	return workflow.Run(ctx, workflow.RunParams{
		EngineParams: params,
		ProjectRoot:  projectRoot,
		Mode:         mode,
		Prompt:       prompt,
		MaxIter:      maxIter,
	})
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
	fmt.Println("--- Cross-checking findings ---")
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

	// Load .claude/settings.json (permissions + hooks for Claude Code compat)
	loadClaudeSettings(cfg, projectRoot)

	// Load .mcp.json (Claude Code MCP server format)
	loadMCPJSON(cfg, projectRoot)

	// Auto-discover and merge plugins
	loadPlugins(cfg, projectRoot)

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

	// Model flag takes highest priority — apply after auth detection
	// so Codex's config.toml model doesn't override an explicit --model
	if modelFlag != "" {
		cfg.Model = modelFlag
	}

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
	for k, v := range overlay.Hooks {
		base.Hooks[k] = append(base.Hooks[k], v...)
	}
	if overlay.Team != nil {
		base.Team = overlay.Team
	}
}

// buildHooksRunner converts config hook entries into a hooks.Runner.
func buildHooksRunner(cfg *config.Config) *hooks.Runner {
	if len(cfg.Hooks) == 0 {
		return hooks.NewRunner(nil)
	}
	converted := make(map[hooks.Event][]hooks.MatcherConfig, len(cfg.Hooks))
	for eventName, matchers := range cfg.Hooks {
		ev := hooks.Event(eventName)
		for _, m := range matchers {
			mc := hooks.MatcherConfig{Matcher: m.Matcher}
			for _, h := range m.Hooks {
				mc.Hooks = append(mc.Hooks, hooks.EntryConfig{
					Type:    h.Type,
					Command: h.Command,
					Timeout: h.Timeout,
				})
			}
			converted[ev] = append(converted[ev], mc)
		}
	}
	return hooks.NewRunner(converted)
}

// loadClaudeSettings reads .claude/settings.json for Claude Code compat.
// Extracts permissions (as allow rules) and hooks.
func loadClaudeSettings(cfg *config.Config, projectRoot string) {
	paths := []string{
		filepath.Join(projectRoot, ".claude", "settings.json"),
		filepath.Join(projectRoot, ".claude", "settings.local.json"),
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		paths = append([]string{filepath.Join(home, ".claude", "settings.json")}, paths...)
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var settings struct {
			Permissions struct {
				Allow []string `json:"allow"`
				Deny  []string `json:"deny"`
			} `json:"permissions"`
			Hooks map[string][]config.HookMatcherConfig `json:"hooks"`
		}
		if json.Unmarshal(data, &settings) != nil {
			continue
		}
		for _, pattern := range settings.Permissions.Allow {
			cfg.Permission = append(cfg.Permission, config.PermissionRule{
				Tool: pattern, Action: "allow",
			})
		}
		for _, pattern := range settings.Permissions.Deny {
			cfg.Permission = append(cfg.Permission, config.PermissionRule{
				Tool: pattern, Action: "deny",
			})
		}
		for k, v := range settings.Hooks {
			cfg.Hooks[k] = append(cfg.Hooks[k], v...)
		}
	}
}

// loadMCPJSON reads .mcp.json (Claude Code format) and merges into config.
func loadMCPJSON(cfg *config.Config, projectRoot string) {
	paths := []string{
		filepath.Join(projectRoot, ".mcp.json"),
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		paths = append(paths, filepath.Join(home, ".claude", ".mcp.json"))
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var mcpFile struct {
			MCPServers map[string]config.MCPServerConfig `json:"mcpServers"`
		}
		if json.Unmarshal(data, &mcpFile) != nil {
			continue
		}
		for name, srv := range mcpFile.MCPServers {
			if _, exists := cfg.MCP[name]; !exists {
				cfg.MCP[name] = srv
			}
		}
	}
}

// loadPlugins discovers plugins from standard directories and merges them.
func loadPlugins(cfg *config.Config, projectRoot string) {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(projectRoot, ".altcode", "plugins"),
		filepath.Join(projectRoot, ".claude", "plugins"),
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".config", "altcode", "plugins"),
			filepath.Join(home, ".claude", "plugins"),
		)
	}
	plugins, _ := plugin.Discover(dirs...)
	for _, p := range plugins {
		p.Merge(cfg)
	}
}
