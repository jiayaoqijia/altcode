package diff

import (
	"strings"
	"testing"
)

const sampleDiff = `--- a/main.go
+++ b/main.go
@@ -1,5 +1,6 @@
 package main

-import "fmt"
+import (
+	"fmt"
+)

 func main() {
@@ -10,3 +11,4 @@ func main() {
 	fmt.Println("hello")
-	fmt.Println("world")
+	fmt.Println("world!")
+	fmt.Println("done")
`

func TestParseSingleFile(t *testing.T) {
	diffs, err := Parse(sampleDiff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 FileDiff, got %d", len(diffs))
	}

	fd := diffs[0]
	if fd.OldPath != "main.go" {
		t.Errorf("OldPath = %q, want %q", fd.OldPath, "main.go")
	}
	if fd.NewPath != "main.go" {
		t.Errorf("NewPath = %q, want %q", fd.NewPath, "main.go")
	}
	if len(fd.Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(fd.Hunks))
	}
	if fd.Adds == 0 {
		t.Error("expected Adds > 0")
	}
	if fd.Deletes == 0 {
		t.Error("expected Deletes > 0")
	}
}

func TestParseHunkLineNumbers(t *testing.T) {
	diffs, _ := Parse(sampleDiff)
	h := diffs[0].Hunks[0]

	if h.OldStart != 1 || h.OldCount != 5 {
		t.Errorf("hunk0 old: start=%d count=%d", h.OldStart, h.OldCount)
	}
	if h.NewStart != 1 || h.NewCount != 6 {
		t.Errorf("hunk0 new: start=%d count=%d", h.NewStart, h.NewCount)
	}
}

func TestParseLineOps(t *testing.T) {
	diffs, _ := Parse(sampleDiff)
	h := diffs[0].Hunks[0]

	var adds, dels, ctx int
	for _, ln := range h.Lines {
		switch ln.Op {
		case '+':
			adds++
		case '-':
			dels++
		case ' ':
			ctx++
		}
	}
	if adds == 0 || dels == 0 || ctx == 0 {
		t.Errorf("adds=%d dels=%d ctx=%d", adds, dels, ctx)
	}
}

func TestParseEmptyInput(t *testing.T) {
	diffs, err := Parse("")
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs, got %d", len(diffs))
	}
}

func TestParseMultipleFiles(t *testing.T) {
	multi := `--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package a
-var x = 1
+var x = 2
--- a/b.go
+++ b/b.go
@@ -1,3 +1,3 @@
 package b
-var y = 1
+var y = 2
`
	diffs, err := Parse(multi)
	if err != nil {
		t.Fatalf("Parse multi: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 FileDiffs, got %d", len(diffs))
	}
	if diffs[0].OldPath != "a.go" {
		t.Errorf("first file = %q", diffs[0].OldPath)
	}
	if diffs[1].OldPath != "b.go" {
		t.Errorf("second file = %q", diffs[1].OldPath)
	}
}

func TestRenderContainsMarkers(t *testing.T) {
	diffs, _ := Parse(sampleDiff)
	out := Render(diffs[0], 80)

	if !strings.Contains(out, "---") {
		t.Error("Render missing --- header")
	}
	if !strings.Contains(out, "+++") {
		t.Error("Render missing +++ header")
	}
	if !strings.Contains(out, "@@") {
		t.Error("Render missing @@ hunk header")
	}
}

func TestRenderSideBySideWidth(t *testing.T) {
	diffs, _ := Parse(sampleDiff)
	out := RenderSideBySide(diffs[0], 80)

	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}
	// Should contain the separator bar.
	if !strings.Contains(out, "│") {
		t.Error("side-by-side missing │ separator")
	}
}

func TestRenderSideBySideMinWidth(t *testing.T) {
	diffs, _ := Parse(sampleDiff)
	// Width below minimum should be clamped.
	out := RenderSideBySide(diffs[0], 10)
	if out == "" {
		t.Error("side-by-side with small width should still produce output")
	}
}

func TestParseCountDefaults(t *testing.T) {
	// When count is missing (e.g., @@ -1 +1 @@), it defaults to 1.
	diff := `--- a/x.go
+++ b/x.go
@@ -1 +1 @@
-old
+new
`
	diffs, err := Parse(diff)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	h := diffs[0].Hunks[0]
	if h.OldCount != 1 || h.NewCount != 1 {
		t.Errorf("expected counts 1,1 got %d,%d", h.OldCount, h.NewCount)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		maxW int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"ab", 2, "ab"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 5, ""},
	}
	for _, tc := range cases {
		got := truncate(tc.in, tc.maxW)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q",
				tc.in, tc.maxW, got, tc.want)
		}
	}
}

func TestVisibleLen(t *testing.T) {
	plain := "hello"
	ansi := "\x1b[31mhello\x1b[0m"
	if visibleLen(plain) != 5 {
		t.Errorf("visibleLen(%q) = %d", plain, visibleLen(plain))
	}
	if visibleLen(ansi) != 5 {
		t.Errorf("visibleLen(%q) = %d", ansi, visibleLen(ansi))
	}
}

func TestPairLinesPairing(t *testing.T) {
	lines := []Line{
		{Op: '-', Content: "old"},
		{Op: '+', Content: "new"},
		{Op: ' ', Content: "ctx"},
		{Op: '+', Content: "added"},
	}
	pairs := pairLines(lines)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(pairs))
	}
	// First pair: removed + added.
	if pairs[0].left == nil || pairs[0].right == nil {
		t.Error("first pair should have both sides")
	}
	// Second pair: context.
	if pairs[1].left == nil || pairs[1].right == nil {
		t.Error("context pair should have both sides")
	}
	// Third pair: lone addition.
	if pairs[2].left != nil || pairs[2].right == nil {
		t.Error("third pair should be right-only")
	}
}
