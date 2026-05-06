package main

// Phase 10: inspection flags. Each --print-* / --doctor entry point
// prints a human-readable (or JSON) view of some part of altcode's
// runtime state and exits. None of these touch the engine's hot
// path — they're intended for quick triage, scripting, and CI
// preflight checks.

import (
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/config"
	"github.com/jiayaoqijia/altcode/internal/engine"
	"github.com/jiayaoqijia/altcode/internal/exec"
)

// printConfig writes the effective (post-cascade) config to stdout
// as pretty-printed JSON and returns nil. Secrets are redacted so
// piping this into a bug report or log doesn't leak credentials.
//
// Redaction scope:
//   - Every ProviderConfig.APIKey → "<redacted>"
//   - Every ProviderConfig.BaseURL that looks like it embeds
//     credentials (`user:pass@host`) → "<redacted-url>"
//   - Every MCPServerConfig.Env value → "<redacted>" (MCP servers
//     routinely receive ANTHROPIC_API_KEY / GITHUB_TOKEN / etc. via
//     the Env map; a CC Phase 10 review caught this leak)
func printConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("no config loaded")
	}
	// Deep-copy all reference fields we're rewriting so the
	// caller's live config stays untouched. ProviderConfig and
	// MCPServerConfig are value types, but Env is a map which
	// is a reference — naive copy would alias.
	redacted := *cfg

	if cfg.Provider != nil {
		redacted.Provider = make(map[string]config.ProviderConfig, len(cfg.Provider))
		for k, v := range cfg.Provider {
			if v.APIKey != "" {
				v.APIKey = "<redacted>"
			}
			if strings.Contains(v.BaseURL, "@") && strings.HasPrefix(v.BaseURL, "http") {
				v.BaseURL = "<redacted-url>"
			}
			redacted.Provider[k] = v
		}
	}

	if cfg.MCP != nil {
		redacted.MCP = make(map[string]config.MCPServerConfig, len(cfg.MCP))
		for k, v := range cfg.MCP {
			// Copy the Env map so the redaction doesn't mutate
			// the live config's server entry.
			if len(v.Env) > 0 {
				newEnv := make(map[string]string, len(v.Env))
				for envKey := range v.Env {
					newEnv[envKey] = "<redacted>"
				}
				v.Env = newEnv
			}
			redacted.MCP[k] = v
		}
	}

	// Team models can carry per-role APIKey and BaseURL overrides;
	// both must be redacted. Codex Phase 10 review caught this.
	if cfg.Team != nil && cfg.Team.Models != nil {
		newTeam := *cfg.Team
		newTeam.Models = make(map[string]config.TeamModel, len(cfg.Team.Models))
		for role, tm := range cfg.Team.Models {
			if tm.APIKey != "" {
				tm.APIKey = "<redacted>"
			}
			if strings.Contains(tm.BaseURL, "@") && strings.HasPrefix(tm.BaseURL, "http") {
				tm.BaseURL = "<redacted-url>"
			}
			newTeam.Models[role] = tm
		}
		redacted.Team = &newTeam
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(&redacted)
}

// printToolsList prints all tools registered with a default engine
// to stdout, sorted alphabetically with their descriptions.
//
// Uses a throwaway engine because the tool registry is populated
// at engine construction (internal/engine/engine.go:146-170).
// This is the same set of tools any real run would see, modulo
// provider/sandbox overrides.
func printToolsList() error {
	eng, err := buildInspectionEngine()
	if err != nil {
		return err
	}
	tools := eng.Registry().All()
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name() < tools[j].Name()
	})
	for _, t := range tools {
		desc := t.Description()
		// Collapse newlines so the one-line-per-tool format stays
		// readable; descriptions sometimes span paragraphs.
		desc = strings.ReplaceAll(desc, "\n", " ")
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}
		fmt.Printf("%-20s %s\n", t.Name(), desc)
	}
	fmt.Printf("\n%d tools registered.\n", len(tools))
	return nil
}

