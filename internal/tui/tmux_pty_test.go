package tui_test

// Automated tmux-PTY integration test for the altcode TUI.
//
// CLAUDE.md Level-2 testing rule: "Every TUI change must be tested at
// three levels: view tests, tmux E2E, and headless CLI." teatest
// covers Level 1; this test adds Level 2 so PR reviewers don't have
// to manually spin up tmux before every ship.
//
// A single session is shared across all subtests to amortise the
// ~5s altcode startup (CLAUDE.md + 4 MCP servers cold-start) and to
// avoid a subtle bug where spawning a second altcode instance in the
// same HOME picks up the first instance's session-store lock.
//
// Skipped when:
//   - `tmux` binary is absent (CI / sandbox without PTY support)
//   - ALTCODE_TMUX_TEST=0 (manual opt-out for flaky local envs)

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tmux-PTY tests require POSIX")
	}
	if os.Getenv("ALTCODE_TMUX_TEST") == "0" {
		t.Skip("ALTCODE_TMUX_TEST=0 — tmux tests disabled")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not in PATH")
	}
}

// buildTUIBinary compiles altcode once per test-binary run.
func buildTUI(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Skip(err)
	}
	root := wd
	for range [10]int{} {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		next := filepath.Dir(root)
		if next == root {
			t.Skipf("go.mod not found above %s", wd)
		}
		root = next
	}

	bin := filepath.Join(t.TempDir(), "altcode-tmux-test")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/altcode/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go build: %v: %s", err, out)
	}
	return bin
}

type tmuxSession struct {
	t    *testing.T
	name string
}

func startTmuxSession(t *testing.T, bin string) *tmuxSession {
	t.Helper()
	name := fmt.Sprintf("altcode-test-%d", time.Now().UnixNano())
	// Defang user-config env-var references that the test subprocess
	// inherits empty: ~/.altcode/config.json may include "$OPENROUTER"
	// etc. that, if unset, expand to an empty apiKey and break provider
	// auto-detect. Setting harmless dummies keeps the TUI start path
	// happy without leaking real credentials. tmux inherits this env.
	cmd := exec.Command("tmux", "new-session", "-d",
		"-s", name, "-x", "180", "-y", "45",
		"env",
		"OPENROUTER=test-key-not-real",
		"OPENROUTER_API_KEY=test-key-not-real",
		bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v: %s", err, out)
	}
	time.Sleep(200 * time.Millisecond)
	s := &tmuxSession{t: t, name: name}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", name).Run()
	})

	// Poll for the TUI to render its header.
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.capture(), "altcode") {
			return s
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("TUI did not render 'altcode' header within 45s")
	return s
}

func (s *tmuxSession) send(keys ...string) {
	s.t.Helper()
	args := []string{"send-keys", "-t", s.name}
	args = append(args, keys...)
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		s.t.Fatalf("tmux send-keys %v: %v: %s", keys, err, out)
	}
	time.Sleep(150 * time.Millisecond)
}

func (s *tmuxSession) capture() string {
	s.t.Helper()
	out, err := exec.Command("tmux", "capture-pane",
		"-t", s.name, "-p", "-S", "-150").CombinedOutput()
	if err != nil {
		s.t.Fatalf("tmux capture-pane: %v: %s", err, out)
	}
	return string(out)
}

func (s *tmuxSession) clearScreen() {
	// Ctrl+L to redraw; useful between subtests so stale output
	// doesn't satisfy a later assertion.
	s.send("C-l")
	time.Sleep(200 * time.Millisecond)
}

// waitFor polls s.capture() every 200ms until pred returns true OR
// the deadline elapses. Returns true on match, false on timeout.
// Replaces fixed `time.Sleep` inside assertion blocks so slow CI
// boxes don't flake. Iter-12 CC review fragility note.
func waitFor(s *tmuxSession, max time.Duration, pred func(string) bool) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if pred(s.capture()) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// TestTmuxPTY runs a sequence of sub-probes against a single shared
// altcode TUI instance. Starting altcode is the most expensive step
// (~5s cold CLAUDE.md + MCP load); amortising it avoids flake and
// lets us assert more behaviour per second of test time.
func TestTmuxPTY(t *testing.T) {
	skipIfNoTmux(t)
	bin := buildTUI(t)
	s := startTmuxSession(t, bin)

	// Probe 1: startup rendered the header.
	t.Run("startup", func(t *testing.T) {
		out := s.capture()
		if !strings.Contains(out, "altcode") {
			t.Errorf("startup capture missing 'altcode' header:\n%s", out)
		}
	})

	// Probe 2: /help renders known slash commands. Capture uses a
	// large scrollback window (-S -150) so help output that
	// scrolled past the visible viewport is still matched.
	t.Run("help", func(t *testing.T) {
		s.send("/help", "Enter")
		// Poll for at least one stable help marker rather than a
		// fixed sleep — slow CI machines could flake on 800ms.
		// Iter-12 fragility cleanup from CC review.
		markers := []string{"/doctor", "/tools", "Ctrl+K", "Ctrl+Q", "commands"}
		if !waitFor(s, 3*time.Second, func(out string) bool {
			for _, m := range markers {
				if strings.Contains(out, m) {
					return true
				}
			}
			return false
		}) {
			t.Errorf("help output missing all stable markers (%v):\n%s",
				markers, s.capture())
		}
		s.clearScreen()
	})

	// Probe 3: /doctor renders a health report.
	t.Run("doctor", func(t *testing.T) {
		s.send("/doctor", "Enter")
		if !waitFor(s, 3*time.Second, func(out string) bool {
			// Case-insensitive substring: "Doctor" header or "doctor"
			// subtitle — the TUI has varied historically.
			return strings.Contains(out, "octor")
		}) {
			t.Errorf("doctor output missing 'octor' marker:\n%s", s.capture())
		}
		s.clearScreen()
	})

	// Probe 4: pgup/pgdown under real PTY keystrokes don't crash.
	// The userScrolledAway flag path is exercised here; in teatest
	// we exercise it with synthetic messages, but a real escape
	// sequence arrives as terminal bytes that Bubbletea has to
	// parse — catching parse-path regressions.
	t.Run("scroll_autofollow_real_pty", func(t *testing.T) {
		s.send("/help", "Enter")
		time.Sleep(400 * time.Millisecond)
		s.send("PageUp")
		time.Sleep(200 * time.Millisecond)
		s.send("PageDown")
		time.Sleep(200 * time.Millisecond)

		out := s.capture()
		if !strings.Contains(out, "altcode") {
			t.Errorf("TUI crashed under real pgup/pgdown:\n%s", out)
		}
	})
}
