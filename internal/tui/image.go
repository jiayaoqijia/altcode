package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RenderImage returns an iTerm2 inline image escape sequence for
// terminals that support it, or a text placeholder otherwise.
// Supported: iTerm2, WezTerm, Mintty, Kitty (via iTerm2 compat).
func RenderImage(data []byte, filename string) string {
	if !supportsInlineImages() {
		return fmt.Sprintf("[Image: %s (%d bytes)]",
			filename, len(data))
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	nameB64 := base64.StdEncoding.EncodeToString([]byte(filename))
	// iTerm2 inline image protocol (OSC 1337).
	// ESC ] 1337 ; File=name=<b64>;inline=1;size=<n>:<b64data> BEL
	return fmt.Sprintf("\033]1337;File=name=%s;inline=1;size=%d:%s\a",
		nameB64, len(data), b64)
}

// RenderImageFromFile reads a file and renders it inline.
func RenderImageFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return RenderImage(data, filepath.Base(path)), nil
}

// supportsInlineImages checks whether the current terminal
// supports the iTerm2 inline image protocol (OSC 1337).
func supportsInlineImages() bool {
	term := os.Getenv("TERM_PROGRAM")
	switch strings.ToLower(term) {
	case "iterm.app", "wezterm", "mintty":
		return true
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	return false
}

// RenderImageBase64 renders an already-base64-encoded image blob.
// mediaType is e.g. "image/png". Used when the engine hands us a
// ContentPart with Source.Type=="base64".
func RenderImageBase64(b64Data, mediaType, filename string) string {
	if !supportsInlineImages() {
		// Decode to get the byte length for the placeholder.
		raw, err := base64.StdEncoding.DecodeString(b64Data)
		if err != nil {
			return fmt.Sprintf("[Image: %s]", filename)
		}
		return fmt.Sprintf("[Image: %s (%d bytes)]",
			filename, len(raw))
	}
	nameB64 := base64.StdEncoding.EncodeToString([]byte(filename))
	raw, err := base64.StdEncoding.DecodeString(b64Data)
	size := 0
	if err == nil {
		size = len(raw)
	}
	return fmt.Sprintf("\033]1337;File=name=%s;inline=1;size=%d:%s\a",
		nameB64, size, b64Data)
}
