package tui

import (
	"strings"
	"testing"
)

// TestFormatToolOutput_DiffPathHitsForLowercaseEdit is the regression
// test for the case-mismatch bug: real Tool.Name() values are lowercase
// (edit/write/bash/apply_patch) but the switch keyed on capitalized
// strings, so EVERY edit/write output bypassed diff coloring and
// landed in the generic plain-dim formatter.
func TestFormatToolOutput_DiffPathHitsForLowercaseEdit(t *testing.T) {
	output := "+ added line\n- removed line\n  context line"
	cases := []string{"edit", "write", "apply_patch", "EDIT", "Apply_Patch"}
	for _, name := range cases {
		out := formatToolOutput(name, output, DefaultTheme, 80, "")
		joined := stripANSI(strings.Join(out, "\n"))
		// Diff formatter preserves +/- chars; generic formatter trims
		// leading whitespace which strips the +/- in plain output.
		if !strings.Contains(joined, "+ added line") {
			t.Errorf("name=%q: + line missing — diff path didn't fire:\n%s",
				name, joined)
		}
		if !strings.Contains(joined, "- removed line") {
			t.Errorf("name=%q: - line missing — diff path didn't fire:\n%s",
				name, joined)
		}
	}
}

// TestFormatToolOutput_BashPathHitsForLowercaseBash — same regression
// for bash. Verifies the lowercase switch hits formatBashOutput which
// runs LinkifyFileRefs (the OSC-8 hyperlink wrapper).
func TestFormatToolOutput_BashPathHitsForLowercaseBash(t *testing.T) {
	output := "internal/tui/app.go:42: case event.PermissionRequest:"
	out := formatToolOutput("bash", output, DefaultTheme, 200, "/home/coder/github/altcode")
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "\x1b]8;;file://") {
		t.Errorf("bash path didn't OSC-8 linkify file:line ref:\n%q", joined)
	}
}

// TestFormatToolOutput_DefaultStillWorks — non-edit/non-bash tools
// (read, grep, etc.) keep going through the generic formatter.
func TestFormatToolOutput_DefaultStillWorks(t *testing.T) {
	out := formatToolOutput("read", "line one\nline two", DefaultTheme, 80, "")
	if len(out) == 0 {
		t.Fatal("read output produced empty result")
	}
	joined := stripANSI(strings.Join(out, "\n"))
	if !strings.Contains(joined, "line one") {
		t.Errorf("read output missing first line:\n%s", joined)
	}
}
