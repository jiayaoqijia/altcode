package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/auth"
	"github.com/altcode-ai/altcode/internal/oauth"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/mcp"
	"github.com/altcode-ai/altcode/internal/memory"
	"github.com/altcode-ai/altcode/internal/orchestrator"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/plugin"
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/altcode-ai/altcode/internal/tui"
	"github.com/altcode-ai/altcode/internal/workflow"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// cliFlags bundles exec-mode flags so run() / runExec() don't grow
// a parameter list per phase. Each Cobra RunE populates one.
type cliFlags struct {
	// Phase 0 (pre-existing)
	jsonMode  bool
	last      bool
	sessionID string

	// Phase 1: output format + observability
	outputFormat   string
	verbose        bool
	quiet          bool
	printCost      bool
	printTools     bool
	printTree      bool
	showSystem     bool
	saveTranscript string
	saveCost       string
	saveDiff       string

	// Phase 2: permission / mode
	permissionMode string
	allowTools     []string
	denyTools      []string
	dryRun         bool

	// Phase 4: session / history
	continueSession bool   // --continue, CC-compat alias for --last
	forkSession     string // --fork-session <id>, branch off past session
	sessionDB       string // --session-db, override ~/.altcode/sessions.db path
	listSessions    bool   // --list-sessions, print + exit

	// Phase 5: input
	images     []string // --image <path> (repeatable); "-" = stdin
	files      []string // --file <path>  (repeatable); context injection
	promptFile string   // --prompt-file <path>; "-" = stdin
	system     string   // --system <text>
	systemFile string   // --system-file <path>
}

