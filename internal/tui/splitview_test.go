package tui

import (
	"strings"
	"testing"
)

// TestSidebar_AddFile_DedupesAndAccumulates verifies the per-path
// accumulator logic: adding the same path twice combines counts rather
// than producing two entries.
func TestSidebar_AddFile_DedupesAndAccumulates(t *testing.T) {
	s := newSidebar(DefaultTheme)
	s.AddFile("foo.go", 5, 2)
	s.AddFile("foo.go", 3, 1)
	s.AddFile("bar.go", 1, 0)

	if got := len(s.files); got != 2 {
		t.Fatalf("got %d files, want 2 (deduped)", got)
	}
	for _, f := range s.files {
		switch f.path {
		case "foo.go":
			if f.adds != 8 || f.dels != 3 {
				t.Errorf("foo.go = +%d -%d, want +8 -3", f.adds, f.dels)
			}
		case "bar.go":
			if f.adds != 1 || f.dels != 0 {
				t.Errorf("bar.go = +%d -%d, want +1 -0", f.adds, f.dels)
			}
		}
	}
}

// TestSidebar_AddFile_RejectsEmptyPath guards against silent blank rows
// when a tool result loses its Title.
func TestSidebar_AddFile_RejectsEmptyPath(t *testing.T) {
	s := newSidebar(DefaultTheme)
	s.AddFile("", 5, 2)
	if len(s.files) != 0 {
		t.Errorf("empty path was not rejected: %+v", s.files)
	}
}

// TestSidebar_View_NarrowReturnsEmpty covers the early return for very
// narrow terminals (w<20) — the sidebar renders nothing rather than a
// chopped-off frame.
func TestSidebar_View_NarrowReturnsEmpty(t *testing.T) {
	s := newSidebar(DefaultTheme)
	s.SetSize(15, 10)
	if got := s.View(); got != "" {
		t.Errorf("narrow view = %q, want empty", got)
	}
}

// TestSidebar_View_EmptyShowsPlaceholder covers the no-files branch.
func TestSidebar_View_EmptyShowsPlaceholder(t *testing.T) {
	s := newSidebar(DefaultTheme)
	s.SetSize(40, 10)
	out := stripANSI(s.View())
	if !strings.Contains(out, "Files") {
		t.Error("missing title")
	}
	if !strings.Contains(out, "no changes yet") {
		t.Error("missing placeholder")
	}
}

// TestSidebar_View_FilesFitWithCounts covers the happy path.
func TestSidebar_View_FilesFitWithCounts(t *testing.T) {
	s := newSidebar(DefaultTheme)
	s.SetSize(40, 12)
	s.AddFile("hello.go", 3, 1)
	s.AddFile("world.go", 0, 5)
	out := stripANSI(s.View())
	for _, want := range []string{"hello.go", "world.go", "+3", "-1", "-5"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestSidebar_View_TruncatesLongPath ensures the rune-safe path-truncate
// branch exercises (long unicode-laden path).
func TestSidebar_View_TruncatesLongPath(t *testing.T) {
	s := newSidebar(DefaultTheme)
	s.SetSize(30, 10)
	s.AddFile("项目/源代码/非常长的文件名/some_really_long_path_to_force_truncation.go", 1, 0)
	out := stripANSI(s.View())
	if !strings.Contains(out, "…") {
		t.Errorf("expected truncation marker '…' in narrow sidebar:\n%s", out)
	}
}

// TestSidebar_View_OverflowShowsCount exercises the "+N more" branch
// when file count exceeds visible rows.
func TestSidebar_View_OverflowShowsCount(t *testing.T) {
	s := newSidebar(DefaultTheme)
	s.SetSize(40, 6) // height-4 = 2 rows visible (clamped to 3)
	for i := 0; i < 8; i++ {
		s.AddFile(string(rune('a'+i))+".go", 1, 0)
	}
	out := stripANSI(s.View())
	if !strings.Contains(out, "more") {
		t.Errorf("expected overflow indicator:\n%s", out)
	}
}
