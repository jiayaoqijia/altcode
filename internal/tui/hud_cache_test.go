package tui

import (
	"strings"
	"testing"
)

// TestHUD_CacheChip_HiddenWhenZero ensures cold-cache turns don't
// add a "cache 0%" chip — the HUD stays quiet until prefix caching
// actually kicks in.
func TestHUD_CacheChip_HiddenWhenZero(t *testing.T) {
	h := hudState{
		ContextTokens: 30000,
		ContextLimit:  64000,
		CachedTokens:  0,
	}
	out := stripANSI(renderHUD(h, statusBarInfo{}, DefaultTheme, 200, false, ""))
	if strings.Contains(out, "cache") {
		t.Errorf("cache chip rendered with zero cached tokens:\n%q", out)
	}
}

// TestHUD_CacheChip_VisibleWhenWarm covers the happy path: cached
// > 0 produces a "cache N%" chip whose percentage is cached/context.
func TestHUD_CacheChip_VisibleWhenWarm(t *testing.T) {
	h := hudState{
		ContextTokens: 10000,
		ContextLimit:  64000,
		CachedTokens:  9000, // 90% of context served from cache
	}
	out := stripANSI(renderHUD(h, statusBarInfo{}, DefaultTheme, 200, false, ""))
	if !strings.Contains(out, "cache 90%") {
		t.Errorf("expected 'cache 90%%' chip in:\n%q", out)
	}
}

// TestHUD_CacheChip_ClampsAt100 — if the provider reports more cached
// than total prompt tokens (rare but possible with async-counted
// usage chunks), the chip must clamp at 100% rather than overflow.
func TestHUD_CacheChip_ClampsAt100(t *testing.T) {
	h := hudState{
		ContextTokens: 100,
		ContextLimit:  64000,
		CachedTokens:  500, // anomalous, > context
	}
	out := stripANSI(renderHUD(h, statusBarInfo{}, DefaultTheme, 200, false, ""))
	if !strings.Contains(out, "cache 100%") {
		t.Errorf("expected clamped 'cache 100%%' chip in:\n%q", out)
	}
}

// TestHUD_CacheChip_HiddenWhenContextZero — degenerate case: if the
// HUD doesn't know context tokens yet (first turn before usage event)
// don't render a chip with a divide-by-zero percentage.
func TestHUD_CacheChip_HiddenWhenContextZero(t *testing.T) {
	h := hudState{
		ContextTokens: 0,
		ContextLimit:  64000,
		CachedTokens:  500,
	}
	out := stripANSI(renderHUD(h, statusBarInfo{}, DefaultTheme, 200, false, ""))
	if strings.Contains(out, "cache") {
		t.Errorf("chip rendered with zero context tokens:\n%q", out)
	}
}
