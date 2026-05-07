package tui

import (
	"testing"
	"time"
)

func TestFormatNotifyDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{1 * time.Minute, "1m"},
		{1*time.Minute + 5*time.Second, "1m5s"},
		{2*time.Minute + 30*time.Second, "2m30s"},
		{0, "0s"},
	}
	for _, c := range cases {
		if got := formatNotifyDuration(c.in); got != c.want {
			t.Errorf("formatNotifyDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEmitTurnNotification_BelowThreshold returns silently when the
// elapsed time is shorter than the threshold (no spam on quick turns).
func TestEmitTurnNotification_BelowThreshold(t *testing.T) {
	// Capture stdout — but emitTurnNotification short-circuits on
	// non-tty stdout (testing.T runs without a tty), so this is
	// double-belt-and-braces. The test mainly ensures no panic.
	emitTurnNotification(5*time.Second, "✓ 1 file read · 5s")
}

func TestEmitTurnNotification_DisabledByEnv(t *testing.T) {
	t.Setenv("ALTCODE_NOTIFY", "0")
	emitTurnNotification(60*time.Second, "✓ done")
}
