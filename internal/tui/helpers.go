package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/config"
)

func normalInputPlaceholder(startupPrompt string) string {
	if strings.TrimSpace(startupPrompt) != "" {
		switch currentProviderFromPrompt(startupPrompt) {
		case "anthropic":
			return "Press Enter to set up your Anthropic API key"
		case "openai":
			return "Press Enter to set up your OpenAI API key"
		default:
			return "Press Enter to start setup"
		}
	}
	return "Ask anything... (Enter to submit, Ctrl+J newline, Esc to quit)"
}

func displayVersion(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return "dev"
	}
	// Callers prepend a literal "v" (e.g. "v"+displayVersion(...)). Strip
	// any leading 'v'/'V' on the input so we never render "vv0.10.4" when
	// the build is tagged with -X main.Version=v0.10.4.
	v = strings.TrimLeft(v, "vV")
	if v == "" {
		return "dev"
	}
	return v
}

// formatUSD renders a dollar amount with adaptive precision so the
// HUD chip ($0.0026), the inline turn summary ($0.0053), and the /cost
// command all show the SAME number for the SAME cost — round-5
// dual-reviewer finding flagged the inconsistency between the
// 4-decimal HUD and the 2-decimal inline summary ($0.0052 vs $0.01).
//
// Rules:
//   - < $0.01 → 4 decimals so half-cent precision is preserved
//   - < $1.00 → 3 decimals (e.g. $0.523)
//   - ≥ $1.00 → 2 decimals (e.g. $12.34) so big costs read naturally
func formatUSD(amount float64) string {
	switch {
	case amount < 0:
		// Negative shouldn't happen, but render the magnitude
		return "-" + formatUSD(-amount)
	case amount < 0.01:
		return fmt.Sprintf("$%.4f", amount)
	case amount < 1.0:
		return fmt.Sprintf("$%.3f", amount)
	default:
		return fmt.Sprintf("$%.2f", amount)
	}
}

func welcomeWordmark() string {
	return strings.Join([]string{
		"    _     _   _____   ____    ___    ____    _____ ",
		"   / \\   | | |_   _| / ___|  / _ \\  |  _ \\  | ____|",
		"  / _ \\  | |   | |  | |     | | | | | | | | |  _|  ",
		" / ___ \\ | |___| |  | |___  | |_| | | |_| | | |___ ",
		"/_/   \\_\\|_____|_|   \\____|  \\___/  |____/  |_____|",
	}, "\n")
}

func currentProviderFromPrompt(prompt string) string {
	switch {
	case strings.Contains(prompt, "Anthropic"):
		return "anthropic"
	case strings.Contains(prompt, "OpenAI"):
		return "openai"
	default:
		return ""
	}
}

func parseProvider(model string) string {
	for i := 0; i < len(model); i++ {
		if model[i] == '/' {
			return model[:i]
		}
	}
	if model == "" {
		return "anthropic"
	}
	return model
}

func providerSetupPlaceholder(provider string) string {
	switch provider {
	case "anthropic":
		return "Paste your Anthropic API key and press Enter"
	case "openai":
		return "Paste your OpenAI API key and press Enter"
	default:
		return "Paste API key and press Enter"
	}
}

func providerLoginLabel(provider string) string {
	switch provider {
	case "anthropic":
		return "Claude Code"
	case "openai":
		return "Codex"
	default:
		return "your CLI"
	}
}

func looksLikeAuthError(msg, provider string) bool {
	authSignals := []string{
		" status 401",
		" status 403",
		"unauthorized",
		"authentication",
		"auth_error",
		"authentication_error",
		"invalid api key",
		"incorrect api key",
		"invalid x-api-key",
	}

	hasProvider := strings.Contains(msg, provider)
	for _, signal := range authSignals {
		if strings.Contains(msg, signal) {
			if hasProvider {
				return true
			}
			if signal == "incorrect api key" || signal == "invalid api key" || signal == "invalid x-api-key" {
				return true
			}
		}
	}
	return false
}

func providerLabel(name string) string {
	switch name {
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	default:
		if name == "" {
			return "Provider"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

// detectProjectRoot finds the project root from cwd.
func detectProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return config.DetectProjectRoot(cwd)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

