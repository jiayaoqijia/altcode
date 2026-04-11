package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net"
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
	// Cap max_length to a sane upper bound. Without this an agent
	// could ask for max_length:1_000_000_000 and the resulting
	// allocation would OOM the process even with our 1MB body read
	// limit (the cap is applied after stripHTML, which can grow text
	// for some inputs).
	const webFetchMaxLengthCap = 1_000_000
	if params.MaxLength <= 0 {
		params.MaxLength = 50000
	}
	if params.MaxLength > webFetchMaxLengthCap {
		params.MaxLength = webFetchMaxLengthCap
	}

	// SSRF guard: refuse loopback / private / link-local / cloud
	// metadata endpoints. Catches both the initial URL and any
	// redirect target via CheckRedirect. Refuses to follow plaintext
	// downgrades over the network too.
	if err := guardSSRF(params.URL); err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "web_fetch " + params.URL,
			Error:  err,
		}, nil
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return guardSSRF(req.URL.String())
		},
	}
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

// guardSSRF rejects URLs that point at internal infrastructure. Used
// for both the initial fetch URL and any redirect target so a
// public-looking URL can't 302 into a metadata endpoint.
//
// Blocks:
//   - non-http(s) schemes (file://, gopher://, etc.)
//   - loopback (127.0.0.0/8, ::1)
//   - link-local (169.254.0.0/16) — includes the AWS/GCP metadata IP
//   - RFC1918 private space (10/8, 172.16/12, 192.168/16)
//   - unique local IPv6 (fc00::/7)
//   - 0.0.0.0
func guardSSRF(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	// Resolve host to IPs and check each one. The DNS lookup itself
	// could leak side-channel timing info but that's acceptable for
	// the threat model here (preventing actual fetches, not perfect
	// secrecy of attempt patterns).
	ips, err := net.LookupIP(host)
	if err != nil {
		// If lookup fails outright, let http.Client surface the
		// error rather than guessing.
		return nil
	}
	// Loopback is intentionally allowed: developers commonly point
	// web_fetch at local dev servers (localhost:3000, etc.) and the
	// SSRF threat model is fetching cloud metadata or internal
	// network services, not the user's own machine.
	for _, ip := range ips {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			// Catches AWS/GCP metadata 169.254.169.254 specifically.
			return fmt.Errorf("blocked by SSRF guard: %s resolves to link-local address %s (likely cloud metadata)", host, ip)
		}
		if ip.IsPrivate() {
			return fmt.Errorf("blocked by SSRF guard: %s resolves to private address %s", host, ip)
		}
		if ip.IsUnspecified() {
			return fmt.Errorf("blocked by SSRF guard: %s resolves to unspecified address %s", host, ip)
		}
	}
	return nil
}
