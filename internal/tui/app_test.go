package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jiayaoqijia/altcode/internal/event"
	"github.com/jiayaoqijia/altcode/internal/workspace"
)

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestAppTypingUpdatesTextarea(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")

	model, _ := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'h'},
	})
	app = model.(*App)

	model, _ = app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'i'},
	})
	app = model.(*App)

	if got := app.input.Value(); got != "hi" {
		t.Fatalf("expected typed input to be preserved, got %q", got)
	}
}

func TestAppCtrlJInsertsNewline(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.input.SetValue("hello")

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	app = model.(*App)

	if got := app.input.Value(); got != "hello\n" {
		t.Fatalf("expected ctrl+j to insert newline, got %q", got)
	}
}

func TestAppComposerGrowthRecomputesViewportHeight(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")

	model, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	app = model.(*App)
	initialViewportHeight := app.viewport.Height

	app.input.SetValue("hello")
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	app = model.(*App)

	if got := app.input.Height(); got != 2 {
		t.Fatalf("expected composer text height to grow to 2, got %d", got)
	}
	if got, want := app.viewport.Height, initialViewportHeight-1; got != want {
		t.Fatalf("viewport height = %d, want %d after composer growth", got, want)
	}
}

func TestAppWorkspaceViewFitsTerminalWithComposer(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.wsView = NewWorkspaceView(&workspace.WorkspaceSession{
		ID:     "01FIT",
		Task:   "fit workspace",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"worker": {
				Role:          "worker",
				Backend:       "codex",
				ActivityState: workspace.ActivityActive,
			},
		},
	})
	app.busy = true

	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	app = model.(*App)

	if got := renderedLineCount(app.View()); got > app.height {
		t.Fatalf("workspace view rendered %d rows, want <= %d", got, app.height)
	}
}

func TestAppTeamViewShrinksForGrowingComposer(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.teamView.Start([]teamRole{
		{Role: "architect", Backend: "codex"},
		{Role: "implementer", Backend: "codex"},
	})
	app.busy = true

	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	app = model.(*App)
	app.input.SetValue("line one")
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	app = model.(*App)

	if got := renderedLineCount(app.View()); got > app.height {
		t.Fatalf("team view rendered %d rows after composer growth, want <= %d", got, app.height)
	}
}

func TestAppWorkflowHeaderFitsWithComposer(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.teamView.Start([]teamRole{
		{Role: "planner", Backend: "codex"},
		{Role: "tester", Backend: "codex"},
	})
	app.wfRunning = true
	app.busy = true
	app.wfHeader.SetPhases([]string{"plan", "test"})
	app.wfHeader.MarkActive("test")

	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	app = model.(*App)

	if got := renderedLineCount(app.View()); got > app.height {
		t.Fatalf("workflow view rendered %d rows, want <= %d", got, app.height)
	}
}

func TestAppOnDoneRecomputesLayoutAfterBusyClears(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	model, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	app = model.(*App)
	app.busy = true
	app.applyLayout(false)
	busyHeight := app.viewport.Height

	app.onDone()

	if !app.viewport.AtBottom() {
		t.Fatal("expected viewport to remain at bottom after completion")
	}
	if got := app.viewport.Height; got != busyHeight {
		t.Fatalf("viewport height = %d, want stable height %d after busy clears", got, busyHeight)
	}
}

func renderedLineCount(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func TestAppAllowsTypingWhileBusy(t *testing.T) {
	// Users need to type /spawn, /send, /quit during workspace mode
	app := New(nil, DefaultTheme, "test", "")
	app.busy = true

	model, _ := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'x'},
	})
	app = model.(*App)

	if got := app.input.Value(); got != "x" {
		t.Fatalf("expected typing to work while busy, got %q", got)
	}
}

func TestWelcomeViewShowsStartupPrompt(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "No Anthropic credentials detected.")

	view := app.welcomeView()
	if !strings.Contains(view, "Let's get altcode connected") {
		t.Fatalf("expected welcome view to show setup section, got %q", view)
	}
	if !strings.Contains(view, "Recommended next step") {
		t.Fatalf("expected welcome view to include onboarding guidance, got %q", view)
	}
}

func TestEnterStartsRecommendedSetupWhenStartupPromptPresent(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "No Anthropic credentials detected.")

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(*App)

	if app.setupProvider != "anthropic" {
		t.Fatalf("expected recommended anthropic setup, got %q", app.setupProvider)
	}
}

func TestStartupMenuOpensAnthropicSetup(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "No Anthropic credentials detected.")

	model, _ := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'a'},
	})
	app = model.(*App)

	if app.setupProvider != "anthropic" {
		t.Fatalf("expected anthropic setup provider, got %q", app.setupProvider)
	}
}

