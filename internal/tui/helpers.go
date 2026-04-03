package tui

import "strings"

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
	if strings.TrimSpace(version) == "" {
		return "dev"
	}
	return version
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
