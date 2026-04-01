package tui_test

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/tui"
)

func TestRenderPlainText(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	result := r.Render("Hello, world!")
	if result == "" {
		t.Fatal("Expected non-empty output")
	}
}

func TestRenderCodeBlock(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	input := "Here is code:\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nDone."
	result := r.Render(input)
	if result == "" {
		t.Fatal("Expected non-empty output")
	}
	t.Logf("Rendered:\n%s", result)
}

func TestRenderIncompleteCodeBlock(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	input := "Here is code:\n\n```go\nfunc main() {"
	result := r.Render(input)
	if result == "" {
		t.Fatal("Expected non-empty output")
	}
	t.Logf("Rendered (incomplete):\n%s", result)
}