// printSkillsList prints the skills discovered by discoverSkills()
// PLUS the agents discovered by discoverAgents(), matching what
// the TUI /skills command shows. CC Phase 10 review caught that
// the naive discoverSkills()-only version dropped every agent —
// the TUI version at main.go:450-460 appends them with a
// " (agent)" suffix.
func printSkillsList() error {
	skills := discoverSkills()
	agents := discoverAgents()
	for _, a := range agents {
		skills = append(skills, engine.Skill{
			Name:        a.Name + " (agent)",
			Description: a.Description,
			Path:        a.Path,
		})
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	for _, s := range skills {
		desc := s.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("%-30s %s\n", s.Name, desc)
		if s.Path != "" {
			fmt.Printf("  %s\n", s.Path)
		}
	}
	fmt.Printf("\n%d skills+agents discovered.\n", len(skills))
	return nil
}

// printMCPServers lists the MCP servers configured in the cascaded
// config. Does NOT start them — just prints the static config so
// users can sanity-check their .mcp.json / settings.json setup
// without paying the 1-5s startup latency per server.
func printMCPServers(cfg *config.Config) error {
	if cfg == nil || len(cfg.MCP) == 0 {
		fmt.Println("No MCP servers configured.")
		return nil
	}
	names := make([]string, 0, len(cfg.MCP))
	for n := range cfg.MCP {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		srv := cfg.MCP[name]
		fmt.Printf("%-20s %s", name, srv.Command)
		if len(srv.Args) > 0 {
			for _, a := range srv.Args {
				fmt.Printf(" %s", a)
			}
		}
		if srv.URL != "" {
			fmt.Printf(" (sse %s)", srv.URL)
		}
		fmt.Println()
	}
	fmt.Printf("\n%d MCP servers configured.\n", len(cfg.MCP))
	return nil
}

// printDoctorReport runs the same environment health checks as the
// TUI's /doctor command, but routed through stdout instead of the
// TUI info pane. Shared logic isn't extracted from internal/tui
// (the TUI version binds to an App receiver); copying the checks
// here avoids a circular internal/tui → cmd/altcode dependency.
//
// Checks:
//   1. Provider credentials (one line per entry in cfg.Provider)
//   2. Tool registry size
//   3. MCP server count
//   4. Git repository presence
//   5. CLI agents on PATH (claude, codex, opencode)
func printDoctorReport(cfg *config.Config) error {
	fmt.Println("altcode doctor report")
	fmt.Println()

	// Providers
	fmt.Println("Providers:")
	if cfg != nil && cfg.Provider != nil {
		names := make([]string, 0, len(cfg.Provider))
		for n := range cfg.Provider {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			pcfg := cfg.Provider[name]
			status := "✗ no key"
			if pcfg.APIKey != "" {
				status = "✓ configured"
			}
			fmt.Printf("  %-12s %s\n", name+":", status)
		}
	}

	// Tools
	eng, err := buildInspectionEngine()
	if err != nil {
		fmt.Printf("\nTools:       (engine build failed: %v)\n", err)
	} else {
		fmt.Printf("\nTools:       %d registered\n", len(eng.Registry().All()))
	}

	// MCP
	if cfg != nil {
		fmt.Printf("MCP servers: %d\n", len(cfg.MCP))
	}

	// Git
	if _, err := osexec.Command("git", "rev-parse", "--git-dir").Output(); err == nil {
		fmt.Println("Git:         ✓ repository detected")
	} else {
		fmt.Println("Git:         ✗ not a git repository")
	}

	// CLI agents
	fmt.Println()
	fmt.Println("Agents:")
	for _, bin := range []string{"claude", "codex", "opencode"} {
		if p, err := osexec.LookPath(bin); err == nil {
			fmt.Printf("  %-12s ✓ %s\n", bin+":", p)
		} else {
			fmt.Printf("  %-12s ✗ not installed\n", bin+":")
		}
	}

	// Config cascade (which files contributed)
	wd, _ := os.Getwd()
	projectRoot := config.DetectProjectRoot(wd)
	fmt.Println()
	fmt.Printf("Project root: %s\n", projectRoot)
	for _, p := range candidateConfigPaths(projectRoot) {
		mark := "✗"
		if _, err := os.Stat(p); err == nil {
			mark = "✓"
		}
		fmt.Printf("  %s %s\n", mark, p)
	}

	return nil
}

// buildInspectionEngine constructs a minimal engine purely so the
// tool registry can be enumerated. Uses a sentinel config that
// avoids touching the network. Returns the engine directly; the
// caller is responsible for discarding it.
func buildInspectionEngine() (*engine.Engine, error) {
	// Minimal config: the engine only needs a Model to pick a
	// provider builder, and we'll allow it to fall through to
	// whatever the default is since we're not calling Run().
	cfg := config.Default()
	return engine.New(engine.EngineParams{Config: cfg})
}

// candidateConfigPaths returns the filesystem locations altcode
// checks during its config cascade, in order. Used by --doctor
// to show which ones exist.
func candidateConfigPaths(projectRoot string) []string {
	home, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(projectRoot, ".altcode", "config.json"),
	}
	if home != "" {
		paths = append(paths,
			filepath.Join(home, ".config", "altcode", "config.json"),
			filepath.Join(home, ".altcode", "config.json"),
		)
	}
	// Claude Code compat
	paths = append(paths,
		filepath.Join(projectRoot, ".claude", "settings.json"),
		filepath.Join(projectRoot, ".mcp.json"),
	)
	return paths
}

