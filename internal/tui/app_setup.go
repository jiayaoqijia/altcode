package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jiayaoqijia/altcode/internal/auth"
	"github.com/jiayaoqijia/altcode/internal/engine"
)

func (a *App) startRecommendedSetup() {
	if provider := a.recommendedSetupProvider(); provider != "" {
		a.beginSetup(provider)
		return
	}
	a.setupError = "This model needs credentials before altcode can send prompts."
	a.setupSuccess = ""
	a.updateViewport()
}

func (a *App) beginSetup(provider string) {
	a.setupProvider = provider
	a.setupError = ""
	a.setupSuccess = ""
	a.setupInput.Reset()
	a.setupInput.Prompt = providerLabel(provider) + " API key: "
	a.setupInput.Placeholder = providerSetupPlaceholder(provider)
	a.setupInput.Focus()
	a.updateViewport()
}

func (a *App) cancelSetup() {
	a.setupProvider = ""
	a.setupInput.Blur()
	a.setupInput.Reset()
	a.input.Focus()
	a.updateViewport()
}

func (a *App) saveSetupKey() {
	key := strings.TrimSpace(a.setupInput.Value())
	if key == "" {
		a.setupError = "Paste an API key before saving."
		a.setupSuccess = ""
		a.updateViewport()
		return
	}

	providerName := a.setupProvider
	requiredProvider := a.recommendedSetupProvider()
	currentModel := a.activeModel()
	cfg := a.engine.Config()
	pcfg := cfg.Provider[providerName]
	pcfg.APIKey = key
	cfg.Provider[providerName] = pcfg

	path, err := auth.SaveProviderAPIKey(providerName, key)
	if err != nil {
		a.setupError = fmt.Sprintf("Could not save the API key: %v", err)
		a.setupSuccess = ""
		a.updateViewport()
		return
	}

	if err := a.refreshEngine(); err != nil {
		a.setupError = fmt.Sprintf("Saved the API key, but could not refresh altcode: %v", err)
		a.setupSuccess = ""
		a.updateViewport()
		return
	}

	a.setupProvider = ""
	a.setupInput.Blur()
	a.setupInput.Reset()
	a.input.Focus()
	a.startupPrompt = auth.MissingCredentialPrompt(a.engine.Config())
	a.input.Placeholder = normalInputPlaceholder(a.startupPrompt)
	a.setupError = ""
	a.setupSuccess = fmt.Sprintf("Saved %s API key to %s.", providerLabel(providerName), path)
	if strings.TrimSpace(a.startupPrompt) == "" && providerName == requiredProvider {
		a.setupSuccess += " You can start chatting now."
	} else if requiredProvider != "" && providerName != requiredProvider {
		a.setupSuccess += fmt.Sprintf(
			" Current model %s still needs %s credentials, or you can relaunch with --model %s/...",
			currentModel,
			providerLabel(requiredProvider),
			providerName,
		)
	}
	a.updateViewport()
}

func (a *App) refreshEngine() error {
	if a.engine == nil {
		return nil
	}
	// Re-create the engine with the updated config to pick up new API keys.
	refreshed, err := engine.New(engine.EngineParams{
		Config:      a.engine.Config(),
		ProjectRoot: a.projectRoot,
	})
	if err != nil {
		return err
	}

	a.engine = refreshed
	return nil
}

func (a *App) welcomeView() string {
	logoStyle := lipgloss.NewStyle().
		Foreground(a.theme.Primary).
		Bold(true)

	titleStyle := lipgloss.NewStyle().
		Foreground(a.theme.Secondary).
		Bold(true)

	mutedStyle := lipgloss.NewStyle().
		Foreground(a.theme.Muted)

	codeStyle := lipgloss.NewStyle().
		Foreground(a.theme.Warning).
		Bold(true)

	version := a.version
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}

	lines := a.welcomeHeader(logoStyle, mutedStyle, version)

	if a.setupSuccess != "" {
		successStyle := lipgloss.NewStyle().
			Foreground(a.theme.Success).
			Bold(true)
		lines = append(lines, "", successStyle.Render(a.setupSuccess))
	}

	if a.setupError != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(a.theme.Error).
			Bold(true)
		lines = append(lines, "", errorStyle.Render(a.setupError))
	}

	if a.setupProvider != "" {
		provider := a.setupProvider
		lines = append(lines,
			"",
			titleStyle.Render("Set "+providerLabel(provider)+" API key"),
			mutedStyle.Render("Current model: ")+codeStyle.Render(a.activeModel()),
			mutedStyle.Render("Paste the key below. It will be masked and saved to ")+codeStyle.Render(auth.UserConfigPath()),
			mutedStyle.Render("• Press ")+codeStyle.Render("Enter")+mutedStyle.Render(" to save the key"),
			mutedStyle.Render("• Press ")+codeStyle.Render("Esc")+mutedStyle.Render(" to go back"),
			mutedStyle.Render("• altcode can also auto-detect ")+codeStyle.Render(providerLoginLabel(provider))+mutedStyle.Render(" on restart"),
		)
		return strings.Join(lines, "\n")
	}

	if strings.TrimSpace(a.startupPrompt) != "" {
		warningStyle := lipgloss.NewStyle().
			Foreground(a.theme.Warning).
			Bold(true)
		provider := a.recommendedSetupProvider()
		lines = append(lines,
			"",
			titleStyle.Render("Let's get altcode connected"),
			warningStyle.Render("Current model: "+a.activeModel()),
			"",
			titleStyle.Render("Recommended next step"),
			mutedStyle.Render("• Press ")+codeStyle.Render("Enter")+mutedStyle.Render(" to add your ")+codeStyle.Render(providerLabel(provider)+" API key"),
			mutedStyle.Render("• Your key is masked while typing and saved to ")+codeStyle.Render(auth.UserConfigPath()),
			mutedStyle.Render("• Already signed into ")+codeStyle.Render(providerLoginLabel(provider))+mutedStyle.Render("? Restart altcode and it will auto-detect it"),
			"",
			titleStyle.Render("Other paths"),
			mutedStyle.Render("• Press ")+codeStyle.Render("A")+mutedStyle.Render(" to save an Anthropic key manually"),
			mutedStyle.Render("• Press ")+codeStyle.Render("O")+mutedStyle.Render(" to save an OpenAI key manually"),
			mutedStyle.Render("• Want local models instead? Relaunch with ")+codeStyle.Render("--model ollama/<model>")+mutedStyle.Render(" or ")+codeStyle.Render("--model lmstudio/<model>"),
		)
	}

	lines = append(lines,
		"",
		codeStyle.Render("Enter")+mutedStyle.Render(" send  ")+
			codeStyle.Render("Ctrl+K")+mutedStyle.Render(" commands  ")+
			codeStyle.Render("/help")+mutedStyle.Render(" all shortcuts"),
	)

	return strings.Join(lines, "\n")
}

