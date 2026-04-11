// Package cost tracks token usage and estimates USD cost per turn.
package cost

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ModelPricing defines per-million-token prices for a model.
//
// CacheCreationPerMillion (Anthropic prompt-cache write, ~1.25x input
// cost) and CacheReadPerMillion (cache hit, ~0.1x input cost) are
// optional. When both are zero we fall back to InputPerMillion for
// the cache-creation tokens and zero for the cache-read tokens.
type ModelPricing struct {
	InputPerMillion          float64
	OutputPerMillion         float64
	CacheCreationPerMillion  float64
	CacheReadPerMillion      float64
}

// TurnCost records usage and cost for a single provider turn.
type TurnCost struct {
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	CostUSD             float64
	Model               string
}

// Tracker accumulates token usage and cost across turns.
type Tracker struct {
	mu      sync.Mutex
	pricing map[string]ModelPricing
	turns   []TurnCost
}

// defaultPricing returns built-in pricing for known models. Keys are
// matched against the model ID with both prefix AND substring lookup
// so real provider IDs like "claude-3-5-sonnet-20241022" or
// "gpt-4o-2024-08-06" find their pricing entry instead of falling
// back to a generic per-token rate.
//
// Cache pricing follows Anthropic's published multipliers: cache
// creation is 1.25x input price, cache read is 0.1x input price.
func defaultPricing() map[string]ModelPricing {
	anthropicCache := func(in, out float64) ModelPricing {
		return ModelPricing{
			InputPerMillion:         in,
			OutputPerMillion:        out,
			CacheCreationPerMillion: in * 1.25,
			CacheReadPerMillion:     in * 0.1,
		}
	}
	return map[string]ModelPricing{
		// Anthropic — both short and full IDs
		"claude-haiku":     anthropicCache(0.25, 1.25),
		"claude-3-haiku":   anthropicCache(0.25, 1.25),
		"claude-haiku-4":   anthropicCache(1.0, 5.0),
		"claude-sonnet":    anthropicCache(3.0, 15.0),
		"claude-3-5-sonnet": anthropicCache(3.0, 15.0),
		"claude-sonnet-4":  anthropicCache(3.0, 15.0),
		"claude-opus":      anthropicCache(15.0, 75.0),
		"claude-3-opus":    anthropicCache(15.0, 75.0),
		"claude-opus-4":    anthropicCache(15.0, 75.0),
		// OpenAI
		"gpt-4":     {InputPerMillion: 2.50, OutputPerMillion: 10.0},
		"gpt-4o":    {InputPerMillion: 2.50, OutputPerMillion: 10.0},
		"gpt-4-turbo": {InputPerMillion: 10.0, OutputPerMillion: 30.0},
		"gpt-5":     {InputPerMillion: 2.0, OutputPerMillion: 8.0},
		"gpt-5.4":   {InputPerMillion: 2.0, OutputPerMillion: 8.0},
		// Chinese providers
		"deepseek": {InputPerMillion: 0.07, OutputPerMillion: 0.80},
		"minimax":  {InputPerMillion: 0.10, OutputPerMillion: 0.30},
		"moonshot": {InputPerMillion: 0.12, OutputPerMillion: 0.40},
		"kimi":     {InputPerMillion: 0.12, OutputPerMillion: 0.40},
		"zhipu":    {InputPerMillion: 0.05, OutputPerMillion: 0.25},
		"glm":      {InputPerMillion: 0.05, OutputPerMillion: 0.25},
		"qwen":     {InputPerMillion: 0.0, OutputPerMillion: 0.0},
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

// RecordTurn records a completed provider turn. Cache tokens default
// to zero so older callers that don't yet pass them get the existing
// behavior; new callers should use RecordTurnWithCache.
func (t *Tracker) RecordTurn(model string, inputTokens, outputTokens int) {
	t.RecordTurnWithCache(model, inputTokens, outputTokens, 0, 0)
}

// RecordTurnWithCache records a completed provider turn including
// Anthropic prompt-cache token counts. Cache tokens are billed via
// the model's CacheCreationPerMillion / CacheReadPerMillion fields,
// falling back to InputPerMillion (creation) and 0 (reads) when the
// model entry doesn't have explicit cache prices.
func (t *Tracker) RecordTurnWithCache(model string, inputTokens, outputTokens, cacheCreation, cacheRead int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Use the *Locked variant — we already hold t.mu and the public
	// lookupPricing would try to retake the same mutex and deadlock.
	p := t.lookupPricingLocked(model)
	cost := computeCostWithCache(p, inputTokens, outputTokens, cacheCreation, cacheRead)

	t.turns = append(t.turns, TurnCost{
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		CacheCreationTokens: cacheCreation,
		CacheReadTokens:     cacheRead,
		CostUSD:             cost,
		Model:               model,
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

// lookupPricingLocked finds pricing for a model. Sorted longest-key
// first so the most specific match wins deterministically. Tries
// strict prefix match first, then falls back to substring contains
// so real provider IDs like "anthropic/claude-3-5-sonnet-20241022"
// match the "claude-3-5-sonnet" entry even though the strict prefix
// would fail because of the "anthropic/" namespace.
//
// CALLER MUST HOLD t.mu — this function reads t.pricing without
// taking the lock so it can be called from within the existing
// critical section in RecordTurn without re-locking.
func (t *Tracker) lookupPricingLocked(model string) ModelPricing {
	if p, ok := t.pricing[model]; ok {
		return p
	}
	keys := make([]string, 0, len(t.pricing))
	for k := range t.pricing {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	// First pass: exact prefix.
	for _, key := range keys {
		if len(model) > len(key) && model[:len(key)] == key {
			return t.pricing[key]
		}
	}
	// Second pass: substring. Catches "anthropic/claude-3-5-sonnet"
	// and "openai/gpt-4o-2024-08-06".
	for _, key := range keys {
		if strings.Contains(model, key) {
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
	return computeCostWithCache(p, inTok, outTok, 0, 0)
}

// computeCostWithCache adds the Anthropic prompt-cache cost on top
// of the base input/output cost. Cache creation falls back to
// InputPerMillion when the model entry has no explicit price; cache
// reads default to zero (no charge) when not configured.
func computeCostWithCache(p ModelPricing, inTok, outTok, cacheCreate, cacheRead int) float64 {
	inCost := float64(inTok) * p.InputPerMillion / 1_000_000
	outCost := float64(outTok) * p.OutputPerMillion / 1_000_000
	creationRate := p.CacheCreationPerMillion
	if creationRate == 0 {
		creationRate = p.InputPerMillion
	}
	cacheCreateCost := float64(cacheCreate) * creationRate / 1_000_000
	cacheReadCost := float64(cacheRead) * p.CacheReadPerMillion / 1_000_000
	return inCost + outCost + cacheCreateCost + cacheReadCost
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
