package daemon

import (
	"strings"
	"sync"
	"testing"
)

func TestBudgetController_TurnLimit(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.MaxTotalTurns = 3
	bc := NewBudgetController(cfg)

	for i := 0; i < 3; i++ {
		if err := bc.RecordTurn(0); err != nil {
			t.Fatalf("turn %d should succeed: %v", i+1, err)
		}
	}
	if err := bc.RecordTurn(0); err == nil {
		t.Fatal("expected error after exceeding max turns")
	} else if !strings.Contains(err.Error(), "max turns") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBudgetController_CostCeiling(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.MaxCostUSD = 10.0
	cfg.MaxTotalTurns = 100
	bc := NewBudgetController(cfg)

	if err := bc.RecordTurn(6.0); err != nil {
		t.Fatalf("first turn should succeed: %v", err)
	}
	if err := bc.RecordTurn(5.0); err == nil {
		t.Fatal("expected error after exceeding cost ceiling")
	} else if !strings.Contains(err.Error(), "cost ceiling") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBudgetController_UnlimitedCost(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.MaxCostUSD = 0
	cfg.MaxTotalTurns = 1000
	bc := NewBudgetController(cfg)

	for i := 0; i < 100; i++ {
		if err := bc.RecordTurn(999.0); err != nil {
			t.Fatalf("turn %d should succeed with unlimited cost: %v", i+1, err)
		}
	}
}

func TestStallDetection_NoFileChanges(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.StallThreshold = 3
	bc := NewBudgetController(cfg)

	snap := ProgressSnapshot{FilesChanged: 5, TestsPassing: 10}
	for i := 0; i < 3; i++ {
		stalled := bc.RecordProgress(snap)
		if i < 2 && stalled {
			t.Fatalf("should not be stalled at snapshot %d", i)
		}
	}
	// The third snapshot completes the window of 3 identical snaps.
	if !bc.RecordProgress(snap) {
		t.Fatal("expected stall: no file changes across threshold")
	}
}

func TestStallDetection_HighChurnNoTestGain(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.StallThreshold = 3
	bc := NewBudgetController(cfg)

	// Each snapshot has different FilesChanged (no signal 1),
	// but high churn with no test improvement.
	for i := 0; i < 3; i++ {
		bc.RecordProgress(ProgressSnapshot{
			FilesChanged:   i,
			TestsPassing:   5,
			DiffChurnBytes: 2000,
		})
	}
	if !bc.isStalled() {
		t.Fatal("expected stall: high churn with no test gain")
	}
}

func TestStallDetection_SameError(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.StallThreshold = 3
	bc := NewBudgetController(cfg)

	snap := ProgressSnapshot{
		FilesChanged: 0,
		ErrorMessage: "cannot compile: undefined var",
	}
	for i := 0; i < 2; i++ {
		bc.RecordProgress(snap)
	}
	if stalled := bc.RecordProgress(snap); !stalled {
		t.Fatal("expected stall: same error repeated 3 times")
	}
}

func TestStallDetection_NotStalledYet(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.StallThreshold = 3
	bc := NewBudgetController(cfg)

	bc.RecordProgress(ProgressSnapshot{FilesChanged: 1, TestsPassing: 5})
	bc.RecordProgress(ProgressSnapshot{FilesChanged: 1, TestsPassing: 5})

	if bc.isStalled() {
		t.Fatal("should not be stalled with fewer snapshots than threshold")
	}
}

func TestStallDetection_Progress(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.StallThreshold = 3
	bc := NewBudgetController(cfg)

	// Each snapshot shows genuine progress on files and tests.
	bc.RecordProgress(ProgressSnapshot{
		FilesChanged: 1, TestsPassing: 5, DiffChurnBytes: 100,
	})
	bc.RecordProgress(ProgressSnapshot{
		FilesChanged: 2, TestsPassing: 7, DiffChurnBytes: 200,
	})
	bc.RecordProgress(ProgressSnapshot{
		FilesChanged: 3, TestsPassing: 9, DiffChurnBytes: 300,
	})

	if bc.isStalled() {
		t.Fatal("should not be stalled when progress is being made")
	}
}

func TestBudgetController_CanReset(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.MaxStrategyResets = 2
	bc := NewBudgetController(cfg)

	if !bc.CanReset() {
		t.Fatal("should allow first reset")
	}
	bc.RecordReset()
	if !bc.CanReset() {
		t.Fatal("should allow second reset")
	}
	bc.RecordReset()
	if bc.CanReset() {
		t.Fatal("should block third reset")
	}
}

func TestBudgetController_Summary(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.MaxTotalTurns = 50
	cfg.MaxCostUSD = 50.0
	cfg.MaxStrategyResets = 2
	bc := NewBudgetController(cfg)

	bc.RecordTurn(1.25)
	bc.RecordTurn(0.75)
	bc.RecordReset()

	got := bc.Summary()
	want := "turns: 2/50, cost: $2.00/$50.00, resets: 1/2"
	if got != want {
		t.Fatalf("Summary mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestBudgetController_ConcurrentRecordTurn(t *testing.T) {
	cfg := DefaultBudgetConfig()
	cfg.MaxTotalTurns = 10000
	cfg.MaxCostUSD = 0 // unlimited
	cfg.MaxStrategyResets = 10000
	bc := NewBudgetController(cfg)

	const goroutines = 50
	const turnsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // 3 groups: RecordTurn, RecordProgress, RecordReset+CanReset+Summary

	// Group 1: concurrent RecordTurn calls.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < turnsPerGoroutine; j++ {
				bc.RecordTurn(0.01)
			}
		}()
	}

	// Group 2: concurrent RecordProgress calls.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < turnsPerGoroutine; j++ {
				bc.RecordProgress(ProgressSnapshot{
					FilesChanged: j,
					TestsPassing: j,
				})
			}
		}()
	}

	// Group 3: concurrent CanReset, RecordReset, Summary calls.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < turnsPerGoroutine; j++ {
				bc.CanReset()
				bc.RecordReset()
				bc.Summary()
			}
		}()
	}

	wg.Wait()

	// Verify final state is consistent.
	summary := bc.Summary()
	if summary == "" {
		t.Fatal("expected non-empty summary after concurrent access")
	}
}
