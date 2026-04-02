package tui

import (
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/event"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

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

func TestAppIgnoresTypingWhileBusy(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.busy = true

	model, _ := app.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'x'},
	})
	app = model.(*App)

	if got := app.input.Value(); got != "" {
		t.Fatalf("expected busy app to ignore typing, got %q", got)
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

	if got := app.input.Placeholder; got != "Press Enter to set up your OpenAI API key" {
		t.Fatalf("unexpected placeholder: %q", got)
	}
}

func TestWelcomeViewUsesCompactLayoutInSmallViewport(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.width = 60
	app.viewport = viewport.New(60, 6)

	view := app.welcomeView()
	if !strings.Contains(view, "altcode  vtest") {
		t.Fatalf("expected compact header in small viewport, got %q", view)
	}
	if strings.Contains(view, "_____") {
		t.Fatalf("expected compact welcome without large wordmark, got %q", view)
	}
}

func TestAuthErrorRepromptsForReplacementKey(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "")
	app.viewport = viewport.New(80, 20)
	app.messages = []string{"> hello"}

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
