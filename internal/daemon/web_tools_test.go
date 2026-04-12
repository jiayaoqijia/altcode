package daemon

import (
	"context"
	"strings"
	"testing"
)

func mockSearch(
	_ context.Context, query string,
) ([]SearchResult, error) {
	return []SearchResult{
		{
			Title:   "Result for " + query,
			URL:     "https://example.com/" + query,
			Snippet: "snippet",
		},
	}, nil
}

func TestWebSearch_WithinLimit(t *testing.T) {
	c := NewWebSearchClient("tavily", 3, mockSearch)

	results, err := c.Search(context.Background(), "golang")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Result for golang" {
		t.Errorf("title = %q", results[0].Title)
	}
	if c.Used() != 1 {
		t.Errorf("used = %d, want 1", c.Used())
	}
}

func TestWebSearch_ExceedsLimit(t *testing.T) {
	c := NewWebSearchClient("tavily", 2, mockSearch)

	// Use up the limit.
	for i := 0; i < 2; i++ {
		if _, err := c.Search(
			context.Background(), "q"); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}

	// Third call should fail.
	_, err := c.Search(context.Background(), "q")
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %q, want rate limit", err)
	}
}

func TestWebSearch_ResetUsage(t *testing.T) {
	c := NewWebSearchClient("tavily", 1, mockSearch)

	_, err := c.Search(context.Background(), "q")
	if err != nil {
		t.Fatalf("first search: %v", err)
	}

	// Should be at limit.
	_, err = c.Search(context.Background(), "q")
	if err == nil {
		t.Fatal("expected rate limit after 1 search")
	}

	// Reset and try again.
	c.ResetUsage()
	if c.Used() != 0 {
		t.Errorf("used after reset = %d, want 0", c.Used())
	}

	_, err = c.Search(context.Background(), "q")
	if err != nil {
		t.Fatalf("search after reset: %v", err)
	}
}
