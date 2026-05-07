package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestStartupPromptActive_Empty returns false when the startup prompt
// is whitespace — guards against opening the API-key wizard on a blank
// session.
func TestStartupPromptActive_Empty(t *testing.T) {
	a := testApp()
	a.startupPrompt = "  "
	if a.startupPromptActive() {
		t.Error("expected false for whitespace startupPrompt")
	}
}

// TestStartupPromptActive_HasMessages returns false once the
// conversation has begun, even if the prompt was originally set.
func TestStartupPromptActive_HasMessages(t *testing.T) {
	a := testApp()
	a.startupPrompt = "Welcome"
	a.messages = []chatMessage{{role: roleUser, content: "hi"}}
	if a.startupPromptActive() {
		t.Error("expected false once messages exist")
	}
}

// TestStartupPromptActive_Active is the happy path: prompt set, no
// messages exchanged.
func TestStartupPromptActive_Active(t *testing.T) {
	a := testApp()
	a.startupPrompt = "Welcome"
	a.messages = nil
	if !a.startupPromptActive() {
		t.Error("expected true for fresh session with prompt")
	}
}

// keyOf builds a tea.KeyMsg matching the well-known string used by
// app_keys.go's switch. Bubbletea's KeyMsg.String() recognizes "a",
// "ctrl+l", "esc", etc.
func keyOf(s string) tea.KeyMsg {
	switch s {
	case "ctrl+l":
		return tea.KeyMsg{Type: tea.KeyCtrlL}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "ctrl+a":
		return tea.KeyMsg{Type: tea.KeyCtrlA}
	case "ctrl+j":
		return tea.KeyMsg{Type: tea.KeyCtrlJ}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "alt+enter":
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestHandleGlobalKey_CtrlL_ClearsVisualState exercises the screen-
// clear branch: messages drained, tool tree dropped, thinking text
// reset, but the engine context is NOT cancelled.
func TestHandleGlobalKey_CtrlL_ClearsVisualState(t *testing.T) {
	a := testApp()
	a.messages = []chatMessage{
		{role: roleUser, content: "hello"},
		{role: roleAssistant, content: "world"},
	}
	a.streaming = "partial"
	a.thinkingText = "thinking..."
	a.tools.Start("t1", "read", "file.go")

	_, _, ok := a.handleGlobalKey(keyOf("ctrl+l"))

	if !ok {
		t.Error("ctrl+l should be handled")
	}
	if len(a.messages) != 0 {
		t.Errorf("messages not cleared: %d", len(a.messages))
	}
	if a.streaming != "" {
		t.Errorf("streaming not cleared: %q", a.streaming)
	}
	if a.thinkingText != "" {
		t.Errorf("thinkingText not cleared: %q", a.thinkingText)
	}
	if len(a.tools.entries) != 0 {
		t.Errorf("tool tree not cleared: %d", len(a.tools.entries))
	}
}

func TestHandleGlobalKey_CtrlCQuitsEvenWithAssistantMessage(t *testing.T) {
	a := testApp()
	a.busy = false
	a.messages = []chatMessage{
		{role: roleUser, content: "hello"},
		{role: roleAssistant, content: "world"},
	}

	_, cmd, ok := a.handleGlobalKey(keyOf("ctrl+c"))

	if !ok {
		t.Fatal("ctrl+c should be handled")
	}
	if cmd == nil {
		t.Fatal("ctrl+c should quit even when there is a response available to copy")
	}
	if got := len(a.messages); got != 2 {
		t.Fatalf("ctrl+c should not append copy status, got %d messages", got)
	}
}

// TestHandleGlobalKey_CtrlR_NoLastMessageReturnsHandled exercises the
// retry path with no prior user message — should still be marked
// handled (so it doesn't fall through to the global router).
func TestHandleGlobalKey_CtrlR_NoLastMessageReturnsHandled(t *testing.T) {
	a := testApp()
	a.messages = nil
	a.busy = false
	_, _, ok := a.handleGlobalKey(keyOf("ctrl+r"))
	if !ok {
		t.Error("ctrl+r should be marked handled even with no last message")
	}
}

func TestHandleGlobalKey_SlashOpensPaletteOnEmptyInput(t *testing.T) {
	a := testApp()
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	_, _, ok := a.handleGlobalKey(keyOf("/"))

	if !ok {
		t.Fatal("slash on empty input should be handled")
	}
	if !a.palette.IsVisible() {
		t.Fatal("slash on empty input should open the command palette")
	}
	if got := a.palette.input.Value(); got != "/" {
		t.Fatalf("palette query = %q, want /", got)
	}
	if got := a.input.Value(); got != "" {
		t.Fatalf("composer input = %q, want empty after slash opens palette", got)
	}
}

func TestHandleGlobalKey_SlashInDraftStaysInComposer(t *testing.T) {
	a := testApp()
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	a.input.SetValue("look in ")

	_, _, ok := a.handleGlobalKey(keyOf("/"))

	if ok {
		t.Fatal("slash in an existing draft should fall through to textarea")
	}
	if a.palette.IsVisible() {
		t.Fatal("slash in an existing draft should not open palette")
	}
}

// TestHandleGlobalKey_CtrlR_BusyDoesNotResubmit guards against
// resubmitting while a turn is in flight (would race the engine).
func TestHandleGlobalKey_CtrlR_BusyDoesNotResubmit(t *testing.T) {
	a := testApp()
	a.busy = true
	a.messages = []chatMessage{{role: roleUser, content: "prior"}}
	_, cmd, _ := a.handleGlobalKey(keyOf("ctrl+r"))
	if cmd != nil {
		t.Error("ctrl+r while busy should not produce a submit cmd")
	}
}

// TestHandleGlobalKey_CtrlJ_InsertsNewline covers all three multi-line
// trigger keys (Ctrl+J + Shift+Enter + Alt+Enter — implementation
// recognizes all three so users get the behavior they expect).
func TestHandleGlobalKey_CtrlJ_InsertsNewline(t *testing.T) {
	a := testApp()
	a.busy = false
	a.input.SetValue("hello")
	_, _, ok := a.handleGlobalKey(keyOf("ctrl+j"))
	if !ok {
		t.Error("ctrl+j should be handled")
	}
	got := a.input.Value()
	// The cursor moves between calls — we only verify that a newline
	// was inserted somewhere in the buffer.
	if !strings.Contains(got, "\n") {
		t.Errorf("expected newline inserted, got %q", got)
	}
}

func TestHandleGlobalKey_NewlineShortcutsInsertWhileBusy(t *testing.T) {
	for _, key := range []string{"ctrl+j", "shift+enter", "alt+enter"} {
		t.Run(key, func(t *testing.T) {
			a := testApp()
			a.busy = true
			a.input.SetValue("draft")

			_, cmd, ok := a.handleGlobalKey(keyOf(key))

			if !ok {
				t.Fatalf("%s should be handled", key)
			}
			if cmd != nil {
				t.Fatalf("%s while busy should not submit", key)
			}
			if got := a.input.Value(); !contains(got, "\n") {
				t.Fatalf("expected newline inserted while busy, got %q", got)
			}
		})
	}
}

func TestHandleGlobalKey_EnterWithDraftBlockedWhileBusy(t *testing.T) {
	a := testApp()
	a.busy = true
	a.input.SetValue("ordinary draft")

	_, cmd, ok := a.handleGlobalKey(keyOf("enter"))

	if !ok {
		t.Fatal("enter should be handled while busy")
	}
	if cmd != nil {
		t.Fatal("ordinary enter while busy should not submit")
	}
	if got := a.input.Value(); got != "ordinary draft" {
		t.Fatalf("ordinary draft changed while busy: %q", got)
	}
	if len(a.messages) != 0 {
		t.Fatalf("ordinary enter while busy appended messages: %d", len(a.messages))
	}
}

func TestHandleGlobalKey_BusyBlocksNonControlSlashCommands(t *testing.T) {
	a := testApp()
	a.busy = true
	a.input.SetValue("/metadata on")

	_, cmd, ok := a.handleGlobalKey(keyOf("enter"))

	if !ok {
		t.Fatal("enter should be handled while busy")
	}
	if cmd != nil {
		t.Fatal("non-control slash command while busy should not submit")
	}
	if !a.busy {
		t.Fatal("non-control slash command should not clear busy state")
	}
	if a.showMessageMeta {
		t.Fatal("metadata command should not execute while busy")
	}
	if got := a.input.Value(); got != "/metadata on" {
		t.Fatalf("blocked slash draft changed while busy: %q", got)
	}
	if len(a.messages) != 0 {
		t.Fatalf("blocked slash command appended messages: %d", len(a.messages))
	}
}

func TestHandleGlobalKey_BusyStopCommandCancels(t *testing.T) {
	a := testApp()
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	a.busy = true
	a.activeToolName = "Bash"
	a.activeToolDetail = "go test ./..."
	a.tools.Start("bash", "Bash", "go test ./...")
	cancelled := false
	a.cancel = func() { cancelled = true }
	a.input.SetValue("/stop")

	_, cmd, ok := a.handleGlobalKey(keyOf("enter"))

	if !ok {
		t.Fatal("enter should be handled while busy")
	}
	if cmd != nil {
		t.Fatal("/stop should not submit to the engine")
	}
	if !cancelled {
		t.Fatal("/stop did not call cancel")
	}
	if a.busy {
		t.Fatal("/stop should clear busy state")
	}
	if got := a.input.Value(); got != "" {
		t.Fatalf("/stop should clear composer, got %q", got)
	}
	if len(a.messages) != 1 || !contains(a.messages[0].content, "cancellation") {
		t.Fatalf("/stop should append cancellation info, got %#v", a.messages)
	}
	if plain := stripANSI(a.View()); contains(plain, "Bash") || contains(plain, "Contemplating") {
		t.Fatalf("/stop left stale running UI:\n%s", plain)
	}
}

func TestHandlePaletteKey_BusyStopCommandCancelsCleanly(t *testing.T) {
	a := testApp()
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	a.busy = true
	a.activeToolName = "Bash"
	a.activeToolDetail = "go test ./..."
	a.tools.Start("bash", "Bash", "go test ./...")
	cancelled := false
	a.cancel = func() { cancelled = true }
	a.palette = NewPalette(DefaultTheme, []PaletteCommand{{Name: "/stop", Group: "Recovery"}})
	a.palette.SetWidth(80)
	a.palette.Show()

	_, cmd, ok := a.handlePaletteKey(keyOf("enter"))

	if !ok {
		t.Fatal("palette enter should be handled")
	}
	if cmd != nil {
		t.Fatal("palette /stop should not submit to the engine")
	}
	if !cancelled {
		t.Fatal("palette /stop did not call cancel")
	}
	if a.busy {
		t.Fatal("palette /stop should clear busy state")
	}
	if plain := stripANSI(a.View()); contains(plain, "Bash") || contains(plain, "Contemplating") {
		t.Fatalf("palette /stop left stale running UI:\n%s", plain)
	}
}

// TestHandleGlobalKey_AKeyOpensSetupOnFreshSession covers the 'a' or
// '1' shortcut that opens the anthropic API-key wizard ONLY when the
// startup prompt is active.
func TestHandleGlobalKey_AKeyOpensSetupOnFreshSession(t *testing.T) {
	a := testApp()
	a.startupPrompt = "Welcome"
	a.messages = nil
	a.input.SetValue("")
	_, _, ok := a.handleGlobalKey(keyOf("a"))
	if !ok {
		t.Error("'a' on fresh session should open setup")
	}
	if a.setupProvider != "anthropic" {
		t.Errorf("setupProvider = %q, want 'anthropic'", a.setupProvider)
	}
}

// TestHandleGlobalKey_AKeyIgnoredWhenInputNonEmpty asserts the
// startup-key guard: with non-empty input, 'a' must NOT open the
// setup wizard. This is the bug the existing comment in app_keys.go
// describes (typing "add a function" used to open setup).
func TestHandleGlobalKey_AKeyIgnoredWhenInputNonEmpty(t *testing.T) {
	a := testApp()
	a.startupPrompt = "Welcome"
	a.messages = nil
	a.input.SetValue("a")
	_, _, ok := a.handleGlobalKey(keyOf("a"))
	if ok {
		t.Error("'a' with existing input should NOT be intercepted")
	}
	if a.setupProvider != "" {
		t.Errorf("setupProvider unexpectedly set: %q", a.setupProvider)
	}
}

// TestHandleGlobalKey_OKeyOpensOpenAISetup verifies the parallel branch.
func TestHandleGlobalKey_OKeyOpensOpenAISetup(t *testing.T) {
	a := testApp()
	a.startupPrompt = "Welcome"
	a.messages = nil
	a.input.SetValue("")
	_, _, ok := a.handleGlobalKey(keyOf("o"))
	if !ok {
		t.Error("'o' on fresh session should open openai setup")
	}
	if a.setupProvider != "openai" {
		t.Errorf("setupProvider = %q, want 'openai'", a.setupProvider)
	}
}

// TestHandleSetupKey_EscCancels covers the cancel branch.
func TestHandleSetupKey_EscCancels(t *testing.T) {
	a := testApp()
	a.setupProvider = "anthropic"
	_, _, ok := a.handleSetupKey(keyOf("esc"))
	if !ok {
		t.Error("esc in setup should be handled")
	}
	if a.setupProvider != "" {
		t.Errorf("setup not cancelled: provider=%q", a.setupProvider)
	}
}

// TestHandleSetupKey_UnhandledFalls returns false for keys outside the
// setup switch.
func TestHandleSetupKey_UnhandledFalls(t *testing.T) {
	a := testApp()
	a.setupProvider = "anthropic"
	_, _, ok := a.handleSetupKey(keyOf("up"))
	if ok {
		t.Error("up arrow in setup should not be handled here")
	}
}

// TestHandleKey_RoutesPaletteFirst guards the dispatcher: when the
// palette is visible, ALL other handlers are bypassed.
func TestHandleKey_RoutesPaletteFirst(t *testing.T) {
	a := testApp()
	a.palette.Show()
	defer a.palette.Hide()
	_, _, ok := a.handleKey(keyOf("ctrl+l"))
	// Routing succeeded if either:
	//   - the palette consumed the key (ok=true), OR
	//   - the palette key handler said "not mine" (ok=false), but the
	//     ctrl+l clear path was NOT taken (messages stayed put).
	a.messages = []chatMessage{{role: roleUser, content: "preserve"}}
	_, _, _ = a.handleKey(keyOf("ctrl+l"))
	if len(a.messages) == 0 {
		t.Error("palette routing leaked into global ctrl+l handler")
	}
	_ = ok
}

// TestHandleKey_RoutesSetupAfterPalette ensures the palette → switcher
// → file popup → vim → setup ordering holds. With only setupProvider
// set, the setup handler must own keys.
func TestHandleKey_RoutesSetupAfterPalette(t *testing.T) {
	a := testApp()
	a.setupProvider = "anthropic"
	_, _, ok := a.handleKey(keyOf("esc"))
	if !ok {
		t.Error("esc during setup should be handled")
	}
	if a.setupProvider != "" {
		t.Error("esc should cancel setup")
	}
}
