package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// --- WebFetch ---

type webFetchTool struct{}

// NewWebFetchTool creates a tool that fetches a URL and returns text.
func NewWebFetchTool() Tool { return &webFetchTool{} }

func (t *webFetchTool) Name() string { return "web_fetch" }
func (t *webFetchTool) Description() string {
	return "Fetch a URL and return its text content. Use for reading documentation, APIs, or web pages."
}
func (t *webFetchTool) IsConcurrencySafe() bool { return true }
func (t *webFetchTool) IsReadOnly() bool        { return true }
func (t *webFetchTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ URL string `json:"url"` }
	json.Unmarshal(input, &p)
	return "web_fetch:" + p.URL
}

func (t *webFetchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "URL to fetch"},
			"max_length": {"type": "integer", "description": "Max chars to return (default 50000)"}
		},
		"required": ["url"]
	}`)
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func stripHTML(s string) string {
	// Remove script and style blocks first
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	s = scriptRe.ReplaceAllString(s, "")
	s = styleRe.ReplaceAllString(s, "")
	// Strip remaining tags
	s = htmlTagRe.ReplaceAllString(s, "")
	// Collapse whitespace
	spaceRe := regexp.MustCompile(`\s+`)
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func (t *webFetchTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		URL       string `json:"url"`
		MaxLength int    `json:"max_length"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	if params.URL == "" {
		return &Result{
			Output: "Error: url is required",
			Title:  "web_fetch",
			Error:  fmt.Errorf("url is required"),
		}, nil
	}
	if params.MaxLength <= 0 {
		params.MaxLength = 50000
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, params.URL, nil)
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "web_fetch " + params.URL,
			Error:  err,
		}, nil
	}
	req.Header.Set("User-Agent", "altcode/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "web_fetch " + params.URL,
			Error:  err,
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &Result{
			Output: fmt.Sprintf("Error: HTTP %d", resp.StatusCode),
			Title:  "web_fetch " + params.URL,
			Error:  fmt.Errorf("http %d", resp.StatusCode),
		}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max read
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error reading body: %v", err),
			Title:  "web_fetch " + params.URL,
			Error:  err,
		}, nil
	}

	text := stripHTML(string(body))
	if len(text) > params.MaxLength {
		text = text[:params.MaxLength]
	}

	return &Result{
		Output: text,
		Title:  fmt.Sprintf("web_fetch %s (%d chars)", params.URL, len(text)),
	}, nil
}

// --- WebSearch ---

type webSearchTool struct{}

// NewWebSearchTool creates a tool that searches the web.
func NewWebSearchTool() Tool { return &webSearchTool{} }

func (t *webSearchTool) Name() string { return "web_search" }
func (t *webSearchTool) Description() string {
	return "Search the web for information. Returns relevant snippets."
}
func (t *webSearchTool) IsConcurrencySafe() bool { return true }
func (t *webSearchTool) IsReadOnly() bool        { return true }
func (t *webSearchTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ Query string `json:"query"` }
	json.Unmarshal(input, &p)
	return "web_search:" + p.Query
}

func (t *webSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query"}
		},
		"required": ["query"]
	}`)
}

type searchResult struct {
	Title   string
	Snippet string
	URL     string
}

func parseDDGResults(html string) []searchResult {
	var results []searchResult
	// DuckDuckGo HTML results have class="result__a" for titles
	// and class="result__snippet" for snippets
	titleRe := regexp.MustCompile(
		`(?is)<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`,
	)
	snippetRe := regexp.MustCompile(
		`(?is)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`,
	)

	titles := titleRe.FindAllStringSubmatch(html, 5)
	snippets := snippetRe.FindAllStringSubmatch(html, 5)

	for i, m := range titles {
		r := searchResult{
			Title: stripHTML(m[2]),
			URL:   m[1],
		}
		if i < len(snippets) {
			r.Snippet = stripHTML(snippets[i][1])
		}
		results = append(results, r)
	}
	return results
}

func (t *webSearchTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	if params.Query == "" {
		return &Result{
			Output: "Error: query is required",
			Title:  "web_search",
		}, nil
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(params.Query)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "web_search",
		}, nil
	}
	req.Header.Set("User-Agent", "altcode/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "web_search",
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &Result{
			Output: fmt.Sprintf("Error reading response: %v", err),
			Title:  "web_search",
		}, nil
	}

	results := parseDDGResults(string(body))
	if len(results) == 0 {
		return &Result{
			Output: "No results found for: " + params.Query,
			Title:  "web_search: " + params.Query,
		}, nil
	}

	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, r.Title)
		if r.URL != "" {
			fmt.Fprintf(&sb, "   URL: %s\n", r.URL)
		}
		if r.Snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", r.Snippet)
		}
		sb.WriteString("\n")
	}

	return &Result{
		Output: sb.String(),
		Title:  fmt.Sprintf("web_search: %s (%d results)", params.Query, len(results)),
	}, nil
}
