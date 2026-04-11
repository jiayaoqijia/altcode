package tui

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/completions"
)

// TestExtractAtQuery covers the @ mention parser used to drive the file
// completion popup. The popup must activate exactly when the cursor is
// inside an unfinished @-token.
func TestExtractAtQuery(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantQuery string
		wantOK    bool
	}{
		{"empty", "", "", false},
		{"no at sign", "look at this", "", false},
		{"bare at", "@", "", true},
		{"partial query", "look @main", "main", true},
		{"completed by space", "look @main ", "", false},
		{"completed by newline", "look @main\nbug", "", false},
		{"second at", "@first @second", "second", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, ok := extractAtQuery(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if query != tt.wantQuery {
				t.Fatalf("query = %q, want %q", query, tt.wantQuery)
			}
		})
	}
}

// TestAcceptFileCompletion_PreservesTrailingText is a regression guard
// for the bug where typing `look at @ma<TAB> for the bug` would lose
// ` for the bug` because acceptFileCompletion spliced on idx alone.
func TestAcceptFileCompletion_PreservesTrailingText(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		path    string
		want    string
	}{
		{
			name:    "no trailing text",
			initial: "look at @ma",
			path:    "main.go",
			want:    "look at main.go",
		},
		{
			name:    "trailing text after query",
			initial: "look at @ma for the bug",
			path:    "main.go",
			want:    "look at main.go for the bug",
		},
		{
			name:    "trailing newline",
			initial: "review @hand\nplease",
			path:    "handlers/auth.go",
			want:    "review handlers/auth.go\nplease",
		},
		{
			name:    "@ at end of input",
			initial: "compare @",
			path:    "README.md",
			want:    "compare README.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(nil, DefaultTheme, "test", "")
			app.input.SetValue(tt.initial)
			app.filePopup.visible = true
			app.filePopup.matches = []completions.Match{{Path: tt.path}}
			app.filePopup.cursor = 0

			if !app.acceptFileCompletion() {
				t.Fatal("acceptFileCompletion returned false")
			}
			if got := app.input.Value(); got != tt.want {
				t.Fatalf("input = %q, want %q", got, tt.want)
			}
			if app.filePopup.visible {
				t.Fatal("popup should be hidden after accept")
			}
		})
	}
}
