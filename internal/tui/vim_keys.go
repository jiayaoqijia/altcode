package tui

import tea "github.com/charmbracelet/bubbletea"

// handleVimKey processes vim-style navigation when the viewport has
// focus (input not focused). Returns true if the key was consumed.
func (a *App) handleVimKey(msg tea.KeyMsg) bool {
	key := msg.String()

	switch key {
	case "j":
		a.viewport.LineDown(1)
		return true
	case "k":
		a.viewport.LineUp(1)
		return true
	case "G":
		a.viewport.GotoBottom()
		return true
	case "g":
		// First 'g': wait for second 'g' via pendingG flag.
		if a.vimPendingG {
			a.viewport.GotoTop()
			a.vimPendingG = false
			return true
		}
		a.vimPendingG = true
		return true
	case "ctrl+d":
		half := a.viewport.Height / 2
		if half < 1 {
			half = 1
		}
		a.viewport.LineDown(half)
		return true
	case "ctrl+u":
		half := a.viewport.Height / 2
		if half < 1 {
			half = 1
		}
		a.viewport.LineUp(half)
		return true
	case "/":
		// Focus input for search (future: proper search).
		a.vimMode = false
		a.input.Focus()
		return true
	default:
		// Any other key clears the pending g.
		a.vimPendingG = false
		return false
	}
}