func (a *App) repromptForAPIKey(provider string) {
	a.beginSetup(provider)
	a.setupError = fmt.Sprintf(
		"%s rejected the current API key for model %s. Enter a new key to continue.",
		providerLabel(provider),
		a.activeModel(),
	)
	a.setupSuccess = ""
	a.updateViewport()
}

func (a *App) welcomeHeader(logoStyle, mutedStyle lipgloss.Style, version string) []string {
	model := a.activeModel()
	short := model
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	// Quiet two-tone tip palette so the welcome screen feels like a
	// finished product, not a placeholder. Round-5 polish pass.
	keyStyle := lipgloss.NewStyle().Foreground(a.theme.Warning).Bold(true)
	codeStyle := lipgloss.NewStyle().Foreground(a.theme.Secondary)
	bullet := mutedStyle.Render("  •")
	return []string{
		logoStyle.Render("⌬ altcode") +
			mutedStyle.Render("  v"+displayVersion(version)) +
			lipgloss.NewStyle().Foreground(a.theme.Secondary).Bold(true).Render("  ["+short+"]"),
		mutedStyle.Render("  the universal agent harness for coding"),
		"",
		bullet + mutedStyle.Render(" type a prompt and press ") + keyStyle.Render("Enter") + mutedStyle.Render(" to start"),
		bullet + mutedStyle.Render(" press ") + keyStyle.Render("Ctrl+K") + mutedStyle.Render(" for the command palette, ") + keyStyle.Render("/help") + mutedStyle.Render(" for keys"),
		bullet + mutedStyle.Render(" attach files with ") + keyStyle.Render("@path") + mutedStyle.Render(" — fuzzy match on tab"),
		bullet + mutedStyle.Render(" cycle thinking effort with ") + keyStyle.Render("Shift+Tab") + mutedStyle.Render(" (when supported)"),
		bullet + mutedStyle.Render(" hot-swap model: ") + codeStyle.Render("/model haiku") + mutedStyle.Render(" or ") + codeStyle.Render("/model deepseek-v4-pro"),
	}
}

func (a *App) headerMeta() string {
	parts := []string{"v" + displayVersion(a.version)}
	if strings.TrimSpace(a.tokenInfo) != "" {
		parts = append(parts, a.tokenInfo)
	}
	if costInfo := a.costSummaryShort(); costInfo != "" {
		parts = append(parts, costInfo)
	}
	return strings.Join(parts, "  ")
}

func (a *App) costSummaryShort() string {
	if a.engine == nil || a.engine.CostTracker() == nil {
		return ""
	}
	ct := a.engine.CostTracker()
	in, out := ct.TotalTokens()
	if in+out == 0 {
		return ""
	}
	return fmt.Sprintf("%dk tokens · %s", (in+out)/1000, formatUSD(ct.TotalCost()))
}

func (a *App) activeModel() string {
	if a.engine == nil || a.engine.Config() == nil || strings.TrimSpace(a.engine.Config().Model) == "" {
		return "anthropic/claude-sonnet-4-20250514"
	}
	return a.engine.Config().Model
}

func (a *App) recommendedSetupProvider() string {
	if a.engine != nil && a.engine.Config() != nil && strings.TrimSpace(a.engine.Config().Model) != "" {
		return parseProvider(a.engine.Config().Model)
	}
	return currentProviderFromPrompt(a.startupPrompt)
}

func (a *App) authErrorProvider(err string) string {
	lower := strings.ToLower(err)
	current := a.recommendedSetupProvider()

	if current == "anthropic" && looksLikeAuthError(lower, "anthropic") {
		return "anthropic"
	}
	if current == "openai" && looksLikeAuthError(lower, "openai") {
		return "openai"
	}

	if looksLikeAuthError(lower, "anthropic") {
		return "anthropic"
	}
	if looksLikeAuthError(lower, "openai") {
		return "openai"
	}
	return ""
}
