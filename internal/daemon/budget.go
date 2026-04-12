package daemon

import (
	"fmt"
	"sync"
)

// BudgetConfig holds all configurable limits for the fix daemon.
type BudgetConfig struct {
	MaxFixAttempts    int     // per step, default 3
	MaxStrategyResets int     // total, default 2
	MaxTotalTurns     int     // across all phases, default 50
	MaxReviewLoops    int     // default 3
	MaxCostUSD        float64 // 0 = unlimited, default 50
	StallThreshold    int     // consecutive stalled turns, default 3
}

// DefaultBudgetConfig returns production defaults.
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		MaxFixAttempts:    3,
		MaxStrategyResets: 2,
		MaxTotalTurns:     50,
		MaxReviewLoops:    3,
		MaxCostUSD:        50.0,
		StallThreshold:    3,
	}
}

// ProgressSnapshot captures measurable progress at a point in time.
type ProgressSnapshot struct {
	FilesChanged   int
	TestsPassing   int
	Commits        int
	DiffChurnBytes int
	ErrorMessage   string
}

// BudgetController tracks usage and enforces limits.
type BudgetController struct {
	mu              sync.Mutex
	cfg             BudgetConfig
	turns           int
	costUSD         float64
	resets          int
	progressHistory []ProgressSnapshot
}

// NewBudgetController creates a controller with the given config.
func NewBudgetController(cfg BudgetConfig) *BudgetController {
	return &BudgetController{cfg: cfg}
}

// RecordTurn increments the turn counter and checks budget.
func (b *BudgetController) RecordTurn(cost float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.turns++
	b.costUSD += cost
	if b.turns > b.cfg.MaxTotalTurns {
		return fmt.Errorf(
			"budget: max turns (%d) exceeded",
			b.cfg.MaxTotalTurns,
		)
	}
	if b.cfg.MaxCostUSD > 0 && b.costUSD > b.cfg.MaxCostUSD {
		return fmt.Errorf(
			"budget: cost ceiling $%.2f exceeded ($%.2f used)",
			b.cfg.MaxCostUSD, b.costUSD,
		)
	}
	return nil
}

// RecordProgress captures a snapshot and returns whether progress
// has stalled according to multi-signal detection.
func (b *BudgetController) RecordProgress(snap ProgressSnapshot) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.progressHistory = append(b.progressHistory, snap)
	return b.isStalled()
}

// isStalled uses three independent signals over the last
// StallThreshold snapshots.
func (b *BudgetController) isStalled() bool {
	n := len(b.progressHistory)
	threshold := b.cfg.StallThreshold
	if n < threshold {
		return false
	}

	window := b.progressHistory[n-threshold:]

	// Signal 1: no file-change delta across the window.
	noFileProgress := true
	for i := 1; i < len(window); i++ {
		if window[i].FilesChanged != window[0].FilesChanged {
			noFileProgress = false
			break
		}
	}

	// Signal 2: high churn but no test improvement.
	last := window[len(window)-1]
	highChurn := last.DiffChurnBytes > 1000
	noTestGain := last.TestsPassing <= window[0].TestsPassing

	// Signal 3: identical non-empty error repeated every turn.
	sameError := true
	if window[0].ErrorMessage == "" {
		sameError = false
	} else {
		for i := 1; i < len(window); i++ {
			if window[i].ErrorMessage != window[0].ErrorMessage {
				sameError = false
				break
			}
		}
	}

	return noFileProgress || (highChurn && noTestGain) || sameError
}

// CanReset reports whether a strategy reset is still within budget.
func (b *BudgetController) CanReset() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.resets < b.cfg.MaxStrategyResets
}

// RecordReset increments the strategy-reset counter.
func (b *BudgetController) RecordReset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resets++
}

// Summary returns a human-readable budget status line.
func (b *BudgetController) Summary() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return fmt.Sprintf(
		"turns: %d/%d, cost: $%.2f/$%.2f, resets: %d/%d",
		b.turns, b.cfg.MaxTotalTurns,
		b.costUSD, b.cfg.MaxCostUSD,
		b.resets, b.cfg.MaxStrategyResets,
	)
}
