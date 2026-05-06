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

// TestApp_DismissFilePopup_ClearsState covers the explicit-dismiss path.
func TestApp_DismissFilePopup_ClearsState(t *testing.T) {
	a := testApp()
	a.filePopup.visible = true
	a.filePopup.matches = []completions.Match{{Path: "a.go"}, {Path: "b.go"}}
	a.filePopup.cursor = 1

	a.dismissFilePopup()

	if a.filePopup.visible {
		t.Error("dismissFilePopup should hide")
	}
	if a.filePopup.matches != nil {
		t.Errorf("matches not cleared: %v", a.filePopup.matches)
	}
	if a.filePopup.cursor != 0 {
		t.Errorf("cursor not reset: %d", a.filePopup.cursor)
	}
}

// TestApp_FilePopupMoveDown_ClampsAtVisibleCap
func TestApp_FilePopupMoveDown_ClampsAtVisibleCap(t *testing.T) {
	a := testApp()
	a.filePopup.matches = make([]completions.Match, filePopupMaxVisible+5)
	for i := range a.filePopup.matches {
		a.filePopup.matches[i] = completions.Match{Path: "f" + string(rune('a'+i%26)) + ".go"}
	}

	for i := 0; i < filePopupMaxVisible*2; i++ {
		a.filePopupMoveDown()
	}

	if a.filePopup.cursor != filePopupMaxVisible-1 {
		t.Errorf("cursor = %d, want %d (cap)", a.filePopup.cursor, filePopupMaxVisible-1)
	}
}

// TestApp_FilePopupMoveUp_ClampsAtZero
func TestApp_FilePopupMoveUp_ClampsAtZero(t *testing.T) {
	a := testApp()
	a.filePopup.matches = []completions.Match{{Path: "a.go"}, {Path: "b.go"}}
	a.filePopup.cursor = 1
	a.filePopupMoveUp()
	a.filePopupMoveUp()
	a.filePopupMoveUp() // try going past zero
	if a.filePopup.cursor != 0 {
		t.Errorf("cursor = %d, want 0", a.filePopup.cursor)
	}
}

// TestApp_FilePopupMoveDown_FewerMatchesThanCap covers the small-list
// branch where len(matches) < filePopupMaxVisible.
func TestApp_FilePopupMoveDown_FewerMatchesThanCap(t *testing.T) {
	a := testApp()
	a.filePopup.matches = []completions.Match{{Path: "a.go"}, {Path: "b.go"}, {Path: "c.go"}}
	for i := 0; i < 10; i++ {
		a.filePopupMoveDown()
	}
	if a.filePopup.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (clamped at len-1)", a.filePopup.cursor)
	}
}

// TestMin covers the local int helper.
func TestMin(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{1, 2, 1},
		{2, 1, 1},
		{0, 0, 0},
		{-3, -1, -3},
	}
	for _, c := range cases {
		if got := min(c.a, c.b); got != c.want {
			t.Errorf("min(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestApp_FilePopupView_HiddenReturnsEmpty
func TestApp_FilePopupView_HiddenReturnsEmpty(t *testing.T) {
	a := testApp()
	a.width = 80
	a.filePopup.visible = false
	if got := a.filePopupView(); got != "" {
		t.Errorf("hidden filePopupView = %q, want empty", got)
	}
}

// TestApp_FilePopupView_NoMatchesReturnsEmpty exercises the second
// guard — visible but no matches → no rendered popup.
func TestApp_FilePopupView_NoMatchesReturnsEmpty(t *testing.T) {
	a := testApp()
	a.width = 80
	a.filePopup.visible = true
	a.filePopup.matches = nil
	if got := a.filePopupView(); got != "" {
		t.Errorf("empty-matches view = %q, want empty", got)
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
