package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// runInit generates a CLAUDE.md file by analyzing the codebase.
func (a *App) runInit() tea.Cmd {
	root := detectProjectRoot()
	path := filepath.Join(root, "CLAUDE.md")

	if _, err := os.Stat(path); err == nil {
		a.appendInfo("[init] CLAUDE.md already exists. Delete it first to regenerate.")
		return nil
	}

	content := buildClaudeMD(root)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		a.appendInfo(fmt.Sprintf("[init] Error: %v", err))
		return nil
	}
	a.appendInfo(fmt.Sprintf("[init] Created %s (%d bytes)\nRe-launch altcode to load it.", path, len(content)))
	return nil
}

func buildClaudeMD(root string) string {
	var sb strings.Builder
	sb.WriteString("# Project Guide\n\n")

	// Detect language
	lang := detectLanguage(root)
	sb.WriteString(fmt.Sprintf("## Language: %s\n\n", lang))

	// Build commands
	sb.WriteString("## Build & Test\n\n```bash\n")
	switch lang {
	case "Go":
		sb.WriteString("go build ./...\ngo test ./... -race\ngo vet ./...\n")
	case "TypeScript", "JavaScript":
		sb.WriteString("npm install\nnpm test\nnpm run build\nnpm run lint\n")
	case "Python":
		sb.WriteString("pip install -e .\npytest\nflake8\n")
	case "Rust":
		sb.WriteString("cargo build\ncargo test\ncargo clippy\n")
	default:
		sb.WriteString("# Add your build commands here\n")
	}
	sb.WriteString("```\n\n")

	// Project structure — pure Go walk so we don't depend on `find`
	// being on PATH and don't silently swallow exec errors. List the
	// top-level directories, skipping dot-files, vendor, node_modules,
	// and OS clutter that almost never belongs in a project guide.
	sb.WriteString("## Structure\n\n```\n")
	if entries, err := os.ReadDir(root); err == nil {
		written := 0
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			switch name {
			case "vendor", "node_modules", "dist", "build", "target", "__pycache__":
				continue
			}
			sb.WriteString(name + "/\n")
			written++
			if written >= 30 {
				sb.WriteString("...\n")
				break
			}
		}
		if written == 0 {
			sb.WriteString("# (no directories detected)\n")
		}
	} else {
		sb.WriteString("# (failed to read directory: " + err.Error() + ")\n")
	}
	sb.WriteString("```\n\n")

	sb.WriteString("## Hard Rules\n\n")
	sb.WriteString("- Never commit secrets (API keys, tokens, passwords)\n")
	sb.WriteString("- Run tests before pushing\n")
	sb.WriteString("- Keep functions under 30 lines\n")
	sb.WriteString("- Keep files under 500 lines\n")

	return sb.String()
}

func detectLanguage(root string) string {
	checks := []struct {
		file string
		lang string
	}{
		{"go.mod", "Go"},
		{"package.json", "TypeScript"},
		{"Cargo.toml", "Rust"},
		{"pyproject.toml", "Python"},
		{"requirements.txt", "Python"},
		{"pom.xml", "Java"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(root, c.file)); err == nil {
			return c.lang
		}
	}
	return "Unknown"
}

// runDoctor checks environment health.
func (a *App) runDoctor() string {
	var sb strings.Builder
	sb.WriteString("Doctor Report\n\n")

	// Check providers
	sb.WriteString("Providers:\n")
	if a.engine != nil {
		cfg := a.engine.Config()
		for name, pcfg := range cfg.Provider {
			status := "✗ no key"
			if pcfg.APIKey != "" {
				status = "✓ configured"
			}
			sb.WriteString(fmt.Sprintf("  %-12s %s\n", name+":", status))
		}
	}

	// Check tools — guarded with the same engine != nil check the rest
	// of the function uses. Without it the TUI panicked when /doctor
	// ran before the engine had been constructed (e.g. failed provider
	// init left a.engine nil but the welcome screen still accepted /).
	if a.engine != nil && a.engine.Registry() != nil {
		sb.WriteString(fmt.Sprintf("\nTools:       %d registered\n", len(a.engine.Registry().All())))
	} else {
		sb.WriteString("\nTools:       (engine not initialized)\n")
	}

	// Check MCP
	if a.engine != nil && a.engine.Config() != nil {
		sb.WriteString(fmt.Sprintf("MCP servers: %d\n", len(a.engine.Config().MCP)))
	}

	// Check git
	if _, err := exec.Command("git", "rev-parse", "--git-dir").Output(); err == nil {
		sb.WriteString("Git:         ✓ repository detected\n")
	} else {
		sb.WriteString("Git:         ✗ not a git repository\n")
	}

	// Check CLI agents
	sb.WriteString("\nAgents:\n")
	for _, bin := range []string{"claude", "codex", "opencode"} {
		if p, err := exec.LookPath(bin); err == nil {
			sb.WriteString(fmt.Sprintf("  %-12s ✓ %s\n", bin+":", p))
		} else {
			sb.WriteString(fmt.Sprintf("  %-12s ✗ not installed\n", bin+":"))
		}
	}

	return sb.String()
}

// startCompare runs the same prompt through multiple available models.
func (a *App) startCompare(task string) tea.Cmd {
	a.appendInfo("[compare] Spawning agents with different models...")
	// Use the workspace system to spawn multiple agents
	return a.startWorkspaceFromTUIWithAgents(task, []agentSpec{
		{backend: "claude", role: "claude-review"},
		{backend: "codex", role: "codex-review"},
	})
}
