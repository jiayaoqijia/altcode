package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBatchLines_SkipsEmptyAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompts.txt")
	content := `# comment at top
first line

# another comment
second line
   third line with indent

`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readBatchLines(path)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := []string{"first line", "second line", "third line with indent"}
	if len(lines) != len(want) {
		t.Errorf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i, w := range want {
		if i >= len(lines) {
			break
		}
		if lines[i] != w {
			t.Errorf("line[%d]=%q, want %q", i, lines[i], w)
		}
	}
}

func TestReadBatchLines_MissingFile(t *testing.T) {
	_, err := readBatchLines("/tmp/definitely-does-not-exist-xyz")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSubstituteBatchPrompt(t *testing.T) {
	cases := []struct {
		name     string
		template string
		line     string
		want     string
	}{
		{"empty template uses line", "", "fix bug", "fix bug"},
		{
			"placeholder substitution",
			"Review this change: {{input}}",
			"add auth",
			"Review this change: add auth",
		},
		{
			"no placeholder appends",
			"Please analyze",
			"foo.go",
			"Please analyze\n\nfoo.go",
		},
		{
			"multiple placeholders",
			"{{input}} and {{input}}",
			"x",
			"x and x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := substituteBatchPrompt(tc.template, tc.line)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCloneBatchParams_ClearsAliasedFields(t *testing.T) {
	orig := Params{
		Prompt:     "original",
		PromptEach: "/tmp/prompts.txt",
		preRunDirty: "M foo.go",
	}
	orig.EngineParams.SessionID = "sess-original"

	clone := cloneBatchParams(orig, "line 1")

	if clone.Prompt != "line 1" {
		t.Errorf("clone.Prompt=%q, want line 1", clone.Prompt)
	}
	if clone.PromptEach != "" {
		t.Error("clone.PromptEach should be cleared to prevent recursion")
	}
	if clone.EngineParams.SessionID != "" {
		t.Error("clone.SessionID should be cleared to avoid alias")
	}
	if clone.preRunDirty != "" {
		t.Error("clone.preRunDirty should be cleared")
	}
	if !clone.Quiet {
		t.Error("clone should suppress banner for batch runs")
	}
	if orig.Prompt != "original" {
		t.Error("original should not be mutated")
	}
}

func TestValidate_ParallelNegative(t *testing.T) {
	p := &Params{Parallel: -1}
	if err := p.Validate(); err == nil {
		t.Error("expected error on negative --parallel")
	}
}

func TestValidate_RetryNegative(t *testing.T) {
	p := &Params{Retry: -1}
	if err := p.Validate(); err == nil {
		t.Error("expected error on negative --retry")
	}
}

func TestValidate_PromptEachWithPositional(t *testing.T) {
	p := &Params{PromptEach: "/tmp/x", Prompt: "positional"}
	if err := p.Validate(); err == nil {
		t.Error("expected error on --prompt-each + positional prompt")
	}
}

func TestValidate_PromptEachAloneOK(t *testing.T) {
	p := &Params{PromptEach: "/tmp/x"}
	if err := p.Validate(); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}

// TestReadBatchLines_LargeLineBuffer verifies the batch reader
// handles prompts larger than bufio's default 64KB buffer. Without
// the explicit Buffer() call, a 100KB prompt would silently
// truncate.
func TestReadBatchLines_LargeLineBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	big := strings.Repeat("x", 100*1024) // 100 KB
	if err := os.WriteFile(path, []byte(big+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readBatchLines(path)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if len(lines[0]) != 100*1024 {
		t.Errorf("line truncated: got %d bytes, want %d", len(lines[0]), 100*1024)
	}
}
