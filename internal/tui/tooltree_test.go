package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestToolTree_StartAddsRunningEntry covers the basic Start path.
func TestToolTree_StartAddsRunningEntry(t *testing.T) {
	tt := newToolTree()
	tt.Start("t1", "read", "main.go")

	if len(tt.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(tt.entries))
	}
	if tt.entries[0].name != "read" || tt.entries[0].detail != "main.go" {
		t.Errorf("entry = %+v", tt.entries[0])
	}
	if tt.entries[0].status != "running" {
		t.Errorf("status = %q, want running", tt.entries[0].status)
	}
	if !tt.HasRunning() {
		t.Error("HasRunning should be true after Start")
	}
}

// TestToolTree_DoneTransitionsRunningToDone covers Done.
func TestToolTree_DoneTransitionsRunningToDone(t *testing.T) {
	tt := newToolTree()
	tt.Start("t1", "edit", "foo.go")
	tt.Done("t1", "edited foo.go", 50*time.Millisecond)

	if tt.entries[0].status != "done" {
		t.Errorf("status = %q, want done", tt.entries[0].status)
	}
	if tt.entries[0].elapsed != 50*time.Millisecond {
		t.Errorf("elapsed = %v, want 50ms", tt.entries[0].elapsed)
	}
	if tt.HasRunning() {
		t.Error("HasRunning should be false after Done")
	}
}

// TestToolTree_DoneWithErrorMarksFailed covers the error transition.
func TestToolTree_DoneWithErrorMarksFailed(t *testing.T) {
	tt := newToolTree()
	tt.Start("t1", "bash", "git push")
	tt.DoneWithError("t1", "rejected", 100*time.Millisecond)

	if tt.entries[0].status != "error" {
		t.Errorf("status = %q, want error", tt.entries[0].status)
	}
	if tt.entries[0].detail != "rejected" {
		t.Errorf("detail = %q, want rejected", tt.entries[0].detail)
	}
	if tt.HasRunning() {
		t.Error("HasRunning should be false after DoneWithError")
	}
}

// TestToolTree_DoneWithErrorOutputAttachesMsg covers the error+output
// branch.
func TestToolTree_DoneWithErrorOutputAttachesMsg(t *testing.T) {
	tt := newToolTree()
	tt.Start("t1", "write", "/etc/passwd")
	tt.DoneWithErrorOutput("t1", "denied", time.Millisecond, "EACCES: permission denied")

	if tt.entries[0].output != "EACCES: permission denied" {
		t.Errorf("output = %q", tt.entries[0].output)
	}
	if tt.entries[0].status != "error" {
		t.Errorf("status = %q, want error", tt.entries[0].status)
	}
}

// TestToolTree_DoneWithErrorIgnoresUnknownID guards against silent
// state corruption when a Done event arrives without a matching Start.
func TestToolTree_DoneWithErrorIgnoresUnknownID(t *testing.T) {
	tt := newToolTree()
	tt.DoneWithError("never-started", "x", 0)
	if len(tt.entries) != 0 {
		t.Errorf("entries = %d, want 0", len(tt.entries))
	}
}

// TestToolTree_ClearWipesEntries
func TestToolTree_ClearWipesEntries(t *testing.T) {
	tt := newToolTree()
	tt.Start("t1", "read", "a")
	tt.Done("t1", "ok", time.Millisecond)
	tt.Start("t2", "read", "b")
	tt.Clear()

	if len(tt.entries) != 0 {
		t.Errorf("entries = %d, want 0 after Clear", len(tt.entries))
	}
	if tt.active != -1 {
		t.Errorf("active = %d, want -1", tt.active)
	}
}

// TestToolTree_SweepRunningDropsZombies covers the zombie-cleanup path
// fired at end of turn.
func TestToolTree_SweepRunningDropsZombies(t *testing.T) {
	tt := newToolTree()
	tt.Start("t1", "read", "a")
	tt.Done("t1", "ok", time.Millisecond)
	tt.Start("t2", "read", "b") // never finishes — zombie

	tt.SweepRunning()

	if len(tt.entries) != 1 {
		t.Fatalf("entries = %d, want 1 (zombie dropped)", len(tt.entries))
	}
	if tt.entries[0].status != "done" {
		t.Errorf("survivor status = %q, want done", tt.entries[0].status)
	}
}

