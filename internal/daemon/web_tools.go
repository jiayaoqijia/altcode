package daemon

import (
	"context"
	"fmt"
	"sync"
)

// SearchResult holds a single web search hit.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchFunc performs the actual HTTP call to a search provider.
// Injected at construction so tests can mock it.
type SearchFunc func(
	ctx context.Context, query string,
) ([]SearchResult, error)

// WebSearchClient provides rate-limited web search for agents.
type WebSearchClient struct {
	provider   string // "tavily", "serper", "exa"
	maxPerTask int
	search     SearchFunc
	used       int
	mu         sync.Mutex
}

// NewWebSearchClient creates a client with per-task rate limiting.
func NewWebSearchClient(
	provider string, maxPerTask int, fn SearchFunc,
) *WebSearchClient {
	if maxPerTask <= 0 {
		maxPerTask = 10
	}
	return &WebSearchClient{
		provider:   provider,
		maxPerTask: maxPerTask,
		search:     fn,
	}
}

// Search performs a web search query. Returns an error when the
// per-task limit has been reached.
func (w *WebSearchClient) Search(
	ctx context.Context, query string,
) ([]SearchResult, error) {
	w.mu.Lock()
	if w.used >= w.maxPerTask {
		w.mu.Unlock()
		return nil, fmt.Errorf(
			"search rate limit: %d/%d used",
			w.used, w.maxPerTask,
		)
	}
	w.used++
	w.mu.Unlock()

	return w.search(ctx, query)
}

// ResetUsage resets the per-task counter. Call at task start.
func (w *WebSearchClient) ResetUsage() {
	w.mu.Lock()
	w.used = 0
	w.mu.Unlock()
}

// Used returns the current usage count.
func (w *WebSearchClient) Used() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.used
}