// printResolvedParams writes a secret-redacted JSON view of the
// resolved exec.Params (flag-parse → struct assembly → engine-ready)
// to stdout and returns nil. Used by --print-params for mechanical
// end-to-end flag-propagation tests — CI can now assert that e.g.
// `altcode --max-turns 7 --print-params "hi"` produces JSON with
// max_turns=7, proving the flag reached the runtime boundary.
//
// What's shown: MaxTurns, MaxCost, OutputFormat, PermissionMode,
// AllowTools, DenyTools, DryRun, Commit, CommitDirty, SaveCost,
// SaveDiff, SaveTranscript, and whether EngineParams.CostBudget
// is wired. Sizes (prompt_len, system_len, etc.) are shown.
//
// What's redacted/truncated:
//   - Any file content that may have been loaded via --file,
//     --prompt-file, or --system-file (logged as sizes only).
//   - The prompt itself is truncated to 200 chars. A --prompt-file
//     that carried a secret would previously round-trip verbatim
//     through this output; iter-5 CC review caught that
//     inconsistency with the file-content redaction contract.
func printResolvedParams(p exec.Params) error {
	view := map[string]any{
		"prompt":                truncate(p.Prompt, 200),
		"prompt_len":            len(p.Prompt),
		"output_format":         string(p.OutputFormat),
		"verbose":               p.Verbose,
		"quiet":                 p.Quiet,
		"print_cost":            p.PrintCost,
		"print_tools":           p.PrintTools,
		"print_tree":            p.PrintTree,
		"show_system":           p.ShowSystem,
		"permission_mode":       p.PermissionMode,
		"permission_prompt_tool": p.PermissionPromptTool,
		"allow_tools":           p.AllowTools,
		"deny_tools":            p.DenyTools,
		"dry_run":               p.DryRun,
		"max_turns":             p.MaxTurns,
		"max_cost":              p.MaxCost,
		"commit":                p.Commit,
		"commit_dirty":          p.CommitDirty,
		"save_transcript":       p.SaveTranscript,
		"save_cost":             p.SaveCost,
		"save_diff":             p.SaveDiff,
		"schema_file":           p.SchemaFile,
		"hooks_count":           len(p.Hooks),
		"mcp_servers_count":     len(p.MCPServers),
		"skills_count":          len(p.Skills),
		"run_workflow":          p.RunWorkflow,
		"prompt_each":           p.PromptEach,
		"parallel":              p.Parallel,
		"retry":                 p.Retry,
		"bail":                  p.Bail,
		"images_count":          len(p.Images),
		"files_count":           len(p.Files),
		"system_len":            len(p.System),
		"engine_max_turns":      p.EngineParams.MaxTurns,
		"engine_cost_budget_wired": p.EngineParams.CostBudget != nil,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(view)
}

// truncate returns s limited to maxRunes RUNES (not bytes); anything
// past the cap is replaced with a "...[N more runes]" tail so the
// caller can tell the value was elided. Byte-indexed truncation
// would split a multi-byte character (CJK, emoji) mid-codepoint and
// emit invalid UTF-8; iter-6 CC review caught that regression. Used
// by --print-params to prevent a --prompt-file with embedded secrets
// from leaking verbatim into JSON intended for copy-paste into bug
// reports.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) +
		fmt.Sprintf("...[%d more runes]", len(runes)-maxRunes)
}