// TestToolTree_FindRunningByIDFindsCorrectRow
func TestToolTree_FindRunningByIDFindsCorrectRow(t *testing.T) {
	tt := newToolTree()
	tt.Start("t1", "read", "a")
	tt.Start("t2", "read", "b")
	tt.Start("t3", "read", "c")

	if got := tt.findRunningByID("t2"); got != 1 {
		t.Errorf("findRunningByID(t2) = %d, want 1", got)
	}
	// findRunningByID falls back to the oldest running entry when the
	// ID is unknown (legacy behavior preserved for events that lose
	// their id mid-stream). Document the fallback rather than the
	// "-1" intuition: missing ID returns the OLDEST running, which
	// here is index 0 (t1).
	if got := tt.findRunningByID("missing"); got != 0 {
		t.Errorf("findRunningByID(missing) = %d, want 0 (fallback to oldest)", got)
	}
}

// TestCollapseEntries_SingleRunStaysAsIs verifies that a single done
// entry is preserved as-is (not wrapped into a group).
func TestCollapseEntries_SingleRunStaysAsIs(t *testing.T) {
	entries := []toolEntry{
		{name: "read", status: "done", elapsed: 10 * time.Millisecond},
	}
	got := collapseEntries(entries)
	if len(got) != 1 {
		t.Errorf("got %d items, want 1 (single done)", len(got))
	}
	if _, ok := got[0].(toolEntry); !ok {
		t.Errorf("single entry should remain toolEntry, got %T", got[0])
	}
}

// TestCollapseEntries_RunningStaysIndividual — running entries should
// not collapse (only same-name done runs do).
func TestCollapseEntries_RunningStaysIndividual(t *testing.T) {
	entries := []toolEntry{
		{name: "read", status: "running"},
		{name: "read", status: "running"},
	}
	got := collapseEntries(entries)
	if len(got) != 2 {
		t.Errorf("got %d, want 2 (running entries don't collapse)", len(got))
	}
}

// TestCollapseEntries_ConsecutiveSameNameGroups exercises the group
// branch: 3 same-name done entries collapse to 1 group with count=3.
func TestCollapseEntries_ConsecutiveSameNameGroups(t *testing.T) {
	entries := []toolEntry{
		{name: "read", status: "done", elapsed: 10 * time.Millisecond},
		{name: "read", status: "done", elapsed: 20 * time.Millisecond},
		{name: "read", status: "done", elapsed: 30 * time.Millisecond},
	}
	got := collapseEntries(entries)
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1 (3 collapsed to 1 group)", len(got))
	}
	g, ok := got[0].(collapsedGroup)
	if !ok {
		t.Fatalf("expected collapsedGroup, got %T", got[0])
	}
	if g.count != 3 || g.name != "read" {
		t.Errorf("group = %+v, want {read, count=3}", g)
	}
	if g.elapsed != 60*time.Millisecond {
		t.Errorf("elapsed sum = %v, want 60ms", g.elapsed)
	}
}

// TestCollapseEntries_DifferentNamesStayDistinct — only same-name runs
// collapse; alternating tools must remain individual.
func TestCollapseEntries_DifferentNamesStayDistinct(t *testing.T) {
	entries := []toolEntry{
		{name: "read", status: "done"},
		{name: "edit", status: "done"},
		{name: "read", status: "done"},
	}
	got := collapseEntries(entries)
	if len(got) != 3 {
		t.Errorf("got %d, want 3 (alternating names)", len(got))
	}
}

// TestToolTree_HasRunningTrueOnlyWhileSomeoneRuns
func TestToolTree_HasRunningTrueOnlyWhileSomeoneRuns(t *testing.T) {
	tt := newToolTree()
	if tt.HasRunning() {
		t.Error("empty tree should not HaveRunning")
	}
	tt.Start("t1", "read", "a")
	if !tt.HasRunning() {
		t.Error("after Start, HasRunning should be true")
	}
	tt.Done("t1", "ok", time.Millisecond)
	if tt.HasRunning() {
		t.Error("after Done, HasRunning should be false")
	}
}

