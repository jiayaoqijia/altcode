package tui

import (
	"strings"
	"testing"
)

// (stripANSI helper is shared from workspace_view_test.go)

func TestRenderInlineDiff_Empty(t *testing.T) {
	if got := renderInlineDiff("", DefaultTheme, 0); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRenderInlineDiff_PreservesAllLines(t *testing.T) {
	in := "+++ b/file.go\n--- a/file.go\n@@ -1 +1 @@\n-old\n+new\n context\n"
	out := renderInlineDiff(in, DefaultTheme, 0)
	plain := stripANSI(out)
	for _, want := range []string{
		"+++ b/file.go",
		"--- a/file.go",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		" context",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in:\n%s", want, plain)
		}
	}
}

func TestRenderInlineDiff_RespectsMaxLines(t *testing.T) {
	in := "+a\n+b\n+c\n+d\n+e\n"
	out := renderInlineDiff(in, DefaultTheme, 3)
	plain := stripANSI(out)
	if !strings.Contains(plain, "+a") {
		t.Errorf("expected +a in capped output: %s", plain)
	}
	if strings.Contains(plain, "+e") {
		t.Errorf("did not expect +e (capped at 3 lines): %s", plain)
	}
}

func TestRenderInlineDiff_PreservesPrefixCharacters(t *testing.T) {
	// Lipgloss strips styling in non-TTY test envs, so we only
	// check that the leading +/-/@@/space/--- markers survive
	// the render. This is the user-visible invariant: a green
	// "+x" line and a red "-y" line both still START with the
	// prefix the diff tool emitted, even if styles are dropped.
	in := "+added\n-removed\n@@ -1 +1 @@\n context\n"
	out := renderInlineDiff(in, DefaultTheme, 0)
	for _, want := range []string{"+added", "-removed", "@@ -1", " context"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing prefix marker %q in: %q", want, out)
		}
	}
}
