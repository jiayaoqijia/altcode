package engine

import (
	"testing"
)

// Phase 8 unit tests for CostBudget. Parent-to-subagent propagation
// is tested indirectly through agent/spawn but the primitive itself
// gets its own tests here.

func TestCostBudget_NilSafe(t *testing.T) {
	var b *CostBudget
	if b.Exceeded() {
		t.Error("nil budget should never be exceeded")
	}
	if b.Used() != 0 {
		t.Error("nil budget Used() should be 0")
	}
	if b.Limit() != 0 {
		t.Error("nil budget Limit() should be 0")
	}
	if !b.Consume(0.10) {
		t.Error("nil budget Consume should return true (unlimited)")
	}
}

func TestCostBudget_UnlimitedWhenZero(t *testing.T) {
	b := NewCostBudget(0)
	if b.Exceeded() {
		t.Error("zero-limit budget should be unlimited")
	}
	if !b.Consume(1000.0) {
		t.Error("zero-limit Consume should always return true")
	}
}

func TestCostBudget_BasicAccumulation(t *testing.T) {
	b := NewCostBudget(1.0) // $1.00 cap
	if b.Limit() != 1.0 {
		t.Errorf("Limit()=%v, want 1.0", b.Limit())
	}
	if b.Exceeded() {
		t.Error("fresh budget should not be exceeded")
	}
	if !b.Consume(0.30) {
		t.Error("Consume($0.30) should stay within $1.00")
	}
	if b.Used() < 0.29 || b.Used() > 0.31 {
		t.Errorf("Used()=%v, want ~0.30", b.Used())
	}
	if !b.Consume(0.60) {
		t.Error("Consume($0.60) should stay within $1.00")
	}
	if b.Exceeded() {
		t.Error("$0.90 used of $1.00 should not be exceeded")
	}
	if b.Consume(0.15) {
		t.Error("Consume($0.15) should push over the limit and return false")
	}
	if !b.Exceeded() {
		t.Error("budget should now be exceeded")
	}
}

func TestCostBudget_MicrocentPrecision(t *testing.T) {
	// Verify no floating-point drift after 100 small turns.
	b := NewCostBudget(1.00)
	for i := 0; i < 100; i++ {
		b.Consume(0.009) // $0.009 × 100 = $0.90
	}
	used := b.Used()
	if used < 0.899 || used > 0.901 {
		t.Errorf("100x$0.009 drift: Used()=%v, want ~0.90", used)
	}
	if b.Exceeded() {
		t.Error("$0.90 used of $1.00 should not be exceeded")
	}
}

func TestCostBudget_NegativeAndZeroConsumeNoop(t *testing.T) {
	b := NewCostBudget(0.10)
	b.Consume(0.05)
	before := b.Used()
	b.Consume(-1.0) // nonsense, must be ignored
	b.Consume(0)
	if b.Used() != before {
		t.Errorf("Used() changed on negative/zero Consume: %v → %v", before, b.Used())
	}
}

// TestCostBudget_ParallelConsume verifies atomic.AddInt64 works
// correctly under contention. Codex Phase 8 review noted the lack
// of an explicit race test for this path; this fills the gap.
func TestCostBudget_ParallelConsume(t *testing.T) {
	b := NewCostBudget(1000.0) // large cap so we don't race to exceeded
	const goroutines = 16
	const perGoroutine = 100
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := 0; j < perGoroutine; j++ {
				b.Consume(0.01) // $0.01 × 1600 calls = $16
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	used := b.Used()
	want := 0.01 * goroutines * perGoroutine
	if used < want-0.01 || used > want+0.01 {
		t.Errorf("parallel Consume drift: Used()=%v, want ~%v", used, want)
	}
}