func TestToolTree_RenderCompact_ShowsImportantOutputOnly(t *testing.T) {
	tt := newToolTree()
	tt.Start("read", "Read", "internal/tui/messages.go")
	tt.DoneWithOutput("read", "Read internal/tui/messages.go", time.Millisecond, "line 1\nline 2\nline 3\nline 4\nline 5")
	tt.Start("bash", "Bash", "go test ./internal/tui/...")
	tt.DoneWithOutput("bash", "go test ./internal/tui/...", time.Millisecond, "ok 1\nok 2\nok 3\nok 4\nok 5\nok 6")

	out := stripANSI(tt.RenderCompact(DefaultTheme, 100))
	for _, want := range []string{"Read", "internal/tui/messages.go", "Bash", "go test ./internal/tui/...", "ok 1", "ok 4", "+2 lines"} {
		if !strings.Contains(out, want) {
			t.Fatalf("compact trace missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line 1") {
		t.Fatalf("completed Read should stay one-line in compact trace:\n%s", out)
	}
	if strings.Contains(out, "ok 5") || strings.Contains(out, "ok 6") {
		t.Fatalf("long Bash output should not be dumped in compact trace:\n%s", out)
	}
}

func TestToolTree_RenderCompact_ErrorOutputIsCapped(t *testing.T) {
	tt := newToolTree()
	tt.Start("bash", "Bash", "go test ./...")
	tt.DoneWithErrorOutput("bash", "go test ./...", time.Second, "line 1\nline 2\nline 3\nline 4\nline 5\nline 6")

	out := stripANSI(tt.RenderCompact(DefaultTheme, 100))
	for _, want := range []string{"Bash", "line 1", "line 4", "+2 lines"} {
		if !strings.Contains(out, want) {
			t.Fatalf("compact error trace missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line 5") || strings.Contains(out, "line 6") {
		t.Fatalf("compact error trace should cap long output:\n%s", out)
	}
}

func TestToolTree_RenderCompact_CollapsesRoutineReads(t *testing.T) {
	tt := newToolTree()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("read-%d", i)
		tt.Start(id, "Read", fmt.Sprintf("file-%d.go", i))
		tt.Done(id, fmt.Sprintf("file-%d.go", i), time.Millisecond)
	}

	out := stripANSI(tt.RenderCompact(DefaultTheme, 100))
	if !strings.Contains(out, "Read 5 files") {
		t.Fatalf("compact trace should summarize repeated reads:\n%s", out)
	}
	if strings.Count(out, "Read") != 1 {
		t.Fatalf("compact trace rendered repeated read rows:\n%s", out)
	}
}

func TestToolTree_RenderCompact_CollapsesRoutineSearchesAndPreservesEdits(t *testing.T) {
	tt := newToolTree()
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("grep-%d", i)
		tt.Start(id, "Grep", fmt.Sprintf("pattern-%d", i))
		tt.Done(id, fmt.Sprintf("pattern-%d", i), time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("glob-%d", i)
		tt.Start(id, "Glob", fmt.Sprintf("*.%d.go", i))
		tt.Done(id, fmt.Sprintf("*.%d.go", i), time.Millisecond)
	}
	tt.Start("edit", "Edit", "internal/tui/app.go")
	tt.DoneWithOutput("edit", "internal/tui/app.go", time.Millisecond, "@@ -1 +1 @@\n-old\n+new")
	tt.Start("bash", "Bash", "go test ./internal/tui")
	tt.DoneWithOutput("bash", "go test ./internal/tui", time.Millisecond, "ok internal/tui 1.0s")

	out := stripANSI(tt.RenderCompact(DefaultTheme, 100))
	for _, want := range []string{"Searched 4 patterns", "Listed 3 patterns", "Edit", "-old", "+new", "Bash", "ok internal/tui"} {
		if !strings.Contains(out, want) {
			t.Fatalf("compact trace missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "Grep") > 0 || strings.Count(out, "Glob") > 0 {
		t.Fatalf("routine searches should be summarized, not repeated:\n%s", out)
	}
}

func TestToolTree_RenderLiveShowsRunningDetailAfterTiming(t *testing.T) {
	tt := newToolTree()
	tt.Start("bash", "bash", "GOFLAGS=-mod=mod go test ./internal/tui/... -race -count=1")
	tt.entries[0].startedAt = time.Now().Add(-3 * time.Second)

	out := stripANSI(tt.RenderLive(DefaultTheme, 100))

	if !strings.Contains(out, "bash 3s") {
		t.Fatalf("running tool should keep name and timing together:\n%s", out)
	}
	if !strings.Contains(out, "· GOFLAGS=-mod=mod go test") {
		t.Fatalf("running tool should show dim detail after timing:\n%s", out)
	}
	if strings.Contains(out, "bash(GOFLAGS") {
		t.Fatalf("running tool detail should not be folded into the bold name:\n%s", out)
	}
}
