package tui

import (
	"strings"
	"testing"
)

func TestOSC8Hyperlink_EmptyURIPassesThrough(t *testing.T) {
	if got := osc8Hyperlink("", "hello"); got != "hello" {
		t.Errorf("got %q, want unchanged 'hello'", got)
	}
}

func TestOSC8Hyperlink_WrapsCorrectly(t *testing.T) {
	got := osc8Hyperlink("file:///tmp/foo.go", "foo.go")
	want := "\x1b]8;;file:///tmp/foo.go\x07foo.go\x1b]8;;\x07"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestLinkifyFileRefs_GrepStylePathLine wraps the canonical
// grep output: "path/to/file.go:123:match content"
func TestLinkifyFileRefs_GrepStylePathLine(t *testing.T) {
	in := "internal/tui/app.go:42: case event.PermissionRequest:"
	out := LinkifyFileRefs(in, "/home/coder/github/altcode")

	if !strings.Contains(out, "\x1b]8;;file://") {
		t.Errorf("expected OSC-8 prefix in:\n%q", out)
	}
	if !strings.Contains(out, "internal/tui/app.go") {
		t.Errorf("expected path text preserved:\n%q", out)
	}
	if !strings.Contains(out, "#L42") {
		t.Errorf("expected #L42 anchor:\n%q", out)
	}
	if !strings.Contains(out, ":42:") {
		t.Errorf("expected :42 in display text:\n%q", out)
	}
}

// TestLinkifyFileRefs_NoFalsePositiveOnURL — https://example.com/foo
// must NOT get wrapped (it's not a file path).
func TestLinkifyFileRefs_NoFalsePositiveOnURL(t *testing.T) {
	in := "see https://example.com/api for details"
	out := LinkifyFileRefs(in, "/home/coder/github/altcode")
	if strings.Contains(out, "\x1b]8") {
		t.Errorf("URL incorrectly linkified:\n%q", out)
	}
}

// TestLinkifyFileRefs_PreservesBoundaryChars — path inside parens
// or quotes keeps the surrounding chars.
func TestLinkifyFileRefs_PreservesBoundaryChars(t *testing.T) {
	cases := []string{
		"see (foo.go:10) for the bug",
		`error in "internal/tui/app.go:42:5"`,
		"file: app.go:1:1.",
	}
	for _, in := range cases {
		out := LinkifyFileRefs(in, "/home/coder/github/altcode")
		if !strings.Contains(out, "\x1b]8") {
			t.Errorf("no link emitted for: %q\noutput: %q", in, out)
		}
	}
}

// TestLinkifyFileRefs_PathOnlyNoLine wraps a bare path without :line.
func TestLinkifyFileRefs_PathOnlyNoLine(t *testing.T) {
	in := "wrote internal/tui/messages.go"
	out := LinkifyFileRefs(in, "/home/coder/github/altcode")
	if !strings.Contains(out, "\x1b]8") {
		t.Errorf("bare path not linkified:\n%q", out)
	}
	if strings.Contains(out, "#L") {
		t.Errorf("path-only must not include #L anchor:\n%q", out)
	}
}

// TestLinkifyFileRefs_AbsolutePathLeftAsIs verifies absolute paths
// (with a recognisable extension — required after the false-positive
// fix that excludes extension-less basenames) are not re-prefixed
// with projectRoot.
func TestLinkifyFileRefs_AbsolutePathLeftAsIs(t *testing.T) {
	in := "open /etc/nginx/nginx.conf"
	out := LinkifyFileRefs(in, "/home/coder/github/altcode")
	// nginx.conf doesn't match (we don't list .conf), but
	// /tmp/foo.json does. Use that.
	in = "wrote /tmp/foo.json successfully"
	out = LinkifyFileRefs(in, "/home/coder/github/altcode")
	if !strings.Contains(out, "file:///tmp/foo.json") {
		t.Errorf("absolute path not preserved:\n%q", out)
	}
	if strings.Contains(out, "altcode/tmp/foo.json") {
		t.Errorf("absolute path was incorrectly prefixed:\n%q", out)
	}
}

// TestLinkifyFileRefs_NoExtensionNoMatch — paths without a known
// extension don't get linkified. This is the cost of the false-positive
// fix: `github.com/x/y` no longer matches, but neither does `/etc/hosts`.
// Tool-output users rarely need linkification on extension-less paths.
func TestLinkifyFileRefs_NoExtensionNoMatch(t *testing.T) {
	in := "open /etc/hosts"
	out := LinkifyFileRefs(in, "/home/coder/github/altcode")
	if strings.Contains(out, "\x1b]8") {
		t.Errorf("extension-less path should NOT linkify:\n%q", out)
	}
}

// TestLinkifyFileRefs_NoMatchReturnsUnchanged — pure prose with
// no file refs comes back byte-identical.
func TestLinkifyFileRefs_NoMatchReturnsUnchanged(t *testing.T) {
	in := "the quick brown fox jumps over the lazy dog"
	out := LinkifyFileRefs(in, "/home/coder/github/altcode")
	if out != in {
		t.Errorf("got  %q\nwant %q (unchanged)", out, in)
	}
}

// TestLinkifyFileRefs_EmptyInput returns empty.
func TestLinkifyFileRefs_EmptyInput(t *testing.T) {
	if got := LinkifyFileRefs("", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
