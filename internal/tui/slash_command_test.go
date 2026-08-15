package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/command"
)

func TestExpandSlashCommandSkillPreservesArguments(t *testing.T) {
	app := New(nil, DefaultTheme, "test", "", &command.Command{
		Name: "evaluate",
		Path: filepath.Join("project", ".agents", "skills", "evaluate", "SKILL.md"),
		Body: "# Evaluator\n\nRun checks and report a verdict.",
	})

	got := app.expandSlashCommand("/evaluate original prompt text")
	if !strings.Contains(got, "Run checks and report a verdict.") {
		t.Fatalf("expanded command missing skill body:\n%s", got)
	}
	if !strings.Contains(got, "original prompt text") {
		t.Fatalf("expanded skill command dropped user arguments:\n%s", got)
	}
}
