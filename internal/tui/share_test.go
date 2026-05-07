package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSessionMarkdown_EmptyHasHeaderOnly(t *testing.T) {
	a := testApp()
	a.sessionSlug = "test-slug"
	md := a.buildSessionMarkdown()
	if !strings.Contains(md, "# altcode session") {
		t.Errorf("missing header:\n%s", md)
	}
	if !strings.Contains(md, "test-slug") {
		t.Errorf("missing slug:\n%s", md)
	}
	if !strings.Contains(md, "---") {
		t.Errorf("missing separator before messages:\n%s", md)
	}
}

func TestBuildSessionMarkdown_RolesProperlyFormatted(t *testing.T) {
	a := testApp()
	a.sessionSlug = "test"
	a.messages = []chatMessage{
		{role: roleUser, content: "what is 2+2?"},
		{role: roleAssistant, content: "Four.", meta: "deepseek-v4-pro · 3s"},
		{role: roleInfo, content: "[auto-allow] bash"},
		{role: roleThinking, content: "considering arithmetic..."},
		{role: roleTool, content: "result: 4"},
	}
	md := a.buildSessionMarkdown()

	cases := []string{
		"## User",
		"what is 2+2?",
		"## Assistant (deepseek-v4-pro · 3s)",
		"Four.",
		"> [auto-allow] bash",
		"```thinking",
		"considering arithmetic...",
		"```tool",
		"result: 4",
	}
	for _, want := range cases {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

// TestBuiltinShareText_WritesFileToDefaultPath covers the happy path:
// /share with no args writes to ~/.altcode/shares/<ts>-<slug>.md and
// returns the path.
func TestBuiltinShareText_WritesFileToDefaultPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	a := testApp()
	a.sessionSlug = "share-test"
	a.messages = []chatMessage{
		{role: roleUser, content: "hi"},
		{role: roleAssistant, content: "hello!"},
	}

	got := a.builtinShareText([]string{"/share"})
	if !strings.Contains(got, "2 messages") {
		t.Errorf("expected '2 messages' in: %q", got)
	}

	// File must exist in ~/.altcode/shares/
	dir := filepath.Join(tmpHome, ".altcode", "shares")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read share dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 share file, got %d", len(entries))
	}
	if !strings.Contains(entries[0].Name(), "share-test") {
		t.Errorf("filename missing slug: %q", entries[0].Name())
	}
}

// TestBuiltinShareText_HonorsExplicitPath
func TestBuiltinShareText_HonorsExplicitPath(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "transcript.md")

	a := testApp()
	a.messages = []chatMessage{
		{role: roleUser, content: "hi"},
		{role: roleAssistant, content: "hello"},
	}

	got := a.builtinShareText([]string{"/share", out})
	if !strings.Contains(got, out) {
		t.Errorf("response missing path %q: %q", out, got)
	}

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read share file: %v", err)
	}
	if !strings.Contains(string(body), "## User") {
		t.Errorf("share file missing user header:\n%s", string(body))
	}
}

// TestBuiltinShareText_EmptyConversationReturnsHint
func TestBuiltinShareText_EmptyConversationReturnsHint(t *testing.T) {
	a := testApp()
	a.messages = nil
	got := a.builtinShareText([]string{"/share"})
	if !strings.Contains(got, "nothing to share") {
		t.Errorf("expected 'nothing to share' hint: %q", got)
	}
}
