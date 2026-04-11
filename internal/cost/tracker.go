// Package cost tracks token usage and estimates USD cost per turn.
package cost

import (
	"fmt"
	"sort"
	"sync"
)

// ModelPricing defines per-million-token prices for a model.
type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// TurnCost records usage and cost for a single provider turn.
type TurnCost struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Model        string
}

// Tracker accumulates token usage and cost across turns.
type Tracker struct {
	mu      sync.Mutex
	pricing map[string]ModelPricing
	turns   []TurnCost
}

// defaultPricing returns built-in pricing for known models.
func defaultPricing() map[string]ModelPricing {
	return map[string]ModelPricing{
		"claude-haiku":  {InputPerMillion: 0.25, OutputPerMillion: 1.25},
		"claude-sonnet": {InputPerMillion: 3.0, OutputPerMillion: 15.0},
		"gpt-4":         {InputPerMillion: 2.50, OutputPerMillion: 10.0},
		"gpt-5.4":       {InputPerMillion: 2.0, OutputPerMillion: 8.0},
		"deepseek":      {InputPerMillion: 0.07, OutputPerMillion: 0.80},
		"minimax":       {InputPerMillion: 0.10, OutputPerMillion: 0.30},
		"moonshot":      {InputPerMillion: 0.12, OutputPerMillion: 0.40},
		"kimi":          {InputPerMillion: 0.12, OutputPerMillion: 0.40},
		"zhipu":         {InputPerMillion: 0.05, OutputPerMillion: 0.25},
		"glm":           {InputPerMillion: 0.05, OutputPerMillion: 0.25},
		"qwen":          {InputPerMillion: 0.0, OutputPerMillion: 0.0},
	}
}

// fallbackPricing is used for unknown models.
var fallbackPricing = ModelPricing{
	InputPerMillion:  1.0,
	OutputPerMillion: 3.0,
}

// NewTracker creates a Tracker pre-loaded with default pricing.
func NewTracker() *Tracker {
	return &Tracker{
		pricing: defaultPricing(),
	}
}

// SetPricing overrides pricing for a specific model.
func (t *Tracker) SetPricing(model string, p ModelPricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pricing[model] = p
}

// RecordTurn records a completed provider turn.
func (t *Tracker) RecordTurn(model string, inputTokens, outputTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Use the *Locked variant — we already hold t.mu and the public
	// lookupPricing would try to retake the same mutex and deadlock.
	p := t.lookupPricingLocked(model)
	cost := computeCost(p, inputTokens, outputTokens)

	t.turns = append(t.turns, TurnCost{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CostUSD:      cost,
		Model:        model,
	})
}

// TotalCost returns the sum of USD cost across all turns.
func (t *Tracker) TotalCost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	var total float64
	for _, tc := range t.turns {
		total += tc.CostUSD
	}
	return total
}

// TotalTokens returns aggregate input and output tokens.
func (t *Tracker) TotalTokens() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var in, out int
	for _, tc := range t.turns {
		in += tc.InputTokens
		out += tc.OutputTokens
	}
	return in, out
}

// Turns returns a copy of all recorded turns.
func (t *Tracker) Turns() []TurnCost {
	t.mu.Lock()
	defer t.mu.Unlock()

	cp := make([]TurnCost, len(t.turns))
	copy(cp, t.turns)
	return cp
}

// Summary returns a human-readable cost summary.
func (t *Tracker) Summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	in, out, cost := t.totalsLocked()
	return fmt.Sprintf(
		"%d turns, %s in / %s out, $%.4f",
		len(t.turns), formatInt(in), formatInt(out), cost,
	)
}

func (t *Tracker) totalsLocked() (int, int, float64) {
	var in, out int
	var cost float64
	for _, tc := range t.turns {
		in += tc.InputTokens
		out += tc.OutputTokens
		cost += tc.CostUSD
	}
	return in, out, cost
}

// lookupPricingLocked finds pricing for a model, using prefix matching
// before falling back to the default. CALLER MUST HOLD t.mu — this
// function reads t.pricing without taking the lock so it can be
// called from within the existing critical section in RecordTurn
// without re-locking. Use lookupPricing if you don't already hold mu.
func (t *Tracker) lookupPricingLocked(model string) ModelPricing {
	if p, ok := t.pricing[model]; ok {
		return p
	}
	// Try prefix match (e.g. "claude-sonnet-4-..." -> "claude-sonnet").
	// Map iteration is randomized in Go, so the previous version
	// could return DIFFERENT pricing across runs for ambiguous model
	// names like "claude-3" that prefix-match both "claude" and
	// "claude-3-opus" entries. Sort keys descending by length so the
	// LONGEST (most specific) prefix wins deterministically.
	keys := make([]string, 0, len(t.pricing))
	for k := range t.pricing {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	for _, key := range keys {
		if len(model) > len(key) && model[:len(key)] == key {
			return t.pricing[key]
		}
	}
	return fallbackPricing
}

// lookupPricing is the lock-safe wrapper for callers outside the
// critical section.
func (t *Tracker) lookupPricing(model string) ModelPricing {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lookupPricingLocked(model)
}

func computeCost(p ModelPricing, inTok, outTok int) float64 {
	inCost := float64(inTok) * p.InputPerMillion / 1_000_000
	outCost := float64(outTok) * p.OutputPerMillion / 1_000_000
	return inCost + outCost
}

// formatInt adds comma separators to an integer.
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
