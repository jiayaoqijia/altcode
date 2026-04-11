package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// openInExternalEditor writes initial to a temp file, launches the
// user's $EDITOR (or $VISUAL, fallback nano/vi/vim), then reads the
// edited content back. Returns the new text or an error message
// suitable for appendInfo.
//
// Mirrors Claude Code's Ctrl+G "open in external editor" feature
// so users can compose long prompts with real editing affordances
// (multi-line wrap, syntax highlighting, mouse, etc.) instead of
// fighting a 3-line textarea.
func openInExternalEditor(initial string) (string, error) {
	editor := pickEditor()
	if editor == "" {
		return initial, fmt.Errorf("no editor found — set $EDITOR or $VISUAL")
	}

	tmp, err := os.CreateTemp("", "altcode-prompt-*.md")
	if err != nil {
		return initial, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(initial); err != nil {
		tmp.Close()
		return initial, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return initial, fmt.Errorf("close temp file: %w", err)
	}

	// Launch the editor attached to the user's terminal so it can
	// take over the screen. Bubbletea will redraw on return.
	cmd := exec.Command("sh", "-c", editor+" "+tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return initial, fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return initial, fmt.Errorf("read edited prompt: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// pickEditor returns the first available editor command, mirroring
// the conventional UNIX precedence: $VISUAL > $EDITOR > nano > vi > vim.
func pickEditor() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	for _, candidate := range []string{"nano", "vim", "vi"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
