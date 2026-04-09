package provider

import (
	"context"
	"fmt"
	"strings"
)

// FallbackProvider tries providers in order, falling back on errors.
type FallbackProvider struct {
	providers []Provider
	names     []string
	current   int
}

// NewFallback creates a provider that falls back through a chain.
func NewFallback(providers []Provider) *FallbackProvider {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return &FallbackProvider{
		providers: providers,
		names:     names,
	}
}

func (f *FallbackProvider) Name() string {
	if f.current < len(f.names) {
		return f.names[f.current]
	}
	return "fallback"
}

func (f *FallbackProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	for i := f.current; i < len(f.providers); i++ {
		ch, err := f.providers[i].Stream(ctx, req)
		if err != nil {
			// Check if rate-limited or auth error — try next provider
			errStr := err.Error()
			if isRetryableError(errStr) {
				f.current = i + 1
				continue
			}
			return nil, err
		}
		f.current = i
		return ch, nil
	}
	return nil, fmt.Errorf("all %d providers failed", len(f.providers))
}

func isRetryableError(err string) bool {
	return strings.Contains(err, "429") ||
		strings.Contains(err, "rate") ||
		strings.Contains(err, "limit") ||
		strings.Contains(err, "503") ||
		strings.Contains(err, "overloaded")
}
