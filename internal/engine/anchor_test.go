package engine

import (
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/provider"
)

func TestEngine_AnchorSurvivesCompact(t *testing.T) {
	e := &Engine{}
	e.SetAnchor("stack", "user is on macOS arm64")
	e.SetAnchor("repo", "jiayaoqijia/altcode")

	// Seed a chunky message history that will be compacted.
	for i := 0; i < 30; i++ {
		e.messages = append(e.messages,
			provider.TextMessage("user", "filler turn "+strings.Repeat("x", 100)),
			provider.TextMessage("assistant", "ack "+strings.Repeat("y", 100)))
	}
	_ = e.Compact()
	// (Microcompactor may or may not drop entries depending on its
	// internal budget; the only contract anchor cares about is
	// "the anchor message ends up at the head".)

	// First message must be the anchor injection.
	first := e.messages[0]
	if first.Role != "user" {
		t.Errorf("first message role = %q, want user", first.Role)
	}
	if !strings.HasPrefix(first.Content, "Persistent anchored facts") {
		t.Errorf("first message missing anchor header: %q", first.Content)
	}
	for _, want := range []string{"stack:", "macOS arm64", "repo:", "jiayaoqijia/altcode"} {
		if !strings.Contains(first.Content, want) {
			t.Errorf("anchor body missing %q in:\n%s", want, first.Content)
		}
	}
}

func TestEngine_AnchorClear(t *testing.T) {
	e := &Engine{}
	e.SetAnchor("foo", "bar")
	e.SetAnchor("baz", "qux")
	if got := len(e.Anchors()); got != 2 {
		t.Errorf("expected 2 anchors, got %d", got)
	}
	e.SetAnchor("foo", "") // clear by empty value
	got := e.Anchors()
	if _, ok := got["foo"]; ok {
		t.Errorf("foo should be cleared")
	}
	if got["baz"] != "qux" {
		t.Errorf("baz survived clear: got %q", got["baz"])
	}
}

func TestEngine_AnchorIdempotentReinjection(t *testing.T) {
	e := &Engine{}
	e.SetAnchor("k", "v")
	e.messages = []provider.Message{
		provider.TextMessage("user", "hi"),
		provider.TextMessage("assistant", "hello"),
	}

	// Compact + reinject twice. Should still be exactly one anchor message.
	e.Compact()
	e.Compact()

	count := 0
	for _, m := range e.messages {
		if isAnchorMessage(m) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 anchor message after double-compact, got %d", count)
	}
}

func TestEngine_AnchorEmptyMapNoInjection(t *testing.T) {
	e := &Engine{}
	e.messages = []provider.Message{
		provider.TextMessage("user", "hi"),
	}
	e.Compact()
	for _, m := range e.messages {
		if isAnchorMessage(m) {
			t.Error("anchor injected with no anchors set")
		}
	}
}
