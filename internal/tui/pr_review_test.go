package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePRReferenceGitHubURL(t *testing.T) {
	ref, ok := parsePRReference("https://github.com/jiayaoqijia/altcode/pull/51")
	if !ok {
		t.Fatal("expected GitHub PR URL to parse")
	}
	if ref.owner != "jiayaoqijia" || ref.repo != "altcode" || ref.number != 51 {
		t.Fatalf("unexpected ref: %+v", ref)
	}
}

func TestPreparePRReviewPromptScopesGitHubPRURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	var checkoutPath string
	var calls []string
	oldRunner := runPRCheckoutCommand
	runPRCheckoutCommand = func(_ context.Context, dir, name string, args ...string) error {
		calls = append(calls, strings.TrimSpace(dir+" "+name+" "+strings.Join(args, " ")))
		if name == "gh" && len(args) >= 4 && args[0] == "repo" && args[1] == "clone" {
			checkoutPath = args[3]
			if err := os.MkdirAll(filepath.Join(checkoutPath, ".git"), 0o755); err != nil {
				t.Fatalf("create fake checkout: %v", err)
			}
		}
		return nil
	}
	defer func() { runPRCheckoutCommand = oldRunner }()

	app := New(nil, DefaultTheme, "test", "")
	prompt, info, err := app.preparePRReviewPrompt("https://github.com/jiayaoqijia/altcode/pull/51")
	if err != nil {
		t.Fatalf("preparePRReviewPrompt: %v", err)
	}
	if checkoutPath == "" {
		t.Fatal("fake gh clone did not capture checkout path")
	}
	if app.projectRoot != checkoutPath {
		t.Fatalf("projectRoot = %q, want checkout %q", app.projectRoot, checkoutPath)
	}
	if app.tools == nil || app.tools.projectRoot != checkoutPath {
		t.Fatalf("tool tree root was not scoped to checkout")
	}
	if !strings.Contains(prompt, "https://github.com/jiayaoqijia/altcode/pull/51") ||
		!strings.Contains(prompt, checkoutPath) {
		t.Fatalf("prompt missing PR URL or checkout root:\n%s", prompt)
	}
	if !strings.Contains(info, checkoutPath) {
		t.Fatalf("info missing checkout root: %s", info)
	}

	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"gh repo clone jiayaoqijia/altcode",
		"git fetch origin pull/51/head",
		"git checkout -B altcode/pr-51 FETCH_HEAD",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing command %q in:\n%s", want, joined)
		}
	}
}