func TestInputPlaceholderGuidesRecommendedSetup(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "No OpenAI credentials detected.")
	app.updateInputPlaceholder()

	if got := app.input.Placeholder; got != "Press Enter to set up your OpenAI API key" {
		t.Fatalf("unexpected placeholder: %q", got)
	}
}

func TestInputPlaceholderKeepsCredentialSetupWhileBusy(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "No OpenAI credentials detected.")
	app.busy = true

	app.updateInputPlaceholder()

	if got := app.input.Placeholder; got != "Press Enter to set up your OpenAI API key" {
		t.Fatalf("credential setup placeholder was replaced while busy: %q", got)
	}
}

func TestInputPlaceholderSuggestsInitWithoutAgentContext(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.projectRoot = t.TempDir()

	app.updateInputPlaceholder()

	if got := app.input.Placeholder; !strings.Contains(got, "/init") {
		t.Fatalf("expected /init guidance without agent context, got %q", got)
	}
}

func TestInputPlaceholderSuggestsFilesWithAgentContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := New(nil, DefaultTheme, "test", "")
	app.projectRoot = root

	app.updateInputPlaceholder()

	got := app.input.Placeholder
	if !strings.Contains(got, "@file") || !strings.Contains(got, "Ctrl+K") {
		t.Fatalf("expected file and palette guidance with agent context, got %q", got)
	}
}

func TestInputPlaceholderBusyMentionsDraftAndStop(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.busy = true

	app.updateInputPlaceholder()

	got := app.input.Placeholder
	if !strings.Contains(got, "Draft") || !strings.Contains(got, "/stop") {
		t.Fatalf("expected busy draft guidance, got %q", got)
	}
}

func TestAppWindowResizeInvalidatesRenderCache(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.messages = []chatMessage{{role: roleAssistant, content: "fresh after resize"}}
	app.renderCache = "stale cached transcript"
	app.renderCacheLen = len(app.messages)

	model, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	app = model.(*App)

	if strings.Contains(app.viewport.View(), "stale cached transcript") {
		t.Fatalf("resize reused stale render cache:\n%s", app.viewport.View())
	}
	if !strings.Contains(stripANSI(app.viewport.View()), "fresh after resize") {
		t.Fatalf("resize did not rebuild viewport from messages:\n%s", app.viewport.View())
	}
}

func TestWelcomeViewUsesCompactLayoutInSmallViewport(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.width = 60
	app.viewport = viewport.New(60, 6)

	view := app.welcomeView()
	if !strings.Contains(view, "altcode ready") {
		t.Fatalf("expected ready empty state in small viewport, got %q", view)
	}
	if strings.Contains(view, "Type a prompt") {
		t.Fatalf("empty state should not duplicate status guidance, got %q", view)
	}
}

func TestAuthErrorRepromptsForReplacementKey(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.viewport = viewport.New(80, 20)
	app.messages = []chatMessage{{role: roleUser, content: "hello"}}

	model, _ := app.handleEvent(event.Event{
		Type:  event.ErrorEvent,
		Error: `anthropic status 401: {"error":{"message":"invalid x-api-key"}}`,
	})
	app = model.(*App)

	if app.setupProvider != "anthropic" {
		t.Fatalf("expected anthropic re-prompt, got %q", app.setupProvider)
	}
	if !strings.Contains(app.setupError, "rejected the current API key") {
		t.Fatalf("expected replacement key guidance, got %q", app.setupError)
	}
}

func TestMetadataDefaultHiddenAndToggleOnOff(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.width = 100

	hidden := stripANSI(app.renderMessage(chatMessage{
		role:    roleAssistant,
		content: "done",
		meta:    "gpt-test (1s)",
	}))
	if strings.Contains(hidden, "gpt-test") {
		t.Fatalf("metadata should be hidden by default:\n%s", hidden)
	}

	handled, _ := app.handleBuiltinCommand("/metadata on")
	if !handled || !app.showMessageMeta {
		t.Fatalf("/metadata on should be handled and enable metadata")
	}
	shown := stripANSI(app.renderMessage(chatMessage{
		role:    roleAssistant,
		content: "done",
		meta:    "gpt-test (1s)",
	}))
	if !strings.Contains(shown, "gpt-test (1s)") {
		t.Fatalf("metadata should be visible after toggle on:\n%s", shown)
	}

	handled, _ = app.handleBuiltinCommand("/metadata off")
	if !handled || app.showMessageMeta {
		t.Fatalf("/metadata off should be handled and disable metadata")
	}
}
