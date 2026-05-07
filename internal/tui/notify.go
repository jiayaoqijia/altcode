package tui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// notifyThreshold is the minimum elapsed turn time before we emit a
// desktop notification. Short turns don't need them — the user is
// watching. 20s is the threshold DS-TUI uses; we match it.
const notifyThreshold = 20 * time.Second

// emitTurnNotification writes an OSC 9 notification escape sequence
// to stdout when the just-finished turn took longer than
// notifyThreshold. Modern terminals (iTerm2, kitty, WezTerm, gnome-
// terminal with notification support, Konsole) intercept the escape
// and surface a system notification. Terminals that don't understand
// OSC 9 simply drop the sequence silently.
//
// Format: ESC ] 9 ; <text> BEL
//   - iTerm2 / kitty: native notification
//   - gnome-terminal: forwards via desktop-notify (when allowed)
//   - tmux: passes through if `set -g allow-passthrough on`
//
// Suppressed when:
//   - elapsed < threshold (turn was short, user was watching)
//   - $ALTCODE_NOTIFY=0 (explicit user opt-out)
//   - stdout is not a tty (headless mode shouldn't ping a desktop)
//
// Bell is also rung on a 30s cooldown so a series of short follow-up
// turns doesn't spam the user. Cooldown is independent of this
// function — see App.lastBell handling in the app layer.
func emitTurnNotification(elapsed time.Duration, summary string) {
	if elapsed < notifyThreshold {
		return
	}
	if os.Getenv("ALTCODE_NOTIFY") == "0" {
		return
	}
	// Don't notify when stdout isn't a tty (headless exec, scripts).
	if fi, err := os.Stdout.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		return
	}
	// Build a short, single-line message — terminals truncate long
	// notification bodies and embedded newlines confuse some
	// implementations.
	msg := fmt.Sprintf("altcode: turn done in %s", formatNotifyDuration(elapsed))
	if summary != "" {
		// Strip leading "✓ " from the turn-summary string and any
		// embedded newlines / ANSI codes. Keep the trailing parts
		// trimmed so the notification fits typical 256-char limits.
		trimmed := strings.TrimPrefix(summary, "✓ ")
		trimmed = strings.ReplaceAll(stripANSI(trimmed), "\n", " · ")
		if len(trimmed) > 120 {
			trimmed = trimmed[:117] + "…"
		}
		msg += " — " + trimmed
	}
	fmt.Fprintf(os.Stdout, "\x1b]9;%s\x07", msg)
}

// formatNotifyDuration is a compact CC-style formatter for the
// notification body — shorter than formatDuration so it fits in
// the typically-narrow notification area.
func formatNotifyDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) - mins*60
	if secs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm%ds", mins, secs)
}
