package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type eventMsg event.Event
type streamDoneMsg struct{}

// App is the top-level Bubbletea model for altcode.
type App struct {
	engine   *engine.Engine
	theme    Theme
	input    textarea.Model
	viewport viewport.Model
	width    int
	height   int

	messages  []string
	streaming string
	busy      bool
	cancel    context.CancelFunc
	events    <-chan event.Event
	tokenInfo string
}

// New creates a new App backed by the given engine and theme.
func New(eng *engine.Engine, theme Theme) *App {
	ti := textarea.New()
	ti.Placeholder = "Ask anything... (Ctrl+D to submit, Esc to quit)"
	ti.Focus()
	ti.SetHeight(3)
	ti.ShowLineNumbers = false

	return &App{
		engine: eng,
		theme:  theme,
		input:  ti,
	}
}

func (a *App) Init() tea.Cmd {
	return textarea.Blink
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKey(msg)
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.viewport = viewport.New(msg.Width, msg.Height-6)
		a.input.SetWidth(msg.Width - 2)
		a.updateViewport()
		return a, nil
	case eventMsg:
		return a.handleEvent(event.Event(msg))
	case streamDoneMsg:
		return a, nil
	}

	if !a.busy {
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		return a, cmd
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if a.busy {
			if a.cancel != nil {
				a.cancel()
			}
			a.busy = false
			return a, nil
		}
		return a, tea.Quit
	case "ctrl+d":
		if a.busy || strings.TrimSpace(a.input.Value()) == "" {
			return a, nil
		}
		return a, a.submit()
	case "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) handleEvent(ev event.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case event.TextDelta:
		a.streaming += ev.Text
		a.updateViewport()
		return a, a.waitForEvent()
	case event.TextDone:
		return a, a.waitForEvent()
	case event.UsageEvent:
		if ev.Usage != nil {
			a.tokenInfo = fmt.Sprintf("tokens: %d in / %d out",
				ev.Usage.InputTokens, ev.Usage.OutputTokens)
		}
		return a, a.waitForEvent()
	case event.ErrorEvent:
		a.messages = append(a.messages,
			fmt.Sprintf("[error] %s", ev.Error))
		a.streaming = ""
		a.busy = false
		a.updateViewport()
		return a, nil
	case event.Done:
		if a.streaming != "" {
			a.messages = append(a.messages, a.streaming)
			a.streaming = ""
		}
		a.busy = false
		a.updateViewport()
		return a, nil
	}
	return a, a.waitForEvent()
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	header := lipgloss.NewStyle().
		Foreground(a.theme.Primary).
		Bold(true).
		Render("altcode") +
		lipgloss.NewStyle().
			Foreground(a.theme.Muted).
			Render("  "+a.tokenInfo)

	sep := lipgloss.NewStyle().
		Foreground(a.theme.Border).
		Render(strings.Repeat("─", a.width))

	status := ""
	if a.busy {
		status = lipgloss.NewStyle().
			Foreground(a.theme.Warning).
			Render("  streaming...")
	}

	return fmt.Sprintf("%s\n%s\n%s%s\n%s\n%s",
		header, sep, a.viewport.View(), status, sep, a.input.View())
}

func (a *App) submit() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	a.input.Reset()
	a.messages = append(a.messages, fmt.Sprintf("> %s", text))
	a.streaming = ""
	a.busy = true
	a.updateViewport()

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.events = a.engine.Run(ctx, text)

	return a.waitForEvent()
}

func (a *App) waitForEvent() tea.Cmd {
	ch := a.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return eventMsg(ev)
	}
}

func (a *App) updateViewport() {
	var sb strings.Builder
	for _, m := range a.messages {
		sb.WriteString(m)
		sb.WriteString("\n\n")
	}
	if a.streaming != "" {
		sb.WriteString(a.streaming)
	}
	a.viewport.SetContent(sb.String())
	a.viewport.GotoBottom()
}
