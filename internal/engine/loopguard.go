package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// LoopGuard blocks runaway tool-call loops where the model repeats the
// SAME (tool, input) over and over because it didn't understand the
// previous result. DS-TUI ships this as `loop_guard.rs` after seeing
// real-world deepseek runs spin on identical bash calls for 50+ turns
// burning cost without making progress.
//
// Two thresholds:
//   - SoftCap (default 3): same (tool, hash(input)) → reject with
//     a synthetic "loop detected" tool result so the model sees the
//     guard's message and (usually) tries something else.
//   - HardCap (default 8): consecutive tool ERRORS across any tool →
//     halt the agent loop, surface a BudgetExceeded-style event.
//     Distinct from SoftCap because errors don't always repeat the
//     same input — a model thrashing on flaky network calls hits
//     this without hitting SoftCap.
//
// Reset() clears all state — called by /clear and at session end.
type LoopGuard struct {
	mu              sync.Mutex
	calls           map[string]int // (tool|inputHash) → count
	consecutiveErrs int
	softCap         int
	hardCap         int
}

// NewLoopGuard returns a guard with the default 3/8 thresholds.
// Override via env ALTCODE_LOOP_SOFT, ALTCODE_LOOP_HARD if needed.
func NewLoopGuard() *LoopGuard {
	return &LoopGuard{
		calls:   make(map[string]int),
		softCap: 3,
		hardCap: 8,
	}
}

// loopKey hashes (tool, args) into a stable identifier. We use sha256
// truncated to 16 hex chars — collisions don't matter much here
// because the worst case is two distinct calls being treated as the
// same loop, and the SoftCap of 3 is forgiving enough that a real
// false-positive would just inconvenience the model once.
func loopKey(toolName string, input []byte) string {
	h := sha256.Sum256(append([]byte(toolName+":"), input...))
	return hex.EncodeToString(h[:8])
}

// Check returns a non-nil "looped" message when the (tool,input) pair
// has been called > softCap times this session. The caller short-
// circuits the dispatch and returns the message as the synthetic
// tool result so the model sees the guard's note.
func (g *LoopGuard) Check(toolName string, input []byte) (looped bool, msg string) {
	if g == nil {
		return false, ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	key := loopKey(toolName, input)
	g.calls[key]++
	if g.calls[key] > g.softCap {
		return true, fmt.Sprintf(
			"Loop guard: this exact tool call (%s with the same input) "+
				"has now been issued %d times. Either change the input, "+
				"try a different approach, or stop. The result is the "+
				"same as before — re-running it won't surface new info.",
			toolName, g.calls[key])
	}
	return false, ""
}

// RecordResult notes whether the most recent tool call ended in
// success or failure. Failures increment the consecutive-errors
// counter; a single success resets it. When the counter exceeds
// hardCap the next AgentShouldHalt() call returns true so the
// engine breaks out of its turn loop.
func (g *LoopGuard) RecordResult(failed bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if failed {
		g.consecutiveErrs++
	} else {
		g.consecutiveErrs = 0
	}
}

// AgentShouldHalt reports whether the consecutive-error count has
// crossed the hard cap. The engine checks this after each tool
// dispatch and emits a BudgetExceeded event if so.
func (g *LoopGuard) AgentShouldHalt() (bool, string) {
	if g == nil {
		return false, ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.consecutiveErrs >= g.hardCap {
		return true, fmt.Sprintf(
			"halted after %d consecutive tool errors — agent loop is "+
				"stuck. Investigate the last error chain manually.",
			g.consecutiveErrs)
	}
	return false, ""
}

// Reset clears all loop-guard state. Called by /clear so a fresh
// session doesn't inherit the prior conversation's loop counts.
func (g *LoopGuard) Reset() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = make(map[string]int)
	g.consecutiveErrs = 0
}