func main() {
	var modelFlag, configFlag, themeFlag string
	var debugFlag bool
	var flags cliFlags

	root := &cobra.Command{
		Use:   "altcode [prompt]",
		Short: "AI-assisted coding CLI",
		Long: `altcode — AI-assisted coding CLI.

  altcode                           Interactive TUI mode
  altcode "prompt"                  Run prompt headlessly, print response
  altcode --output-format json "p"  Run prompt, emit single JSON object
  altcode --output-format diff "p"  Run prompt, print final diff only
  altcode --json "prompt"           Alias for --output-format stream-json
  altcode --last                    Resume last session in TUI
  altcode --last "prompt"           Resume last session with new prompt`,
		Version:      Version,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := loadConfig(modelFlag, configFlag, themeFlag)
			prompt := strings.Join(args, " ")
			if debugFlag {
				os.Setenv("ALTCODE_DEBUG", "1")
			}
			if err := run(cfg, prompt, flags); err != nil {
				// Top-level UsageError → print + exit AFTER all
				// deferred cleanup (MCP teardown etc.) has run,
				// which happens inside run() before the return.
				// See spec v7 — os.Exit inside runExec would leak
				// subprocess-backed MCP servers.
				var uerr *exec.UsageError
				if errors.As(err, &uerr) {
					fmt.Fprintln(os.Stderr, "altcode:", uerr.Msg)
					os.Exit(uerr.ExitCode)
				}
				return err
			}
			return nil
		},
	}

	root.PersistentFlags().StringVar(&modelFlag, "model", "", "Model override")
	root.PersistentFlags().StringVar(&configFlag, "config", "", "Config file path")
	root.PersistentFlags().StringVar(&themeFlag, "theme", "", "Theme name")
	root.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Print events to stderr for debugging")

	// --- Phase 0: session + json alias ---
	root.Flags().BoolVar(&flags.jsonMode, "json", false, "Emit JSONL events (alias for --output-format stream-json)")
	root.Flags().BoolVar(&flags.last, "last", false, "Resume last session")
	root.Flags().StringVar(&flags.sessionID, "session", "", "Resume session by ID")

	// --- Phase 1: output format + observability ---
	root.Flags().StringVar(&flags.outputFormat, "output-format", "",
		"Output shape: text (default) | json | stream-json | diff")
	root.Flags().BoolVar(&flags.verbose, "verbose", false,
		"Include tool args + thinking blocks in text mode")
	root.Flags().BoolVar(&flags.quiet, "quiet", false,
		"Suppress banner, tool chatter, and trailing newline")
	root.Flags().BoolVar(&flags.printCost, "print-cost", false,
		"Print cost + timing summary to stderr at end of run")
	root.Flags().BoolVar(&flags.printTools, "print-tools", false,
		"Log each tool call to stderr (forces on even when not a TTY)")
	root.Flags().BoolVar(&flags.printTree, "print-tree", false,
		"Print end-of-run ASCII tool tree to stderr (Phase 12 — accepted but flat)")
	root.Flags().BoolVar(&flags.showSystem, "show-system", false,
		"Print the system prompt to stderr at start (debugging aid)")
	root.Flags().StringVar(&flags.saveTranscript, "save-transcript", "",
		"Write full JSONL transcript to file (Phase 7)")
	root.Flags().StringVar(&flags.saveCost, "save-cost", "",
		"Write cost report (JSON) to file (Phase 7)")
	root.Flags().StringVar(&flags.saveDiff, "save-diff", "",
		"Write final unified diff to file (Phase 7)")

	// --- Phase 2: permission / mode ---
	root.Flags().StringVar(&flags.permissionMode, "permission-mode", "",
		"Permission mode: plan | auto | default | bypass")
	root.Flags().StringArrayVar(&flags.allowTools, "allow-tool", nil,
		"Allow a tool [name] or [name:pattern] for this session (repeatable)")
	root.Flags().StringArrayVar(&flags.denyTools, "deny-tool", nil,
		"Deny a tool [name] or [name:pattern] for this session (repeatable)")
	root.Flags().BoolVar(&flags.dryRun, "dry-run", false,
		"Alias for --permission-mode plan (read-only, no writes)")

	// --- Phase 5: input ---
	root.Flags().StringArrayVar(&flags.images, "image", nil,
		"Attach image file (repeatable). Path or '-' for stdin. "+
			"Anthropic models only in v1.")
	root.Flags().StringArrayVar(&flags.files, "file", nil,
		"Inject file contents as pre-loaded prompt context (repeatable)")
	root.Flags().StringVar(&flags.promptFile, "prompt-file", "",
		"Read prompt from file (or '-' for stdin)")
	root.Flags().StringVar(&flags.system, "system", "",
		"Append a string to the system prompt")
	root.Flags().StringVar(&flags.systemFile, "system-file", "",
		"Append a file's contents to the system prompt")

	// --- Phase 4: session / history ---
	root.Flags().BoolVar(&flags.continueSession, "continue", false,
		"Resume most recent session (CC-compat alias for --last)")
	root.Flags().StringVar(&flags.forkSession, "fork-session", "",
		"Branch off a past session into a new one (by session ID)")
	// Named --session-db (not --session-dir) because store.Open takes
	// a FILE path, not a directory. Using --session-dir would mislead
	// users into passing a directory that becomes a SQLite file named
	// after the last path segment. Spec v7 called this a BLOCKER from
	// the Phase 4 review.
	root.PersistentFlags().StringVar(&flags.sessionDB, "session-db", "",
		"Override sessions.db file path (default: XDG/platform data dir)")
	root.Flags().BoolVar(&flags.listSessions, "list-sessions", false,
		"Print sessions and exit (alias for `altcode sessions`)")

	// Cobra mutex: the session-entry flags each pick a different
	// strategy and can't combine cleanly. --continue and --last
	// mean the same thing (continue is a CC-compat alias), but
	// we exclude them from each other so users don't accidentally
	// set both and then wonder why one is ignored.
	root.MarkFlagsMutuallyExclusive("continue", "last")
	root.MarkFlagsMutuallyExclusive("continue", "session", "fork-session")
	root.MarkFlagsMutuallyExclusive("last", "session", "fork-session")

	// Cobra-enforced mutual exclusion (flag presence only — value-dependent
	// rules like --prompt-file - vs --image - live in exec.Params.Validate).
	//
	// Intentionally NOT adding `permission-mode + dry-run` here:
	// ApplyPermissionOverrides is already doing "explicit mode wins"
	// with dry-run as a fallback alias, and that logic is reached both
	// from the CLI and from config-file PermissionMode. Adding a Cobra
	// mutex here would make the helper's precedence branch unreachable
	// from the CLI (dead code) while leaving the config-file case open.
	// Let the helper be the single source of truth.
	root.MarkFlagsMutuallyExclusive("quiet", "verbose")
	root.MarkFlagsMutuallyExclusive("quiet", "show-system")

	sessCmd := &cobra.Command{
		Use:   "sessions",
		Short: "List recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Honor --session-db from root PersistentFlags so the
			// subcommand and `--list-sessions` root-flag shortcut
			// read from the same database. Without inheriting, the
			// subcommand silently fell back to the default path.
			return listSessionsFromDB(flags.sessionDB)
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

	addWorkspaceCmd(root)

	// Provider-specific login commands under "altcode login <provider>"
	loginRoot := &cobra.Command{
		Use:   "login <provider>",
		Short: "Log in with a provider subscription",
		Long: `Log in to use a provider's subscription directly.

  altcode login codex              ChatGPT subscription (browser OAuth)
  altcode login codex --no-browser Print URL only (useful over SSH)

More providers will be added (e.g. altcode login claude, altcode login altllm).`,
		Args: cobra.MinimumNArgs(1),
	}

	var loginBrowser bool
	codexLogin := &cobra.Command{
		Use:     "codex",
		Aliases: []string{"chatgpt", "openai"},
		Short:   "Log in with ChatGPT/Codex subscription",
		Long: `Login methods:
  altcode login codex            Device code flow (default — works everywhere)
  altcode login codex --browser  Browser OAuth (opens a browser on desktop)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			path := oauth.DefaultAuthFile()

			if loginBrowser {
				_, err := oauth.Login(ctx, path, oauth.LoginOptions{
					OpenBrowser: true,
					Stdout:      os.Stdout,
				})
				return err
			}
			// Default: device code flow (works over SSH, no port binding needed)
			// Falls back to browser OAuth if device code is disabled on the account
			err := runDeviceCodeLogin(ctx, path)
			if err != nil && strings.Contains(err.Error(), "not enabled") {
				fmt.Fprintln(os.Stderr, "Device code login not enabled. Falling back to browser OAuth...")
				_, err = oauth.Login(ctx, path, oauth.LoginOptions{
					OpenBrowser: true,
					Stdout:      os.Stdout,
				})
			}
			return err
		},
	}
	codexLogin.Flags().BoolVar(&loginBrowser, "browser", false, "Use browser OAuth instead of device code")
	// loginDeviceCode flag removed — device code is now the default
	loginRoot.AddCommand(codexLogin)
	root.AddCommand(loginRoot)

	logoutRoot := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored login credentials",
		Long: `Remove stored credentials for a provider.

  altcode logout codex    Remove ChatGPT/Codex credentials
  altcode logout          Remove all altcode credentials`,
	}
	codexLogout := &cobra.Command{
		Use:     "codex",
		Aliases: []string{"chatgpt", "openai"},
		Short:   "Remove ChatGPT/Codex credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oauth.Logout(oauth.DefaultAuthFile()); err != nil {
				return err
			}
			fmt.Println("Codex credentials removed.")
			return nil
		},
	}
	logoutRoot.AddCommand(codexLogout)
	logoutRoot.RunE = func(cmd *cobra.Command, args []string) error {
		// No subcommand → remove all
		if err := oauth.Logout(oauth.DefaultAuthFile()); err != nil {
			return err
		}
		fmt.Println("All credentials removed.")
		return nil
	}
	root.AddCommand(logoutRoot)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cfg *config.Config, prompt string, flags cliFlags) error {
	// Phase 4 shortcut: --list-sessions prints and exits before
	// doing any setup. Honors --session-db so users can list
	// sessions from an alternate database path.
	if flags.listSessions {
		return listSessionsFromDB(flags.sessionDB)
	}

	// --continue is a CC-compat alias for --last; merge them early
	// so downstream logic only needs to check flags.last.
	if flags.continueSession {
		flags.last = true
	}

	// Skip SQLite in exec mode when no session resume needed — saves ~5-10ms.
	// --fork-session also needs DB (reads source, creates destination).
	needsDB := flags.last || flags.sessionID != "" || flags.forkSession != "" || prompt == ""
	var db *store.DB
	if needsDB {
		var err error
		db, err = store.Open(flags.sessionDB)
		if err != nil {
			// Previously errors were swallowed here; --list-sessions
			// did surface them. Consistent error handling means a
			// bad --session-db path fails fast on the main run path
			// too, instead of silently falling back to no-DB and
			// losing the user's session-resume intent.
			return fmt.Errorf("open session store: %w", err)
		}
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

	// Convert config permission rules into the engine's Rule format
	// and build an Evaluator. Without this, params.Perm was never set
	// and the engine fell back to ModeBypass — silently dropping
	// every allow/deny rule the user had configured in settings.json.
	// This was a security regression: a user thinking they had
	// "deny bash:rm -rf *" enforced was actually running with no
	// rules at all.
	permEval := buildPermissionEvaluator(cfg, projectRoot)

	// Phase 2: translate CLI permission flags into evaluator mutations
	// BEFORE branching into exec vs TUI so both paths honor the flags.
	// (Codex Phase 2 review caught that the earlier placement, inside
	// the headless branch, meant `altcode --dry-run` without a prompt
	// started a normal interactive session.)
	//
	// Build an ep shell just for Validate + ApplyPermissionOverrides —
	// the full exec.Params is constructed inside the headless branch
	// below with all the Phase 1 output fields.
	permShell := exec.Params{
		PermissionMode: flags.permissionMode,
		AllowTools:     flags.allowTools,
		DenyTools:      flags.denyTools,
		DryRun:         flags.dryRun,
	}
	if err := permShell.Validate(); err != nil {
		return err
	}
	newPermEval, err := exec.ApplyPermissionOverrides(permEval, &permShell, projectRoot)
	if err != nil {
		return err
	}
	permEval = newPermEval

	params := engine.EngineParams{
		Config:       cfg,
		Instructions: instructions,
		Memory:       memStore,
		Hooks:        hooksRunner,
		Skills:       skills,
		Perm:         permEval,
	}
	// Phase 4: --fork-session copies messages from an existing
	// session into a new one, then runs the new session. Takes
	// precedence over --last and --session because the cobra mutex
	// above forbids combining them.
	effectiveSessionID := flags.sessionID
	if flags.forkSession != "" {
		forkedID, err := forkSession(db, flags.forkSession, cfg.Model)
		if err != nil {
			return err
		}
		effectiveSessionID = forkedID
	}

	if err := loadSession(db, &params, flags.last, effectiveSessionID); err != nil {
		return err
	}

	if prompt != "" {
		// Build exec.Params from the engine params + CLI flags.
		// Phase 1 adds output-format + observability fields; Phase 2
		// adds permission / mode fields. Later phases extend this
		// struct with input/artifact flags. Keeping construction in
		// one place makes each phase a localized additive edit.
		//
		// Validate runs here (in addition to exec.Run) so Phase 1
		// rules like --quiet+--verbose surface before the engine is
		// built. Phase 2 rules were already validated via permShell
		// above, but re-running is harmless and consistent.
		ep := exec.Params{
			EngineParams:   params,
			Prompt:         prompt,
			JSON:           flags.jsonMode,
			Quiet:          flags.quiet,
			Model:          cfg.Model,
			Auth:           auth.CredentialSource(cfg),
			OutputFormat:   flags.outputFormat,
			Verbose:        flags.verbose,
			PrintCost:      flags.printCost,
			PrintTools:     flags.printTools,
			PrintTree:      flags.printTree,
			ShowSystem:     flags.showSystem,
			SaveTranscript: flags.saveTranscript,
			SaveCost:       flags.saveCost,
			SaveDiff:       flags.saveDiff,
			PermissionMode: flags.permissionMode,
			AllowTools:     flags.allowTools,
			DenyTools:      flags.denyTools,
			DryRun:         flags.dryRun,
			Images:         flags.images,
			Files:          flags.files,
			PromptFile:     flags.promptFile,
			System:         flags.system,
			SystemFile:     flags.systemFile,
		}
		if err := ep.Validate(); err != nil {
			return err
		}
		// Phase 5: read --prompt-file / --file / --system / --image
		// before the engine is built. PrepareInputs mutates ep.Prompt,
		// ep.EngineParams.Instructions, and ep.EngineParams.PendingInputParts.
		if err := ep.PrepareInputs(os.Stdin); err != nil {
			return err
		}
		return runExec(ep)
	}
	return runTUI(params)
}

// buildPermissionEvaluator translates the JSON config rules into the
// engine's permission.Rule format and returns a configured Evaluator.
// Returns nil only when there's nothing to configure — the engine
// then falls back to its own ModeBypass default.
func buildPermissionEvaluator(cfg *config.Config, projectRoot string) *permission.Evaluator {
	if cfg == nil {
		return nil
	}
	rules := make([]permission.Rule, 0, len(cfg.Permission))
	for _, pr := range cfg.Permission {
		var action permission.ActionType
		switch strings.ToLower(pr.Action) {
		case "allow":
			action = permission.ActionAllow
		case "deny":
			action = permission.ActionDeny
		case "ask":
			action = permission.ActionAsk
		default:
			// Unknown action — skip silently rather than crash on
			// startup. Users get the default eval semantics for the
			// rule's tool/pattern.
			continue
		}
		rules = append(rules, permission.Rule{
			Tool:    pr.Tool,
			Pattern: pr.Pattern,
			Action:  action,
			Source:  "config",
		})
	}
	return permission.NewEvaluator(permission.ModeDefault, projectRoot, rules)
}

// runExec is the headless entry point. Takes a fully-populated
// exec.Params; constructs the engine, starts MCP servers if needed,
// threads the signal-cancellable ctx down to exec.Run, and ensures
// deferred MCP cleanup runs on every return path (including typed
// UsageError returns — those must unwind through this function so
// defers fire before the top-level caller translates to os.Exit).
//
// Signal handling: traps SIGINT and SIGTERM. SIGTERM lets container
// runners and batch orchestrators ask for clean shutdown (spec v7
// Phase 11 folded in early — trivial change, avoids another edit
// in the same file).
func runExec(ep exec.Params) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eng, err := engine.New(ep.EngineParams)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}
	ep.Engine = eng

	// Only start MCP servers if prompt likely needs them.
	// MCP startup adds 1-5s of blocking latency per server.
	// Use the signal-cancellable ctx so SIGTERM tears down servers
	// instead of leaking them with context.Background().
	//
	// Phase 3 will add: `|| ep.PermissionPromptTool != ""` so that
	// headless permission-prompt-tool works even when the prompt
	// doesn't happen to mention MCP. For Phase 1 we keep the current
	// keyword-only gating.
	var mcpCleanup func()
	if needsMCP(ep.Prompt) {
		mcpCleanup = connectMCPWithCtx(ctx, ep.EngineParams.Config, eng)
	}
	if mcpCleanup != nil {
		defer mcpCleanup()
	}

	// Banner fields on exec.Params were previously populated from
	// engine.EngineParams.Config inside runExec. Now that the caller
	// passes exec.Params directly we still backfill the Model/Auth
	// fields here so the banner renders identically.
	if ep.Model == "" && ep.EngineParams.Config != nil {
		ep.Model = ep.EngineParams.Config.Model
	}
	if ep.Auth == "" && ep.EngineParams.Config != nil {
		ep.Auth = auth.CredentialSource(ep.EngineParams.Config)
	}

	return exec.Run(ctx, ep)
}

// needsMCP returns true if the prompt likely requires MCP tools.
func needsMCP(prompt string) bool {
	lower := strings.ToLower(prompt)
	keywords := []string{
		"mcp__", "mcp ", "mcp:",
		"playwright", "browser", "chrome", "devtools",
		"language-server", "lsp", "gopls", "pyright",
		"screenshot", "navigate to", "visit http",
		"memory graph", "knowledge graph",
		"filesystem mcp", "read_multiple",
		"sequential-thinking", "sequentialthinking",
	}
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func runTUI(params engine.EngineParams) error {
	// Install a signal-cancellable context so MCP subprocesses get
	// torn down on Ctrl+C / SIGTERM even if Bubbletea exits abnormally.
	// runExec and runWorkflow already do this; runTUI was the gap.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	eng, err := engine.New(params)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	mcpCleanup := connectMCPWithCtx(ctx, params.Config, eng)
	defer mcpCleanup()

	cmds := discoverCommands()

	theme := tui.GetTheme(params.Config.Theme)
	app := tui.New(eng, theme, Version, auth.MissingCredentialPrompt(params.Config), cmds...)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

// connectMCPWithCtx is connectMCP with an explicit context so MCP
// subprocesses can be cancelled by signal handling. Falls back to
// connectMCP semantics if cfg.MCP is empty.
func connectMCPWithCtx(ctx context.Context, cfg *config.Config, eng *engine.Engine) func() {
	if len(cfg.MCP) == 0 {
		return func() {}
	}
	mgr := mcp.NewManager(ctx, cfg.MCP)
	mgr.RegisterAll(ctx, eng.Registry())
	return mgr.Close
}

// connectMCP is kept as a thin shim for any remaining call sites that
// have no signal context to thread. Prefer connectMCPWithCtx — the
// background-context version cannot tear down MCP subprocesses on
// Ctrl+C / SIGTERM and leaks them past altcode shutdown.
func connectMCP(cfg *config.Config, eng *engine.Engine) func() {
	return connectMCPWithCtx(context.Background(), cfg, eng)
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
	// Plugin-contributed agents discovered earlier in loadPlugins —
	// fold them in so user-installed plugins can ship subagents.
	agents = append(agents, pluginAgents...)
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
	// Fold in commands contributed by plugins. Plugins were previously
	// parsed but their commands never reached the slash-command loader.
	cmds = append(cmds, pluginCommands...)
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

func runDeviceCodeLogin(ctx context.Context, path string) error {
	dc, err := oauth.RequestDeviceCode(ctx)
	if err != nil {
		return err
	}

	fmt.Println("altcode login (device code)")
	fmt.Println()
	fmt.Println("1. Open this link in your browser:")
	fmt.Printf("   \033[94m%s\033[0m\n", dc.VerificationURL)
	fmt.Println()
	fmt.Println("2. Enter this code (expires in 15 minutes):")
	fmt.Printf("   \033[94m%s\033[0m\n", dc.UserCode)
	fmt.Println()
	fmt.Println("\033[90mDevice codes are a phishing target. Never share this code.\033[0m")
	fmt.Println()
	fmt.Println("Waiting for authorization...")

	td, err := dc.PollForToken(ctx, 15*time.Minute)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	auth := &oauth.AuthJSON{
		AuthMode:    "Chatgpt",
		Tokens:      td,
		LastRefresh: &now,
	}
	if err := oauth.Save(path, auth); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	fmt.Println("Login successful. Credentials saved to " + path)
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
	return listSessionsFromDB("")
}

// listSessionsFromDB opens the store at the given file path (empty =
// default) and prints all sessions. Shared by the `sessions` subcommand
// and the Phase 4 `--list-sessions` root flag. Passing a custom
// sessionDB path lets users inspect a non-default database file.
func listSessionsFromDB(sessionDB string) error {
	db, err := store.Open(sessionDB)
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

// forkSession thin-wraps store.DB.ForkSession to produce a
// *exec.UsageError on missing-source (so the top-level wrapper
// returns exit 64 EX_USAGE instead of opaque errors) and emits a
// diagnostic line to stderr on success.
//
// The heavy lifting (transaction, message copy, prepared statement)
// lives in the store package so the ~10k-message perf cliff from
// per-row fsync doesn't apply.
func forkSession(db *store.DB, sourceID, modelFallback string) (string, error) {
	if db == nil {
		return "", exec.NewUsageError("--fork-session requires a session store")
	}
	newSess, copied, err := db.ForkSession(sourceID, "", modelFallback)
	if err != nil {
		// Distinguish "source not found" from I/O errors so users
		// get a crisp UsageError for the common typo case. Use
		// errors.Is against the sentinel instead of substring
		// matching (CC Phase 4 v2 nit).
		if errors.Is(err, store.ErrSessionNotFound) {
			return "", exec.NewUsageError(
				"--fork-session: source session %q not found "+
					"(run `altcode --list-sessions` to see available IDs)",
				sourceID)
		}
		return "", err
	}
	fmt.Fprintf(os.Stderr, "altcode: forked %s → %s (%d messages copied)\n",
		shortID(sourceID), shortID(newSess.ID), copied)
	return newSess.ID, nil
}

// shortID returns the first 8 characters of a session ID, for display.
func shortID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
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

	// Chinese AI provider API keys (all OpenAI-compatible)
	for _, ep := range []struct{ env, provider string }{
		{"DEEPSEEK_API_KEY", "deepseek"},
		{"ZHIPU_API_KEY", "zhipu"},
		{"MOONSHOT_API_KEY", "moonshot"},
		{"MINIMAX_API_KEY", "minimax"},
		{"DASHSCOPE_API_KEY", "qwen"},
		{"ALTLLM_API_KEY", "altllm"},
		{"ALTLLM", "altllm"},
	} {
		if key := os.Getenv(ep.env); key != "" {
			if p, ok := cfg.Provider[ep.provider]; !ok || p.APIKey == "" {
				cfg.Provider[ep.provider] = config.ProviderConfig{APIKey: key}
			}
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
	overlay, err := config.LoadFile(path)
	if err != nil {
		// Missing files are normal — skip silently. Anything else
		// (malformed JSON, permission denied, syntax error) is a real
		// startup problem that the user needs to know about, so we
		// print to stderr BEFORE the TUI takes over the screen.
		// This is intentional: the alternative is silently dropping
		// 'altcode ignored my settings' bug reports.
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "altcode: failed to load config %q: %v\n", path, err)
		}
		return
	}
	mergeConfig(base, overlay)
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

// pluginCommands and pluginAgents hold contributions from loaded plugins
// so discoverCommands/discoverAgents can fold them into the global slash
// command and subagent registries. Plugins parsed without this wiring
// were silently dropped — Plugin.Merge only ever propagated hooks.
var (
	pluginCommands []*command.Command
	pluginAgents   []*agent.Agent
)

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
	pluginCommands = pluginCommands[:0]
	pluginAgents = pluginAgents[:0]
	for _, p := range plugins {
		p.Merge(cfg)
		pluginCommands = append(pluginCommands, p.Commands...)
		pluginAgents = append(pluginAgents, p.Agents...)
	}
}
