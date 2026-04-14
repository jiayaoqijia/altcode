package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderImage_Fallback(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "xterm")
	t.Setenv("KITTY_WINDOW_ID", "")
	result := RenderImage([]byte("fake image data"), "test.png")
	if !strings.Contains(result, "[Image:") {
		t.Errorf("expected fallback placeholder, got: %s", result)
	}
	if !strings.Contains(result, "test.png") {
		t.Errorf("expected filename in fallback, got: %s", result)
	}
	if !strings.Contains(result, "15 bytes") {
		t.Errorf("expected byte count in fallback, got: %s", result)
	}
}

func TestRenderImage_iTerm2(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	t.Setenv("KITTY_WINDOW_ID", "")
	result := RenderImage([]byte("fake"), "test.png")
	if !strings.Contains(result, "\033]1337;File=") {
		t.Errorf("expected iTerm2 escape sequence, got: %s", result)
	}
	if !strings.Contains(result, "inline=1") {
		t.Errorf("expected inline=1, got: %s", result)
	}
}

func TestRenderImage_WezTerm(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WezTerm")
	t.Setenv("KITTY_WINDOW_ID", "")
	result := RenderImage([]byte("data"), "pic.jpg")
	if !strings.Contains(result, "\033]1337;File=") {
		t.Errorf("expected iTerm2 escape for WezTerm, got: %s", result)
	}
}

func TestSupportsInlineImages(t *testing.T) {
	tests := []struct {
		name        string
		termProgram string
		kittyID     string
		want        bool
	}{
		{"iTerm2", "iTerm.app", "", true},
		{"WezTerm", "WezTerm", "", true},
		{"Mintty", "mintty", "", true},
		{"xterm", "xterm", "", false},
		{"empty", "", "", false},
		{"Kitty", "", "12345", true},
		{"KittyWithTerm", "xterm", "12345", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", tt.termProgram)
			t.Setenv("KITTY_WINDOW_ID", tt.kittyID)
			got := supportsInlineImages()
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderImageFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TERM_PROGRAM", "xterm")
	t.Setenv("KITTY_WINDOW_ID", "")
	result, err := RenderImageFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[Image: test.png") {
		t.Errorf("expected fallback with filename, got: %s", result)
	}
}

func TestRenderImageFromFile_NotFound(t *testing.T) {
	_, err := RenderImageFromFile("/no/such/file.png")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRenderImageBase64_Fallback(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "xterm")
	t.Setenv("KITTY_WINDOW_ID", "")
	data := []byte("hello image bytes")
	b64 := base64.StdEncoding.EncodeToString(data)
	result := RenderImageBase64(b64, "image/png", "out.png")
	if !strings.Contains(result, "[Image: out.png") {
		t.Errorf("expected fallback, got: %s", result)
	}
	if !strings.Contains(result, "17 bytes") {
		t.Errorf("expected byte count, got: %s", result)
	}
}

func TestRenderImageBase64_iTerm2(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	t.Setenv("KITTY_WINDOW_ID", "")
	data := []byte("img")
	b64 := base64.StdEncoding.EncodeToString(data)
	result := RenderImageBase64(b64, "image/png", "out.png")
	if !strings.Contains(result, "\033]1337;File=") {
		t.Errorf("expected iTerm2 escape, got: %s", result)
	}
}

func TestRenderImageBase64_InvalidBase64(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "xterm")
	t.Setenv("KITTY_WINDOW_ID", "")
	result := RenderImageBase64("not-valid-base64!!!", "image/png", "bad.png")
	if !strings.Contains(result, "[Image: bad.png]") {
		t.Errorf("expected simple fallback for bad b64, got: %s", result)
	}
}

func TestRenderImage_EmptyData(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "xterm")
	t.Setenv("KITTY_WINDOW_ID", "")
	result := RenderImage([]byte{}, "empty.png")
	if !strings.Contains(result, "0 bytes") {
		t.Errorf("expected 0 bytes in fallback, got: %s", result)
	}
}

func TestRenderImage_CaseInsensitive(t *testing.T) {
	// TERM_PROGRAM matching should be case-insensitive
	t.Setenv("TERM_PROGRAM", "ITERM.APP")
	t.Setenv("KITTY_WINDOW_ID", "")
	result := RenderImage([]byte("x"), "t.png")
	if !strings.Contains(result, "\033]1337;File=") {
		t.Errorf("expected case-insensitive match, got: %s", result)
	}
}
