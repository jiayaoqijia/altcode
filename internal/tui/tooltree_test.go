package tui

import (
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
