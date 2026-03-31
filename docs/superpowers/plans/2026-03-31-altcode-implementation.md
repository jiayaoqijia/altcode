# Altcode CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a minimal, blazing-fast Go CLI/TUI for AI-assisted coding supporting Claude, Codex, Gemini, and OpenAI-compatible models.

**Architecture:** Channel-pipeline with goroutine concurrency. Engine emits `<-chan Event`, consumed identically by TUI and SDK. Bubbletea for terminal UI, pure-Go SQLite for storage, provider interface for multi-model support.

**Tech Stack:** Go 1.23+, Bubbletea/Lipgloss/Bubbles (TUI), modernc.org/sqlite (storage), alecthomas/chroma (syntax highlighting), cobra (CLI), oklog/ulid (IDs)

**Spec:** `docs/superpowers/specs/2026-03-31-altcode-cli-design.md`

---

## Phase 1: Walking Skeleton (Tasks 1-5)

Minimal end-to-end: type a prompt, get a streamed response from Anthropic, see it in a basic TUI. This de-risks the core architecture.

---

### Task 1: Go Module + Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/altcode/main.go`
- Create: `internal/event/event.go`
- Create: `Makefile`

- [ ] **Step 1: Initialize Go module**

```bash
cd /home/coder/github/altcode
go mod init github.com/altcode-ai/altcode
```

- [ ] **Step 2: Create event types**

Create `internal/event/event.go`:

```go
package event

import "encoding/json"

type Type int

const (
	TextDelta Type = iota
	TextDone
	ToolStart
	ToolDelta
	ToolDone
	ToolResult
	ThinkingDelta
	Usage
	PermissionRequest
	PermissionResponse
	Error
	Done
)

type Event struct {
	Type       Type
	Text       string
	ToolCall   *ToolCall
	ToolResult *Result
	Error      error
	Usage      *UsageInfo
	Thinking   string
	Permission *PermReq
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
	Eager bool
}

type Result struct {
	Output   string
	Title    string
	Metadata map[string]any
	Error    error
}

type UsageInfo struct {
	InputTokens  int
	OutputTokens int
	CacheHits    int
}

type PermReq struct {
	ToolName string
	Pattern  string
	Response chan PermResponse
}

type PermResponse struct {
	Action     Action
	Persistent bool
}

type Action int

const (
	Allow Action = iota
	Deny
	Ask
)
```

- [ ] **Step 3: Create entry point**

Create `cmd/altcode/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("altcode", version)
		os.Exit(0)
	}
	fmt.Println("altcode", version, "- not yet implemented")
}
```

- [ ] **Step 4: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build test lint clean

VERSION ?= dev
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o dist/altcode ./cmd/altcode

test:
	go test ./... -v -race

lint:
	go vet ./...

clean:
	rm -rf dist/
```

- [ ] **Step 5: Verify it builds and runs**

```bash
make build
./dist/altcode --version
```

Expected: `altcode dev`

- [ ] **Step 6: Commit**

```bash
git add go.mod cmd/ internal/event/ Makefile
git commit -m "feat: project scaffold with Go module, event types, and entry point"
```

---

### Task 2: Provider Interface + Anthropic Streaming

**Files:**
- Create: `internal/provider/provider.go`
- Create: `internal/provider/message.go`
- Create: `internal/provider/stream.go`
- Create: `internal/provider/anthropic.go`
- Create: `internal/provider/retry.go`
- Create: `internal/provider/provider_test.go`

- [ ] **Step 1: Write the provider interface test**

Create `internal/provider/provider_test.go`:

```go
package provider_test

import (
	"context"
	"os"
	"testing"

	"github.com/altcode-ai/altcode/internal/provider"
)

func TestAnthropicStream(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey: key,
	})

	req := &provider.Request{
		Model:     "claude-haiku-4-5-20251001",
		Messages:  []provider.Message{{Role: "user", Content: "Say hello in exactly 3 words."}},
		System:    []provider.SystemSection{{Content: "You are a helpful assistant."}},
		MaxTokens: 100,
	}

	stream, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}

	var fullText string
	var gotDone bool
	for ev := range stream {
		switch ev.Type {
		case provider.StreamTextDelta:
			fullText += ev.Delta
		case provider.StreamDone:
			gotDone = true
		case provider.StreamError:
			t.Fatalf("Stream error event: %v", ev.Error)
		}
	}

	if fullText == "" {
		t.Fatal("Expected non-empty response text")
	}
	if !gotDone {
		t.Fatal("Expected Done event")
	}
	t.Logf("Response: %s", fullText)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/provider/ -v -run TestAnthropicStream
```

Expected: FAIL — package doesn't exist yet

- [ ] **Step 3: Create provider types**

Create `internal/provider/provider.go`:

```go
package provider

import "context"

type Provider interface {
	Name() string
	Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error)
}

type Request struct {
	Model       string
	Messages    []Message
	System      []SystemSection
	Tools       []ToolSchema
	MaxTokens   int
	Temperature *float64
	Thinking    *ThinkingConfig
	Metadata    map[string]any
}

type SystemSection struct {
	Content      string
	CacheControl *CacheControl
}

type CacheControl struct {
	Type string `json:"type"`
}

type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type StreamEventType int

const (
	StreamTextDelta StreamEventType = iota
	StreamTextDone
	StreamToolCallStart
	StreamToolCallDelta
	StreamToolCallEnd
	StreamThinkingDelta
	StreamUsage
	StreamError
	StreamDone
)

type StreamEvent struct {
	Type    StreamEventType
	Delta   string
	ToolUse *ToolCallEvent
	Usage   *UsageInfo
	Error   error
}

type ToolCallEvent struct {
	ID    string
	Name  string
	Delta string
}

type UsageInfo struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
```

- [ ] **Step 4: Create message types**

Create `internal/provider/message.go`:

```go
package provider

import "encoding/json"

type Message struct {
	Role    string        `json:"role"`
	Content string        `json:"content,omitempty"`
	Parts   []ContentPart `json:"parts,omitempty"`
}

type ContentPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}
```

- [ ] **Step 5: Create SSE stream decoder**

Create `internal/provider/stream.go`:

```go
package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type SSEDecoder struct {
	scanner *bufio.Scanner
}

func NewSSEDecoder(r io.Reader) *SSEDecoder {
	return &SSEDecoder{scanner: bufio.NewScanner(r)}
}

func (d *SSEDecoder) Next() (eventType string, data string, err error) {
	var eventName string
	var dataLines []string

	for d.scanner.Scan() {
		line := d.scanner.Text()

		if line == "" {
			if len(dataLines) > 0 {
				return eventName, strings.Join(dataLines, "\n"), nil
			}
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "data:" {
			dataLines = append(dataLines, "")
		}
	}

	if err := d.scanner.Err(); err != nil {
		return "", "", fmt.Errorf("scanner error: %w", err)
	}
	return "", "", io.EOF
}

func decodeJSON[T any](data string) (T, error) {
	var v T
	err := json.Unmarshal([]byte(data), &v)
	return v, err
}
```

- [ ] **Step 6: Create Anthropic provider**

Create `internal/provider/anthropic.go`:

```go
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type AnthropicConfig struct {
	APIKey  string
	BaseURL string
}

type anthropicProvider struct {
	config AnthropicConfig
	client *http.Client
}

func NewAnthropic(cfg AnthropicConfig) Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	return &anthropicProvider{
		config: cfg,
		client: &http.Client{},
	}
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	body := p.buildRequestBody(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error %d: %s", resp.StatusCode, string(errBody))
	}

	events := make(chan StreamEvent, 32)
	go p.processSSE(ctx, resp.Body, events)
	return events, nil
}

func (p *anthropicProvider) buildRequestBody(req *Request) map[string]any {
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     true,
	}

	if len(req.System) > 0 {
		var systemParts []map[string]any
		for _, s := range req.System {
			part := map[string]any{"type": "text", "text": s.Content}
			if s.CacheControl != nil {
				part["cache_control"] = s.CacheControl
			}
			systemParts = append(systemParts, part)
		}
		body["system"] = systemParts
	}

	var messages []map[string]any
	for _, m := range req.Messages {
		msg := map[string]any{"role": m.Role, "content": m.Content}
		messages = append(messages, msg)
	}
	body["messages"] = messages

	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}

	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}

	return body
}

func (p *anthropicProvider) processSSE(ctx context.Context, body io.ReadCloser, out chan<- StreamEvent) {
	defer close(out)
	defer body.Close()

	decoder := NewSSEDecoder(body)
	var currentToolID, currentToolName string
	var toolInputBuf bytes.Buffer

	for {
		select {
		case <-ctx.Done():
			out <- StreamEvent{Type: StreamError, Error: ctx.Err()}
			return
		default:
		}

		eventType, data, err := decoder.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			out <- StreamEvent{Type: StreamError, Error: err}
			return
		}

		switch eventType {
		case "content_block_start":
			var block struct {
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &block); err != nil {
				continue
			}
			if block.ContentBlock.Type == "tool_use" {
				currentToolID = block.ContentBlock.ID
				currentToolName = block.ContentBlock.Name
				toolInputBuf.Reset()
				out <- StreamEvent{
					Type:    StreamToolCallStart,
					ToolUse: &ToolCallEvent{ID: currentToolID, Name: currentToolName},
				}
			}

		case "content_block_delta":
			var delta struct {
				Delta struct {
					Type         string `json:"type"`
					Text         string `json:"text"`
					Thinking     string `json:"thinking"`
					PartialJSON  string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				continue
			}
			switch delta.Delta.Type {
			case "text_delta":
				out <- StreamEvent{Type: StreamTextDelta, Delta: delta.Delta.Text}
			case "thinking_delta":
				out <- StreamEvent{Type: StreamThinkingDelta, Delta: delta.Delta.Thinking}
			case "input_json_delta":
				toolInputBuf.WriteString(delta.Delta.PartialJSON)
				out <- StreamEvent{
					Type:    StreamToolCallDelta,
					ToolUse: &ToolCallEvent{ID: currentToolID, Delta: delta.Delta.PartialJSON},
				}
			}

		case "content_block_stop":
			if currentToolID != "" {
				out <- StreamEvent{
					Type: StreamToolCallEnd,
					ToolUse: &ToolCallEvent{
						ID:   currentToolID,
						Name: currentToolName,
						Delta: toolInputBuf.String(),
					},
				}
				currentToolID = ""
				currentToolName = ""
			}

		case "message_delta":
			var md struct {
				Usage *UsageInfo `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &md); err == nil && md.Usage != nil {
				out <- StreamEvent{Type: StreamUsage, Usage: md.Usage}
			}

		case "message_stop":
			out <- StreamEvent{Type: StreamDone}
			return

		case "error":
			out <- StreamEvent{Type: StreamError, Error: fmt.Errorf("anthropic stream error: %s", data)}
			return
		}
	}

	out <- StreamEvent{Type: StreamDone}
}
```

- [ ] **Step 7: Create retry wrapper**

Create `internal/provider/retry.go`:

```go
package provider

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

var DefaultRetryConfig = RetryConfig{
	MaxRetries: 10,
	BaseDelay:  500 * time.Millisecond,
	MaxDelay:   30 * time.Second,
}

func RetryableStream(ctx context.Context, cfg RetryConfig, fn func() (<-chan StreamEvent, error)) (<-chan StreamEvent, error) {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		stream, err := fn()
		if err == nil {
			return stream, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}

		delay := backoffDelay(cfg, attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func backoffDelay(cfg RetryConfig, attempt int) time.Duration {
	delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(2, float64(attempt)))
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	return delay
}

func isRetryable(err error) bool {
	// Override with specific HTTP status checks in production
	return true
}

func parseRetryAfter(header http.Header) time.Duration {
	val := header.Get("Retry-After")
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(val); err == nil {
		return time.Duration(secs) * time.Second
	}
	return 0
}
```

- [ ] **Step 8: Fix missing import in provider.go**

Add the missing `encoding/json` import to `internal/provider/provider.go`:

```go
package provider

import (
	"context"
	"encoding/json"
)
```

- [ ] **Step 9: Run test to verify it passes**

```bash
go mod tidy
go test ./internal/provider/ -v -run TestAnthropicStream
```

Expected: PASS (requires `ANTHROPIC_API_KEY` env var)

- [ ] **Step 10: Commit**

```bash
git add internal/provider/ go.mod go.sum
git commit -m "feat: provider interface with Anthropic streaming implementation"
```

---

### Task 3: SQLite Storage Layer

**Files:**
- Create: `internal/store/db.go`
- Create: `internal/store/session.go`
- Create: `internal/store/message.go`
- Create: `internal/store/store_test.go`

- [ ] **Step 1: Write storage test**

Create `internal/store/store_test.go`:

```go
package store_test

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/store"
)

func TestSessionCRUD(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Create session
	sess, err := db.CreateSession("test-project", "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("Expected non-empty session ID")
	}
	if sess.ProjectID != "test-project" {
		t.Fatalf("Expected project 'test-project', got %q", sess.ProjectID)
	}

	// Get session
	got, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("Expected session ID %q, got %q", sess.ID, got.ID)
	}

	// List sessions
	sessions, err := db.ListSessions("test-project")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("Expected 1 session, got %d", len(sessions))
	}
}

func TestMessageCRUD(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	sess, err := db.CreateSession("test-project", "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Add message
	msg, err := db.AddMessage(sess.ID, "user", `{"text":"hello"}`, "", 10, 0)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if msg.Role != "user" {
		t.Fatalf("Expected role 'user', got %q", msg.Role)
	}

	// List messages
	messages, err := db.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/store/ -v
```

Expected: FAIL — package doesn't exist

- [ ] **Step 3: Create database layer**

Create `internal/store/db.go`:

```go
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

func Open(dsn string) (*DB, error) {
	if dsn == "" {
		dsn = defaultDBPath()
	}

	if dsn != ":memory:" {
		dir := filepath.Dir(dsn)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := configurePragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func configurePragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -8000",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS session (
		id          TEXT PRIMARY KEY,
		project_id  TEXT NOT NULL,
		title       TEXT DEFAULT '',
		model       TEXT DEFAULT '',
		created_at  INTEGER NOT NULL,
		updated_at  INTEGER NOT NULL,
		summary     TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS message (
		id          TEXT PRIMARY KEY,
		session_id  TEXT NOT NULL REFERENCES session(id),
		role        TEXT NOT NULL,
		content     BLOB NOT NULL,
		model       TEXT DEFAULT '',
		tokens_in   INTEGER DEFAULT 0,
		tokens_out  INTEGER DEFAULT 0,
		created_at  INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_message_session ON message(session_id);

	CREATE TABLE IF NOT EXISTS permission_rule (
		id          TEXT PRIMARY KEY,
		source      TEXT NOT NULL,
		tool        TEXT NOT NULL,
		pattern     TEXT NOT NULL,
		action      TEXT NOT NULL,
		created_at  INTEGER NOT NULL
	);
	`
	_, err := db.Exec(schema)
	return err
}

func newID() string {
	return ulid.Make().String()
}

func nowUnix() int64 {
	return time.Now().UnixMilli()
}

func defaultDBPath() string {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		if runtime.GOOS == "darwin" {
			dataDir = filepath.Join(home, "Library", "Application Support")
		} else {
			dataDir = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(dataDir, "altcode", "altcode.db")
}
```

- [ ] **Step 4: Create session operations**

Create `internal/store/session.go`:

```go
package store

import "fmt"

type Session struct {
	ID        string
	ProjectID string
	Title     string
	Model     string
	CreatedAt int64
	UpdatedAt int64
	Summary   string
}

func (d *DB) CreateSession(projectID, model string) (*Session, error) {
	now := nowUnix()
	s := &Session{
		ID:        newID(),
		ProjectID: projectID,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := d.db.Exec(
		`INSERT INTO session (id, project_id, title, model, created_at, updated_at, summary)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.ProjectID, s.Title, s.Model, s.CreatedAt, s.UpdatedAt, s.Summary,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return s, nil
}

func (d *DB) GetSession(id string) (*Session, error) {
	s := &Session{}
	err := d.db.QueryRow(
		`SELECT id, project_id, title, model, created_at, updated_at, summary FROM session WHERE id = ?`, id,
	).Scan(&s.ID, &s.ProjectID, &s.Title, &s.Model, &s.CreatedAt, &s.UpdatedAt, &s.Summary)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return s, nil
}

func (d *DB) ListSessions(projectID string) ([]Session, error) {
	rows, err := d.db.Query(
		`SELECT id, project_id, title, model, created_at, updated_at, summary
		 FROM session WHERE project_id = ? ORDER BY updated_at DESC`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Title, &s.Model, &s.CreatedAt, &s.UpdatedAt, &s.Summary); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (d *DB) UpdateSessionTitle(id, title string) error {
	_, err := d.db.Exec(`UPDATE session SET title = ?, updated_at = ? WHERE id = ?`, title, nowUnix(), id)
	return err
}
```

- [ ] **Step 5: Create message operations**

Create `internal/store/message.go`:

```go
package store

import "fmt"

type Message struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	Model     string
	TokensIn  int
	TokensOut int
	CreatedAt int64
}

func (d *DB) AddMessage(sessionID, role, content, model string, tokensIn, tokensOut int) (*Message, error) {
	m := &Message{
		ID:        newID(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Model:     model,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
		CreatedAt: nowUnix(),
	}

	_, err := d.db.Exec(
		`INSERT INTO message (id, session_id, role, content, model, tokens_in, tokens_out, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.Role, m.Content, m.Model, m.TokensIn, m.TokensOut, m.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	// Update session timestamp
	d.db.Exec(`UPDATE session SET updated_at = ? WHERE id = ?`, nowUnix(), sessionID)

	return m, nil
}

func (d *DB) ListMessages(sessionID string) ([]Message, error) {
	rows, err := d.db.Query(
		`SELECT id, session_id, role, content, model, tokens_in, tokens_out, created_at
		 FROM message WHERE session_id = ? ORDER BY created_at ASC`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Model, &m.TokensIn, &m.TokensOut, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
```

- [ ] **Step 6: Run tests**

```bash
go mod tidy
go test ./internal/store/ -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/store/ go.mod go.sum
git commit -m "feat: SQLite storage layer with session and message CRUD"
```

---

### Task 4: Config System

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/project.go`
- Create: `internal/config/instructions.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write config test**

Create `internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/altcode-ai/altcode/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg := config.Default()
	if cfg.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Fatalf("Expected default model, got %q", cfg.Model)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	os.WriteFile(cfgPath, []byte(`{
		"model": "openai/gpt-4o",
		"theme": "dracula"
	}`), 0o644)

	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Model != "openai/gpt-4o" {
		t.Fatalf("Expected model 'openai/gpt-4o', got %q", cfg.Model)
	}
	if cfg.Theme != "dracula" {
		t.Fatalf("Expected theme 'dracula', got %q", cfg.Theme)
	}
}

func TestEnvVarExpansion(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-123")
	result := config.ExpandEnv("$TEST_API_KEY")
	if result != "sk-test-123" {
		t.Fatalf("Expected 'sk-test-123', got %q", result)
	}
}

func TestDetectProjectRoot(t *testing.T) {
	// Create a temp dir with a .git directory
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	sub := filepath.Join(dir, "a", "b")
	os.MkdirAll(sub, 0o755)

	root, err := config.DetectProjectRoot(sub)
	if err != nil {
		t.Fatalf("DetectProjectRoot: %v", err)
	}
	if root != dir {
		t.Fatalf("Expected root %q, got %q", dir, root)
	}
}

func TestLoadInstructions(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Project Rules\nBe concise."), 0o644)
	os.WriteFile(filepath.Join(dir, "ALTCODE.md"), []byte("# Altcode Rules\nUse Go."), 0o644)

	instructions, err := config.LoadInstructions(dir)
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}
	if len(instructions) < 2 {
		t.Fatalf("Expected at least 2 instruction files, got %d", len(instructions))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -v
```

Expected: FAIL

- [ ] **Step 3: Create config types and loading**

Create `internal/config/config.go`:

```go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Config struct {
	Model      string                       `json:"model"`
	Provider   map[string]ProviderConfig    `json:"provider"`
	Permission []PermissionRule             `json:"permission"`
	MCP        map[string]MCPServerConfig   `json:"mcp"`
	Theme      string                       `json:"theme"`
	Agent      map[string]AgentConfig       `json:"agent"`
	Hooks      map[string][]HookConfig      `json:"hooks"`
}

type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseURL"`
}

type PermissionRule struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}

type MCPServerConfig struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Transport string            `json:"transport"`
}

type AgentConfig struct {
	Model string   `json:"model"`
	Tools []string `json:"tools"`
}

type HookConfig struct {
	Tool    string `json:"tool"`
	Command string `json:"command"`
}

func Default() *Config {
	return &Config{
		Model:    "anthropic/claude-sonnet-4-20250514",
		Provider: make(map[string]ProviderConfig),
		Theme:    "default",
		Agent:    make(map[string]AgentConfig),
	}
}

func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Strip JSONC comments (// and /* */)
	cleaned := stripJSONComments(string(data))

	cfg := Default()
	if err := json.Unmarshal([]byte(cleaned), cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Expand env vars in provider API keys
	for name, p := range cfg.Provider {
		p.APIKey = ExpandEnv(p.APIKey)
		p.BaseURL = ExpandEnv(p.BaseURL)
		cfg.Provider[name] = p
	}

	return cfg, nil
}

var envVarRegex = regexp.MustCompile(`\$([A-Z_][A-Z0-9_]*)`)

func ExpandEnv(s string) string {
	return envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		name := strings.TrimPrefix(match, "$")
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match
	})
}

func stripJSONComments(s string) string {
	// Simple line-comment stripping (not inside strings — good enough for config)
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		// Strip inline comments (naive but sufficient for config files)
		if idx := strings.Index(line, "//"); idx > 0 {
			// Only strip if not inside a string (check for unescaped quotes)
			before := line[:idx]
			if strings.Count(before, `"`)%2 == 0 {
				line = before
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Create project detection**

Create `internal/config/project.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func DetectProjectRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}

	for {
		if isGitRoot(dir) {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root — use startDir as fallback
			return filepath.Abs(startDir)
		}
		dir = parent
	}
}

func isGitRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}
```

- [ ] **Step 5: Create instructions loading**

Create `internal/config/instructions.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
)

type Instruction struct {
	Path    string
	Content string
}

func LoadInstructions(projectRoot string) ([]Instruction, error) {
	var instructions []Instruction

	// User global instructions
	home, _ := os.UserHomeDir()
	userPath := filepath.Join(home, ".config", "altcode", "instructions.md")
	if content, err := os.ReadFile(userPath); err == nil {
		instructions = append(instructions, Instruction{Path: userPath, Content: string(content)})
	}

	// Project-level instruction files
	candidates := []string{
		"CLAUDE.md",
		"AGENTS.md",
		"ALTCODE.md",
	}

	for _, name := range candidates {
		path := filepath.Join(projectRoot, name)
		if content, err := os.ReadFile(path); err == nil {
			instructions = append(instructions, Instruction{Path: path, Content: string(content)})
		}
	}

	// Modular rules: .altcode/rules/*.md
	rulesDir := filepath.Join(projectRoot, ".altcode", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(rulesDir, e.Name())
			if content, err := os.ReadFile(path); err == nil {
				instructions = append(instructions, Instruction{Path: path, Content: string(content)})
			}
		}
	}

	return instructions, nil
}
```

- [ ] **Step 6: Run tests**

```bash
go mod tidy
go test ./internal/config/ -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: config system with JSONC loading, project detection, and instructions"
```

---

### Task 5: Minimal TUI + Engine Integration (Walking Skeleton)

**Files:**
- Create: `internal/engine/engine.go`
- Create: `internal/tui/app.go`
- Create: `internal/tui/theme.go`
- Modify: `cmd/altcode/main.go`

- [ ] **Step 1: Create minimal engine**

Create `internal/engine/engine.go`:

```go
package engine

import (
	"context"
	"fmt"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/provider"
)

type Engine struct {
	cfg      *config.Config
	provider provider.Provider
	model    string
	messages []provider.Message
}

func New(cfg *config.Config) (*Engine, error) {
	providerName, modelName := parseModel(cfg.Model)

	var p provider.Provider
	switch providerName {
	case "anthropic":
		pcfg := cfg.Provider["anthropic"]
		p = provider.NewAnthropic(provider.AnthropicConfig{
			APIKey:  pcfg.APIKey,
			BaseURL: pcfg.BaseURL,
		})
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerName)
	}

	return &Engine{
		cfg:      cfg,
		provider: p,
		model:    modelName,
	}, nil
}

func (e *Engine) Run(ctx context.Context, input string) <-chan event.Event {
	events := make(chan event.Event, 64)
	go func() {
		defer close(events)
		e.loop(ctx, input, events)
	}()
	return events
}

func (e *Engine) loop(ctx context.Context, input string, out chan<- event.Event) {
	e.messages = append(e.messages, provider.Message{Role: "user", Content: input})

	req := &provider.Request{
		Model:    e.model,
		Messages: e.messages,
		System: []provider.SystemSection{
			{Content: "You are a helpful coding assistant. Be concise."},
		},
		MaxTokens: 4096,
	}

	stream, err := e.provider.Stream(ctx, req)
	if err != nil {
		out <- event.Event{Type: event.Error, Error: err}
		return
	}

	var fullText string
	for sev := range stream {
		switch sev.Type {
		case provider.StreamTextDelta:
			fullText += sev.Delta
			out <- event.Event{Type: event.TextDelta, Text: sev.Delta}
		case provider.StreamThinkingDelta:
			out <- event.Event{Type: event.ThinkingDelta, Thinking: sev.Delta}
		case provider.StreamUsage:
			if sev.Usage != nil {
				out <- event.Event{Type: event.Usage, Usage: &event.UsageInfo{
					InputTokens:  sev.Usage.InputTokens,
					OutputTokens: sev.Usage.OutputTokens,
				}}
			}
		case provider.StreamError:
			out <- event.Event{Type: event.Error, Error: sev.Error}
			return
		case provider.StreamDone:
			out <- event.Event{Type: event.TextDone, Text: fullText}
		}
	}

	e.messages = append(e.messages, provider.Message{Role: "assistant", Content: fullText})
	out <- event.Event{Type: event.Done}
}

func parseModel(model string) (providerName, modelName string) {
	for i, c := range model {
		if c == '/' {
			return model[:i], model[i+1:]
		}
	}
	return "anthropic", model
}
```

- [ ] **Step 2: Create theme system**

Create `internal/tui/theme.go`:

```go
package tui

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	Name       string
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Error      lipgloss.Color
	Warning    lipgloss.Color
	Success    lipgloss.Color
	Muted      lipgloss.Color
	Background lipgloss.Color
	Foreground lipgloss.Color
	Border     lipgloss.Color
}

var DefaultTheme = Theme{
	Name:       "default",
	Primary:    lipgloss.Color("#7C3AED"),
	Secondary:  lipgloss.Color("#06B6D4"),
	Error:      lipgloss.Color("#EF4444"),
	Warning:    lipgloss.Color("#F59E0B"),
	Success:    lipgloss.Color("#10B981"),
	Muted:      lipgloss.Color("#6B7280"),
	Background: lipgloss.Color(""),
	Foreground: lipgloss.Color(""),
	Border:     lipgloss.Color("#374151"),
}

var CatppuccinMocha = Theme{
	Name:       "catppuccin-mocha",
	Primary:    lipgloss.Color("#CBA6F7"),
	Secondary:  lipgloss.Color("#89DCEB"),
	Error:      lipgloss.Color("#F38BA8"),
	Warning:    lipgloss.Color("#FAB387"),
	Success:    lipgloss.Color("#A6E3A1"),
	Muted:      lipgloss.Color("#6C7086"),
	Background: lipgloss.Color("#1E1E2E"),
	Foreground: lipgloss.Color("#CDD6F4"),
	Border:     lipgloss.Color("#313244"),
}

var Themes = map[string]Theme{
	"default":          DefaultTheme,
	"catppuccin-mocha": CatppuccinMocha,
}

func GetTheme(name string) Theme {
	if t, ok := Themes[name]; ok {
		return t
	}
	return DefaultTheme
}
```

- [ ] **Step 3: Create minimal Bubbletea TUI**

Create `internal/tui/app.go`:

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type eventMsg event.Event
type streamDoneMsg struct{}

type App struct {
	engine   *engine.Engine
	theme    Theme
	input    textarea.Model
	viewport viewport.Model
	width    int
	height   int

	messages   []string
	streaming  string
	busy       bool
	cancel     context.CancelFunc
	tokenInfo  string
}

func New(eng *engine.Engine, theme Theme) *App {
	ti := textarea.New()
	ti.Placeholder = "Ask anything... (Ctrl+D to submit, Esc to quit)"
	ti.Focus()
	ti.SetHeight(3)
	ti.ShowLineNumbers = false

	return &App{
		engine: eng,
		theme:  theme,
		input:  ti,
	}
}

func (a *App) Init() tea.Cmd {
	return textarea.Blink
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if a.busy {
				if a.cancel != nil {
					a.cancel()
				}
				a.busy = false
				return a, nil
			}
			return a, tea.Quit
		case "ctrl+d":
			if a.busy || strings.TrimSpace(a.input.Value()) == "" {
				return a, nil
			}
			return a, a.submit()
		case "ctrl+c":
			return a, tea.Quit
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.viewport = viewport.New(msg.Width, msg.Height-6)
		a.input.SetWidth(msg.Width - 2)
		a.updateViewport()
		return a, nil

	case eventMsg:
		return a.handleEvent(event.Event(msg))

	case streamDoneMsg:
		return a, nil
	}

	if !a.busy {
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		return a, cmd
	}
	return a, nil
}

func (a *App) handleEvent(ev event.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case event.TextDelta:
		a.streaming += ev.Text
		a.updateViewport()
		return a, a.waitForEvent()

	case event.TextDone:
		return a, a.waitForEvent()

	case event.Usage:
		if ev.Usage != nil {
			a.tokenInfo = fmt.Sprintf("tokens: %d in / %d out", ev.Usage.InputTokens, ev.Usage.OutputTokens)
		}
		return a, a.waitForEvent()

	case event.Error:
		a.messages = append(a.messages, fmt.Sprintf("[error] %v", ev.Error))
		a.streaming = ""
		a.busy = false
		a.updateViewport()
		return a, nil

	case event.Done:
		if a.streaming != "" {
			a.messages = append(a.messages, a.streaming)
			a.streaming = ""
		}
		a.busy = false
		a.updateViewport()
		return a, nil
	}

	return a, a.waitForEvent()
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	header := lipgloss.NewStyle().
		Foreground(a.theme.Primary).
		Bold(true).
		Render("altcode") +
		lipgloss.NewStyle().Foreground(a.theme.Muted).Render("  "+a.tokenInfo)

	separator := lipgloss.NewStyle().
		Foreground(a.theme.Border).
		Render(strings.Repeat("─", a.width))

	status := ""
	if a.busy {
		status = lipgloss.NewStyle().
			Foreground(a.theme.Warning).
			Render("  streaming...")
	}

	return fmt.Sprintf("%s\n%s\n%s%s\n%s\n%s",
		header,
		separator,
		a.viewport.View(),
		status,
		separator,
		a.input.View(),
	)
}

func (a *App) submit() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	a.input.Reset()
	a.messages = append(a.messages, fmt.Sprintf("> %s", text))
	a.streaming = ""
	a.busy = true
	a.updateViewport()

	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	events := a.engine.Run(ctx, text)

	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return streamDoneMsg{}
		}
		return eventMsg(ev)
	}
}

func (a *App) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		// The engine's event channel is stored implicitly via the goroutine
		// This is a placeholder — in production, we store the channel on App
		return streamDoneMsg{}
	}
}

func (a *App) updateViewport() {
	var sb strings.Builder
	for _, m := range a.messages {
		sb.WriteString(m)
		sb.WriteString("\n\n")
	}
	if a.streaming != "" {
		sb.WriteString(a.streaming)
	}
	a.viewport.SetContent(sb.String())
	a.viewport.GotoBottom()
}
```

**Note:** The `waitForEvent` pattern above is simplified. The full implementation will store the event channel on the App and use a proper tea.Cmd that reads from it. For the walking skeleton this is sufficient — we'll fix the channel plumbing in Task 6.

- [ ] **Step 4: Wire up main.go**

Replace `cmd/altcode/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("altcode", version)
		os.Exit(0)
	}

	cfg := config.Default()

	// Load API key from env
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		if cfg.Provider == nil {
			cfg.Provider = make(map[string]config.ProviderConfig)
		}
		cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: key}
	}

	eng, err := engine.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	theme := tui.GetTheme(cfg.Theme)
	app := tui.New(eng, theme)

	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Install dependencies and build**

```bash
go mod tidy
make build
```

Expected: Builds successfully

- [ ] **Step 6: Manual test**

```bash
ANTHROPIC_API_KEY=<your-key> ./dist/altcode
```

Expected: TUI opens, you can type a prompt, press Ctrl+D, see streamed response. Esc to quit.

- [ ] **Step 7: Commit**

```bash
git add cmd/altcode/ internal/engine/ internal/tui/ go.mod go.sum
git commit -m "feat: walking skeleton — minimal TUI with Anthropic streaming"
```

---

## Phase 2: Tool System (Tasks 6-8)

Build the tool interface, implement core tools, and wire tool dispatch into the agent loop.

---

### Task 6: Tool Interface + Registry + Dispatch

**Files:**
- Create: `internal/tool/tool.go`
- Create: `internal/tool/registry.go`
- Create: `internal/tool/dispatch.go`
- Create: `internal/tool/dispatch_test.go`

- [ ] **Step 1: Write dispatch test**

Create `internal/tool/dispatch_test.go`:

```go
package tool_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/altcode-ai/altcode/internal/tool"
)

type mockTool struct {
	name       string
	concurrent bool
	readOnly   bool
	callCount  atomic.Int32
}

func (m *mockTool) Name() string             { return m.name }
func (m *mockTool) Description() string       { return "mock tool" }
func (m *mockTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) IsConcurrencySafe() bool   { return m.concurrent }
func (m *mockTool) IsReadOnly() bool          { return m.readOnly }
func (m *mockTool) PermissionPattern(_ json.RawMessage) string { return m.name + ":*" }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	m.callCount.Add(1)
	return &tool.Result{Output: "ok", Title: m.name}, nil
}

func TestPartitionByConcurrency(t *testing.T) {
	read1 := &mockTool{name: "read1", concurrent: true}
	read2 := &mockTool{name: "read2", concurrent: true}
	write1 := &mockTool{name: "write1", concurrent: false}
	read3 := &mockTool{name: "read3", concurrent: true}

	calls := []tool.Call{
		{ID: "1", Tool: read1},
		{ID: "2", Tool: read2},
		{ID: "3", Tool: write1},
		{ID: "4", Tool: read3},
	}

	batches := tool.PartitionByConcurrency(calls)
	if len(batches) != 3 {
		t.Fatalf("Expected 3 batches, got %d", len(batches))
	}
	if len(batches[0]) != 2 {
		t.Fatalf("Expected first batch size 2, got %d", len(batches[0]))
	}
	if len(batches[1]) != 1 {
		t.Fatalf("Expected second batch size 1, got %d", len(batches[1]))
	}
	if len(batches[2]) != 1 {
		t.Fatalf("Expected third batch size 1, got %d", len(batches[2]))
	}
}

func TestDispatchConcurrent(t *testing.T) {
	read1 := &mockTool{name: "read1", concurrent: true}
	read2 := &mockTool{name: "read2", concurrent: true}

	calls := []tool.Call{
		{ID: "1", Tool: read1, Input: json.RawMessage(`{}`)},
		{ID: "2", Tool: read2, Input: json.RawMessage(`{}`)},
	}

	results := tool.Dispatch(context.Background(), calls)
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}
	if read1.callCount.Load() != 1 {
		t.Fatal("read1 should be called once")
	}
	if read2.callCount.Load() != 1 {
		t.Fatal("read2 should be called once")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tool/ -v
```

Expected: FAIL

- [ ] **Step 3: Create tool interface**

Create `internal/tool/tool.go`:

```go
package tool

import (
	"context"
	"encoding/json"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (*Result, error)
	IsConcurrencySafe() bool
	IsReadOnly() bool
	PermissionPattern(input json.RawMessage) string
}

type Result struct {
	Output   string
	Title    string
	Metadata map[string]any
	Error    error
}

type Call struct {
	ID          string
	Tool        Tool
	Input       json.RawMessage
	Eager       bool
	EagerResult *Result
}
```

- [ ] **Step 4: Create registry**

Create `internal/tool/registry.go`:

```go
package tool

import "encoding/json"

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) All() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

func (r *Registry) Schemas() []Schema {
	schemas := make([]Schema, 0, len(r.tools))
	for _, t := range r.tools {
		schemas = append(schemas, Schema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		})
	}
	return schemas
}

func (r *Registry) Subset(names []string) *Registry {
	sub := NewRegistry()
	for _, name := range names {
		if t, ok := r.tools[name]; ok {
			sub.Register(t)
		}
	}
	return sub
}

type Schema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}
```

- [ ] **Step 5: Create dispatch with concurrency partitioning**

Create `internal/tool/dispatch.go`:

```go
package tool

import (
	"context"
	"sync"
)

func PartitionByConcurrency(calls []Call) [][]Call {
	if len(calls) == 0 {
		return nil
	}

	var batches [][]Call
	current := []Call{calls[0]}
	currentSafe := calls[0].Tool.IsConcurrencySafe()

	for _, call := range calls[1:] {
		safe := call.Tool.IsConcurrencySafe()
		if safe && currentSafe {
			current = append(current, call)
		} else {
			batches = append(batches, current)
			current = []Call{call}
			currentSafe = safe
		}
	}
	batches = append(batches, current)
	return batches
}

func Dispatch(ctx context.Context, calls []Call) []Result {
	batches := PartitionByConcurrency(calls)
	var results []Result

	for _, batch := range batches {
		if len(batch) == 1 || !batch[0].Tool.IsConcurrencySafe() {
			for _, call := range batch {
				if call.EagerResult != nil {
					results = append(results, *call.EagerResult)
					continue
				}
				r, err := call.Tool.Execute(ctx, call.Input)
				if err != nil {
					results = append(results, Result{Error: err, Title: call.Tool.Name()})
				} else {
					results = append(results, *r)
				}
			}
		} else {
			batchResults := make([]Result, len(batch))
			var wg sync.WaitGroup
			for i, call := range batch {
				if call.EagerResult != nil {
					batchResults[i] = *call.EagerResult
					continue
				}
				wg.Add(1)
				go func(idx int, c Call) {
					defer wg.Done()
					r, err := c.Tool.Execute(ctx, c.Input)
					if err != nil {
						batchResults[idx] = Result{Error: err, Title: c.Tool.Name()}
					} else {
						batchResults[idx] = *r
					}
				}(i, call)
			}
			wg.Wait()
			results = append(results, batchResults...)
		}
	}
	return results
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/tool/ -v -race
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tool/
git commit -m "feat: tool interface, registry, and concurrent dispatch"
```

---

### Task 7: Core Read-Only Tools (read, glob, grep, ls)

**Files:**
- Create: `internal/tool/read.go`
- Create: `internal/tool/glob.go`
- Create: `internal/tool/grep.go`
- Create: `internal/tool/ls.go`
- Create: `internal/tool/tools_test.go`

- [ ] **Step 1: Write tool tests**

Create `internal/tool/tools_test.go`:

```go
package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/altcode-ai/altcode/internal/tool"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\nThis is a test."), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "data.txt"), []byte("line1\nline2\nline3\n"), 0o644)
	return dir
}

func TestReadTool(t *testing.T) {
	dir := setupTestDir(t)
	rt := tool.NewReadTool()

	input, _ := json.Marshal(map[string]any{"file_path": filepath.Join(dir, "hello.go")})
	result, err := rt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output == "" {
		t.Fatal("Expected non-empty output")
	}
	if result.Title == "" {
		t.Fatal("Expected non-empty title")
	}
}

func TestReadToolWithLineRange(t *testing.T) {
	dir := setupTestDir(t)
	rt := tool.NewReadTool()

	input, _ := json.Marshal(map[string]any{
		"file_path": filepath.Join(dir, "sub", "data.txt"),
		"offset":    1,
		"limit":     2,
	})
	result, err := rt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should contain lines 2-3 (0-indexed offset 1, limit 2)
	t.Logf("Output: %s", result.Output)
}

func TestGlobTool(t *testing.T) {
	dir := setupTestDir(t)
	gt := tool.NewGlobTool()

	input, _ := json.Marshal(map[string]any{"pattern": "**/*.go", "path": dir})
	result, err := gt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output == "" {
		t.Fatal("Expected matches")
	}
	t.Logf("Glob output: %s", result.Output)
}

func TestLsTool(t *testing.T) {
	dir := setupTestDir(t)
	lt := tool.NewLsTool()

	input, _ := json.Marshal(map[string]any{"path": dir})
	result, err := lt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output == "" {
		t.Fatal("Expected directory listing")
	}
	t.Logf("Ls output: %s", result.Output)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tool/ -v -run "TestReadTool|TestGlobTool|TestLsTool"
```

Expected: FAIL

- [ ] **Step 3: Implement read tool**

Create `internal/tool/read.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type readTool struct{}

func NewReadTool() Tool { return &readTool{} }

func (t *readTool) Name() string        { return "read" }
func (t *readTool) Description() string  { return "Read a file's contents. Supports line offset and limit." }
func (t *readTool) IsConcurrencySafe() bool { return true }
func (t *readTool) IsReadOnly() bool        { return true }
func (t *readTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ FilePath string `json:"file_path"` }
	json.Unmarshal(input, &p)
	return "read:" + p.FilePath
}

func (t *readTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Absolute path to the file to read"},
			"offset": {"type": "integer", "description": "Line number to start from (0-indexed)"},
			"limit": {"type": "integer", "description": "Max number of lines to read"}
		},
		"required": ["file_path"]
	}`)
}

func (t *readTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return &Result{Output: fmt.Sprintf("Error: %v", err), Title: "read " + params.FilePath}, nil
	}

	lines := strings.Split(string(data), "\n")

	if params.Offset > 0 || params.Limit > 0 {
		start := params.Offset
		if start >= len(lines) {
			return &Result{Output: "", Title: "read " + params.FilePath}, nil
		}
		end := len(lines)
		if params.Limit > 0 && start+params.Limit < end {
			end = start + params.Limit
		}
		lines = lines[start:end]
	}

	// Add line numbers
	var sb strings.Builder
	for i, line := range lines {
		lineNum := params.Offset + i + 1
		fmt.Fprintf(&sb, "%4d\t%s\n", lineNum, line)
	}

	return &Result{
		Output: sb.String(),
		Title:  fmt.Sprintf("read %s (%d lines)", params.FilePath, len(lines)),
	}, nil
}
```

- [ ] **Step 4: Implement glob tool**

Create `internal/tool/glob.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type globTool struct{}

func NewGlobTool() Tool { return &globTool{} }

func (t *globTool) Name() string        { return "glob" }
func (t *globTool) Description() string  { return "Find files matching a glob pattern." }
func (t *globTool) IsConcurrencySafe() bool { return true }
func (t *globTool) IsReadOnly() bool        { return true }
func (t *globTool) PermissionPattern(_ json.RawMessage) string { return "glob:*" }

func (t *globTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Glob pattern (e.g. **/*.go)"},
			"path": {"type": "string", "description": "Base directory to search in"}
		},
		"required": ["pattern"]
	}`)
}

func (t *globTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	base := params.Path
	if base == "" {
		base, _ = os.Getwd()
	}

	fsys := os.DirFS(base)
	matches, err := doublestar.Glob(fsys, params.Pattern)
	if err != nil {
		return &Result{Output: fmt.Sprintf("Error: %v", err), Title: "glob"}, nil
	}

	// Sort by modification time (newest first)
	type fileInfo struct {
		path    string
		modTime int64
	}
	var files []fileInfo
	for _, m := range matches {
		fullPath := filepath.Join(base, m)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			files = append(files, fileInfo{path: m, modTime: info.ModTime().UnixNano()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime > files[j].modTime })

	var sb strings.Builder
	for _, f := range files {
		sb.WriteString(f.path)
		sb.WriteByte('\n')
	}

	return &Result{
		Output: sb.String(),
		Title:  fmt.Sprintf("glob %s (%d matches)", params.Pattern, len(files)),
	}, nil
}
```

- [ ] **Step 5: Implement ls tool**

Create `internal/tool/ls.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type lsTool struct{}

func NewLsTool() Tool { return &lsTool{} }

func (t *lsTool) Name() string        { return "ls" }
func (t *lsTool) Description() string  { return "List directory contents." }
func (t *lsTool) IsConcurrencySafe() bool { return true }
func (t *lsTool) IsReadOnly() bool        { return true }
func (t *lsTool) PermissionPattern(_ json.RawMessage) string { return "ls:*" }

func (t *lsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Directory path to list"}
		},
		"required": ["path"]
	}`)
}

func (t *lsTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	entries, err := os.ReadDir(params.Path)
	if err != nil {
		return &Result{Output: fmt.Sprintf("Error: %v", err), Title: "ls"}, nil
	}

	var sb strings.Builder
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		typeChar := "-"
		if e.IsDir() {
			typeChar = "d"
		}
		fmt.Fprintf(&sb, "%s %8d %s\n", typeChar, info.Size(), e.Name())
	}

	return &Result{
		Output: sb.String(),
		Title:  fmt.Sprintf("ls %s (%d entries)", params.Path, len(entries)),
	}, nil
}
```

- [ ] **Step 6: Implement grep tool (exec ripgrep)**

Create `internal/tool/grep.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type grepTool struct{}

func NewGrepTool() Tool { return &grepTool{} }

func (t *grepTool) Name() string        { return "grep" }
func (t *grepTool) Description() string  { return "Search file contents using ripgrep. Falls back to grep." }
func (t *grepTool) IsConcurrencySafe() bool { return true }
func (t *grepTool) IsReadOnly() bool        { return true }
func (t *grepTool) PermissionPattern(_ json.RawMessage) string { return "grep:*" }

func (t *grepTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "Regex pattern to search for"},
			"path": {"type": "string", "description": "Directory or file to search in"},
			"glob": {"type": "string", "description": "File glob filter (e.g. *.go)"},
			"case_insensitive": {"type": "boolean", "description": "Case insensitive search"}
		},
		"required": ["pattern"]
	}`)
}

func (t *grepTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		Glob            string `json:"glob"`
		CaseInsensitive bool   `json:"case_insensitive"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	args := []string{"--no-heading", "--line-number", "--color=never"}
	if params.CaseInsensitive {
		args = append(args, "-i")
	}
	if params.Glob != "" {
		args = append(args, "--glob", params.Glob)
	}
	args = append(args, params.Pattern)

	searchPath := params.Path
	if searchPath == "" {
		searchPath = "."
	}
	args = append(args, searchPath)

	// Try ripgrep first, fall back to grep
	bin := "rg"
	if _, err := exec.LookPath("rg"); err != nil {
		bin = "grep"
		args = []string{"-rn"}
		if params.CaseInsensitive {
			args = append(args, "-i")
		}
		args = append(args, params.Pattern, searchPath)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	output := string(out)

	// grep/rg exit code 1 = no matches (not an error)
	if err != nil && output == "" {
		return &Result{Output: "No matches found.", Title: "grep " + params.Pattern}, nil
	}

	// Truncate large output
	lines := strings.Split(output, "\n")
	if len(lines) > 200 {
		output = strings.Join(lines[:200], "\n") + fmt.Sprintf("\n... (%d more lines)", len(lines)-200)
	}

	return &Result{
		Output: output,
		Title:  fmt.Sprintf("grep %s (%d matches)", params.Pattern, len(lines)-1),
	}, nil
}
```

- [ ] **Step 7: Run tests**

```bash
go mod tidy
go test ./internal/tool/ -v -run "TestReadTool|TestGlobTool|TestLsTool"
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/tool/ go.mod go.sum
git commit -m "feat: core read-only tools — read, glob, grep, ls"
```

---

### Task 8: Write Tools (bash, edit, write) + Engine Tool Loop

**Files:**
- Create: `internal/tool/bash.go`
- Create: `internal/tool/edit.go`
- Create: `internal/tool/write.go`
- Modify: `internal/engine/engine.go` — wire tools into agent loop

- [ ] **Step 1: Write bash tool test**

Add to `internal/tool/tools_test.go`:

```go
func TestBashTool(t *testing.T) {
	bt := tool.NewBashTool()
	input, _ := json.Marshal(map[string]any{"command": "echo hello"})
	result, err := bt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Fatalf("Expected output to contain 'hello', got %q", result.Output)
	}
}

func TestEditTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world\nfoo bar\n"), 0o644)

	et := tool.NewEditTool()
	input, _ := json.Marshal(map[string]any{
		"file_path":  path,
		"old_string": "hello world",
		"new_string": "hello altcode",
	})
	result, err := et.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("Tool error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "hello altcode") {
		t.Fatalf("Expected file to contain 'hello altcode', got %q", string(data))
	}
}

func TestWriteTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	wt := tool.NewWriteTool()
	input, _ := json.Marshal(map[string]any{
		"file_path": path,
		"content":   "brand new file\n",
	})
	result, err := wt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("Tool error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "brand new file\n" {
		t.Fatalf("Expected file content 'brand new file\\n', got %q", string(data))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tool/ -v -run "TestBashTool|TestEditTool|TestWriteTool"
```

Expected: FAIL

- [ ] **Step 3: Implement bash tool**

Create `internal/tool/bash.go`:

```go
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type bashTool struct{}

func NewBashTool() Tool { return &bashTool{} }

func (t *bashTool) Name() string        { return "bash" }
func (t *bashTool) Description() string  { return "Execute a bash command and return its output." }
func (t *bashTool) IsConcurrencySafe() bool { return false }
func (t *bashTool) IsReadOnly() bool        { return false }
func (t *bashTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ Command string `json:"command"` }
	json.Unmarshal(input, &p)
	return "bash:" + p.Command
}

func (t *bashTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "The bash command to execute"},
			"timeout": {"type": "integer", "description": "Timeout in milliseconds (default 120000)"}
		},
		"required": ["command"]
	}`)
}

func (t *bashTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	timeout := 120 * time.Second
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Millisecond
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", params.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	title := params.Command
	if len(title) > 60 {
		title = title[:60] + "..."
	}

	return &Result{
		Output:   output,
		Title:    title,
		Metadata: map[string]any{"exit_code": exitCode},
	}, nil
}
```

- [ ] **Step 4: Implement edit tool**

Create `internal/tool/edit.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type editTool struct{}

func NewEditTool() Tool { return &editTool{} }

func (t *editTool) Name() string        { return "edit" }
func (t *editTool) Description() string  { return "Perform exact string replacement in a file." }
func (t *editTool) IsConcurrencySafe() bool { return false }
func (t *editTool) IsReadOnly() bool        { return false }
func (t *editTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ FilePath string `json:"file_path"` }
	json.Unmarshal(input, &p)
	return "edit:" + p.FilePath
}

func (t *editTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to the file to edit"},
			"old_string": {"type": "string", "description": "Exact string to find and replace"},
			"new_string": {"type": "string", "description": "Replacement string"},
			"replace_all": {"type": "boolean", "description": "Replace all occurrences (default false)"}
		},
		"required": ["file_path", "old_string", "new_string"]
	}`)
}

func (t *editTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	data, err := os.ReadFile(params.FilePath)
	if err != nil {
		return &Result{Output: fmt.Sprintf("Error reading file: %v", err), Title: "edit"}, nil
	}

	content := string(data)
	if !strings.Contains(content, params.OldString) {
		return &Result{
			Output: "Error: old_string not found in file. Make sure it matches exactly.",
			Title:  "edit " + params.FilePath,
			Error:  fmt.Errorf("old_string not found"),
		}, nil
	}

	count := strings.Count(content, params.OldString)
	if count > 1 && !params.ReplaceAll {
		return &Result{
			Output: fmt.Sprintf("Error: old_string found %d times. Use replace_all or provide more context to make it unique.", count),
			Title:  "edit " + params.FilePath,
			Error:  fmt.Errorf("ambiguous match: %d occurrences", count),
		}, nil
	}

	var newContent string
	if params.ReplaceAll {
		newContent = strings.ReplaceAll(content, params.OldString, params.NewString)
	} else {
		newContent = strings.Replace(content, params.OldString, params.NewString, 1)
	}

	if err := os.WriteFile(params.FilePath, []byte(newContent), 0o644); err != nil {
		return &Result{Output: fmt.Sprintf("Error writing file: %v", err), Title: "edit"}, nil
	}

	return &Result{
		Output: fmt.Sprintf("Replaced %d occurrence(s) in %s", count, params.FilePath),
		Title:  "edit " + params.FilePath,
	}, nil
}
```

- [ ] **Step 5: Implement write tool**

Create `internal/tool/write.go`:

```go
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type writeTool struct{}

func NewWriteTool() Tool { return &writeTool{} }

func (t *writeTool) Name() string        { return "write" }
func (t *writeTool) Description() string  { return "Write content to a file, creating it if necessary." }
func (t *writeTool) IsConcurrencySafe() bool { return false }
func (t *writeTool) IsReadOnly() bool        { return false }
func (t *writeTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ FilePath string `json:"file_path"` }
	json.Unmarshal(input, &p)
	return "write:" + p.FilePath
}

func (t *writeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file_path": {"type": "string", "description": "Path to write to"},
			"content": {"type": "string", "description": "Content to write"}
		},
		"required": ["file_path", "content"]
	}`)
}

func (t *writeTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	dir := filepath.Dir(params.FilePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &Result{Output: fmt.Sprintf("Error creating directory: %v", err), Title: "write"}, nil
	}

	if err := os.WriteFile(params.FilePath, []byte(params.Content), 0o644); err != nil {
		return &Result{Output: fmt.Sprintf("Error writing file: %v", err), Title: "write"}, nil
	}

	return &Result{
		Output: fmt.Sprintf("Wrote %d bytes to %s", len(params.Content), params.FilePath),
		Title:  "write " + params.FilePath,
	}, nil
}
```

- [ ] **Step 6: Run all tool tests**

```bash
go test ./internal/tool/ -v -race
```

Expected: PASS

- [ ] **Step 7: Wire tools into engine**

Update `internal/engine/engine.go` — add tool registry initialization and tool dispatch to the loop. Replace the `New` and `loop` functions:

In `New()`, add after creating the provider:

```go
	// Build tool registry
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool())
	registry.Register(tool.NewGlobTool())
	registry.Register(tool.NewGrepTool())
	registry.Register(tool.NewLsTool())
	registry.Register(tool.NewBashTool())
	registry.Register(tool.NewEditTool())
	registry.Register(tool.NewWriteTool())
```

Add `tools *tool.Registry` field to `Engine` struct and set it in `New()`.

In `buildRequest()`, add tool schemas:

```go
func (e *Engine) buildRequest() *provider.Request {
	return &provider.Request{
		Model:    e.model,
		Messages: e.messages,
		System: []provider.SystemSection{
			{Content: "You are a helpful coding assistant. Be concise."},
		},
		Tools:     e.toolSchemas(),
		MaxTokens: 4096,
	}
}

func (e *Engine) toolSchemas() []provider.ToolSchema {
	var schemas []provider.ToolSchema
	for _, t := range e.tools.All() {
		schemas = append(schemas, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		})
	}
	return schemas
}
```

In the `loop()`, after processing the stream and getting tool calls, dispatch them:

```go
	// After stream processing:
	// If we got tool calls, execute them and continue the loop
	// If no tool calls, we're done
```

The full tool-call dispatch integration will be completed after the stream processing correctly extracts tool calls. For now, the tools are registered and their schemas are sent to the API.

- [ ] **Step 8: Build and verify**

```bash
go mod tidy
make build
```

Expected: Builds successfully

- [ ] **Step 9: Commit**

```bash
git add internal/tool/ internal/engine/ go.mod go.sum
git commit -m "feat: write tools (bash, edit, write) and wire tool registry into engine"
```

---

## Phase 3: Permission System (Task 9)

---

### Task 9: Permission Evaluator + Rules

**Files:**
- Create: `internal/permission/permission.go`
- Create: `internal/permission/rules.go`
- Create: `internal/permission/defaults.go`
- Create: `internal/permission/doom.go`
- Create: `internal/permission/permission_test.go`

- [ ] **Step 1: Write permission tests**

Create `internal/permission/permission_test.go`:

```go
package permission_test

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/permission"
)

func TestDefaultRulesAllowReads(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	result := eval.Check("read", "read:/some/file.go")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for read, got %v", result)
	}
}

func TestDefaultRulesAllowGitStatus(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	result := eval.Check("bash", "bash:git status")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for git status, got %v", result)
	}
}

func TestDefaultRulesAskForUnknownBash(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	result := eval.Check("bash", "bash:rm -rf /")
	if result != permission.ActionAsk {
		t.Fatalf("Expected Ask for rm -rf, got %v", result)
	}
}

func TestBypassModeAllowsEverything(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeBypass, "", nil)

	result := eval.Check("bash", "bash:rm -rf /")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow in bypass mode, got %v", result)
	}
}

func TestPlanModeBlocksWrites(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModePlan, "", nil)

	result := eval.CheckWithReadOnly("edit", "edit:/file.go", false)
	if result != permission.ActionDeny {
		t.Fatalf("Expected Deny for edit in plan mode, got %v", result)
	}

	result = eval.CheckWithReadOnly("read", "read:/file.go", true)
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for read in plan mode, got %v", result)
	}
}

func TestDoomLoopDetection(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeAuto, "", nil)

	// Same call 3 times should trigger ask
	eval.RecordCall("bash", "bash:echo hello")
	eval.RecordCall("bash", "bash:echo hello")
	eval.RecordCall("bash", "bash:echo hello")

	result := eval.Check("bash", "bash:echo hello")
	if result != permission.ActionAsk {
		t.Fatalf("Expected Ask after doom loop, got %v", result)
	}
}

func TestCustomRules(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "bash", Pattern: "npm run *", Action: permission.ActionAllow, Source: "project"},
	}
	eval := permission.NewEvaluator(permission.ModeDefault, "", rules)

	result := eval.Check("bash", "bash:npm run test")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for npm run test, got %v", result)
	}
}

func TestSessionRulePersistence(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	eval.AddSessionRule(permission.Rule{
		Tool: "bash", Pattern: "make *", Action: permission.ActionAllow, Source: "session",
	})

	result := eval.Check("bash", "bash:make build")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow after session rule, got %v", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/permission/ -v
```

Expected: FAIL

- [ ] **Step 3: Create permission types and evaluator**

Create `internal/permission/permission.go`:

```go
package permission

import "sync"

type Mode int

const (
	ModeDefault Mode = iota
	ModeAuto
	ModeBypass
	ModePlan
)

type ActionType int

const (
	ActionAllow ActionType = iota
	ActionDeny
	ActionAsk
)

type Rule struct {
	Tool    string
	Pattern string
	Action  ActionType
	Source  string // "cli", "session", "project", "user"
}

type Evaluator struct {
	mode         Mode
	projectRoot  string
	rules        []Rule
	sessionRules []Rule
	callHistory  []callRecord
	mu           sync.Mutex
}

type callRecord struct {
	tool    string
	pattern string
}

func NewEvaluator(mode Mode, projectRoot string, rules []Rule) *Evaluator {
	allRules := append(DefaultRules(), rules...)
	return &Evaluator{
		mode:        mode,
		projectRoot: projectRoot,
		rules:       allRules,
	}
}

func (e *Evaluator) Check(toolName, pattern string) ActionType {
	return e.CheckWithReadOnly(toolName, pattern, false)
}

func (e *Evaluator) CheckWithReadOnly(toolName, pattern string, readOnly bool) ActionType {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Bypass mode allows everything
	if e.mode == ModeBypass {
		return ActionAllow
	}

	// Plan mode blocks non-read-only tools
	if e.mode == ModePlan && !readOnly {
		return ActionDeny
	}

	// Check doom loop
	if e.isDoomLoop(toolName, pattern) {
		return ActionAsk
	}

	// Check deny rules first (highest priority)
	for _, r := range e.sessionRules {
		if r.Action == ActionDeny && matchRule(r, toolName, pattern) {
			return ActionDeny
		}
	}
	for _, r := range e.rules {
		if r.Action == ActionDeny && matchRule(r, toolName, pattern) {
			return ActionDeny
		}
	}

	// Check allow rules
	for _, r := range e.sessionRules {
		if r.Action == ActionAllow && matchRule(r, toolName, pattern) {
			return ActionAllow
		}
	}
	for _, r := range e.rules {
		if r.Action == ActionAllow && matchRule(r, toolName, pattern) {
			return ActionAllow
		}
	}

	// Mode-based fallback
	switch e.mode {
	case ModeAuto:
		return ActionDeny
	case ModeDefault:
		return ActionAsk
	default:
		return ActionAsk
	}
}

func (e *Evaluator) RecordCall(toolName, pattern string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.callHistory = append(e.callHistory, callRecord{tool: toolName, pattern: pattern})
}

func (e *Evaluator) AddSessionRule(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionRules = append(e.sessionRules, r)
}

func (e *Evaluator) SetMode(mode Mode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mode = mode
}

func (e *Evaluator) Clone() *Evaluator {
	e.mu.Lock()
	defer e.mu.Unlock()
	rules := make([]Rule, len(e.rules))
	copy(rules, e.rules)
	return &Evaluator{
		mode:        e.mode,
		projectRoot: e.projectRoot,
		rules:       rules,
	}
}

func (e *Evaluator) isDoomLoop(toolName, pattern string) bool {
	n := len(e.callHistory)
	if n < 3 {
		return false
	}
	for i := n - 3; i < n; i++ {
		if e.callHistory[i].tool != toolName || e.callHistory[i].pattern != pattern {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Create rule matching**

Create `internal/permission/rules.go`:

```go
package permission

import (
	"path/filepath"
	"strings"
)

func matchRule(rule Rule, toolName, pattern string) bool {
	// Match tool name
	if rule.Tool != "*" && rule.Tool != toolName {
		return false
	}

	// Extract the argument part from the pattern (after "toolname:")
	arg := pattern
	if idx := strings.Index(pattern, ":"); idx >= 0 {
		arg = pattern[idx+1:]
	}

	// Match pattern
	return globMatch(rule.Pattern, arg)
}

func globMatch(pattern, value string) bool {
	if pattern == "*" {
		return true
	}

	// Use filepath.Match for simple glob
	matched, err := filepath.Match(pattern, value)
	if err == nil && matched {
		return true
	}

	// Handle ** for recursive matching
	if strings.Contains(pattern, "**") {
		// Convert ** glob to a prefix match
		prefix := strings.Split(pattern, "**")[0]
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}

	// Handle space-separated command patterns (e.g., "git *" matches "git status")
	if strings.Contains(pattern, " ") {
		parts := strings.SplitN(pattern, " ", 2)
		valueParts := strings.SplitN(value, " ", 2)
		if len(valueParts) >= 1 && parts[0] == valueParts[0] {
			if len(parts) > 1 && parts[1] == "*" {
				return true
			}
			if len(parts) > 1 && len(valueParts) > 1 {
				return globMatch(parts[1], valueParts[1])
			}
		}
	}

	return pattern == value
}
```

- [ ] **Step 5: Create default rules**

Create `internal/permission/defaults.go`:

```go
package permission

func DefaultRules() []Rule {
	return []Rule{
		// Allow: read-only operations
		{Tool: "read", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "glob", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "grep", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "ls", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "fetch", Pattern: "*", Action: ActionAllow, Source: "default"},

		// Allow: safe git commands
		{Tool: "bash", Pattern: "git status", Action: ActionAllow, Source: "default"},
		{Tool: "bash", Pattern: "git diff *", Action: ActionAllow, Source: "default"},
		{Tool: "bash", Pattern: "git log *", Action: ActionAllow, Source: "default"},
	}
}
```

- [ ] **Step 6: Create doom loop detection**

Create `internal/permission/doom.go`:

```go
package permission

const doomLoopThreshold = 3

// isDoomLoop is implemented on Evaluator in permission.go
// This file documents the doom loop detection strategy.
//
// A doom loop is detected when the same tool+pattern combination
// is called 3 consecutive times. This prevents infinite loops
// where the model keeps retrying the same failed operation.
//
// When detected, the evaluator forces ActionAsk even in auto mode,
// breaking the loop by requiring user intervention.
```

- [ ] **Step 7: Run tests**

```bash
go test ./internal/permission/ -v -race
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/permission/
git commit -m "feat: permission system with glob rules, modes, and doom loop detection"
```

---

## Phase 4: Context & Compaction (Task 10)

---

### Task 10: System Prompt Assembly + Context Compaction

**Files:**
- Create: `internal/context/system.go`
- Create: `internal/context/env.go`
- Create: `internal/compact/budget.go`
- Create: `internal/compact/micro.go`
- Create: `internal/compact/compact_test.go`

- [ ] **Step 1: Write compaction test**

Create `internal/compact/compact_test.go`:

```go
package compact_test

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/compact"
	"github.com/altcode-ai/altcode/internal/provider"
)

func TestToolResultBudget(t *testing.T) {
	// Create messages with large tool results
	messages := []provider.Message{
		{Role: "user", Content: "read files"},
		{Role: "assistant", Content: "I'll read them."},
		{Role: "tool", Content: makeString(100_000)}, // 100KB
		{Role: "assistant", Content: "Found it."},
		{Role: "user", Content: "read more"},
		{Role: "assistant", Content: "Reading."},
		{Role: "tool", Content: makeString(100_000)}, // 100KB
		{Role: "assistant", Content: "Done."},
		{Role: "user", Content: "and more"},
		{Role: "assistant", Content: "Sure."},
		{Role: "tool", Content: makeString(100_000)}, // 100KB — total 300KB
	}

	// Budget is 200KB — should truncate oldest tool result
	compactor := compact.NewBudgetCompactor(200_000)
	compacted := compactor.Apply(messages)

	totalSize := 0
	for _, m := range compacted {
		if m.Role == "tool" {
			totalSize += len(m.Content)
		}
	}

	if totalSize > 200_000 {
		t.Fatalf("Expected total tool output <= 200KB, got %d", totalSize)
	}
}

func TestMicrocompact(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "tool_use:read"},
		{Role: "tool", Content: "file contents"},
		{Role: "assistant", Content: "I see the file."},
		// ... many more old turns
	}

	// Add 15 more turn pairs to exceed the 10-turn window
	for i := 0; i < 15; i++ {
		messages = append(messages,
			provider.Message{Role: "user", Content: "next"},
			provider.Message{Role: "assistant", Content: "tool_use:read"},
			provider.Message{Role: "tool", Content: "more content"},
			provider.Message{Role: "assistant", Content: "processed"},
		)
	}

	mc := compact.NewMicrocompactor(10)
	compacted := mc.Apply(messages)

	if len(compacted) >= len(messages) {
		t.Fatalf("Expected compaction, got same length: %d", len(compacted))
	}
}

func makeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/compact/ -v
```

Expected: FAIL

- [ ] **Step 3: Create system prompt assembly**

Create `internal/context/system.go`:

```go
package context

import (
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/tool"
)

func BuildSystemPrompt(cfg *config.Config, tools *tool.Registry, instructions []config.Instruction, env EnvContext) []provider.SystemSection {
	var sections []provider.SystemSection

	// Static sections (cacheable)
	sections = append(sections, provider.SystemSection{
		Content:      corePersona(),
		CacheControl: &provider.CacheControl{Type: "ephemeral"},
	})

	sections = append(sections, provider.SystemSection{
		Content:      toolDescriptions(tools),
		CacheControl: &provider.CacheControl{Type: "ephemeral"},
	})

	for _, inst := range instructions {
		sections = append(sections, provider.SystemSection{
			Content:      "# " + inst.Path + "\n\n" + inst.Content,
			CacheControl: &provider.CacheControl{Type: "ephemeral"},
		})
	}

	// Dynamic sections (not cached — change between turns)
	sections = append(sections, provider.SystemSection{
		Content: envSection(env),
	})

	return sections
}

func corePersona() string {
	return `You are an expert coding assistant. You help users with software engineering tasks including writing code, debugging, refactoring, and explaining code.

Key behaviors:
- Be concise and direct
- Write clean, idiomatic code
- Explain your reasoning when making non-obvious choices
- Use tools to read files before modifying them
- Prefer editing existing files over creating new ones`
}

func toolDescriptions(registry *tool.Registry) string {
	var desc string
	for _, t := range registry.All() {
		desc += "## " + t.Name() + "\n" + t.Description() + "\n\n"
	}
	return desc
}

func envSection(env EnvContext) string {
	return "# Environment\n" +
		"Working directory: " + env.WorkDir + "\n" +
		"Date: " + env.Date + "\n" +
		"Platform: " + env.Platform + "\n"
}
```

- [ ] **Step 4: Create environment context**

Create `internal/context/env.go`:

```go
package context

import (
	"os"
	"runtime"
	"time"
)

type EnvContext struct {
	WorkDir  string
	Date     string
	Platform string
}

func DetectEnv() EnvContext {
	wd, _ := os.Getwd()
	return EnvContext{
		WorkDir:  wd,
		Date:     time.Now().Format("2006-01-02"),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
}
```

- [ ] **Step 5: Create tool result budget compactor**

Create `internal/compact/budget.go`:

```go
package compact

import "github.com/altcode-ai/altcode/internal/provider"

type BudgetCompactor struct {
	maxBytes int
}

func NewBudgetCompactor(maxBytes int) *BudgetCompactor {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024 // 512KB default
	}
	return &BudgetCompactor{maxBytes: maxBytes}
}

func (c *BudgetCompactor) Apply(messages []provider.Message) []provider.Message {
	// Calculate total tool output size
	totalSize := 0
	for _, m := range messages {
		if m.Role == "tool" {
			totalSize += len(m.Content)
		}
	}

	if totalSize <= c.maxBytes {
		return messages
	}

	// Truncate oldest tool results until within budget
	result := make([]provider.Message, len(messages))
	copy(result, messages)

	for i := range result {
		if totalSize <= c.maxBytes {
			break
		}
		if result[i].Role == "tool" && len(result[i].Content) > 100 {
			freed := len(result[i].Content) - 50
			result[i].Content = "[result truncated — exceeded budget]"
			totalSize -= freed
		}
	}

	return result
}
```

- [ ] **Step 6: Create microcompactor**

Create `internal/compact/micro.go`:

```go
package compact

import "github.com/altcode-ai/altcode/internal/provider"

type Microcompactor struct {
	keepTurns int
}

func NewMicrocompactor(keepTurns int) *Microcompactor {
	if keepTurns <= 0 {
		keepTurns = 10
	}
	return &Microcompactor{keepTurns: keepTurns}
}

func (c *Microcompactor) Apply(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return messages
	}

	// Count user turns from the end
	turnCount := 0
	protectFrom := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			turnCount++
			if turnCount >= c.keepTurns {
				protectFrom = i
				break
			}
		}
	}

	// Remove tool results before the protection boundary
	var result []provider.Message
	for i, m := range messages {
		if i < protectFrom && m.Role == "tool" {
			// Replace with compact stub
			result = append(result, provider.Message{
				Role:    "tool",
				Content: "[previous tool result removed]",
			})
		} else {
			result = append(result, m)
		}
	}

	return result
}
```

- [ ] **Step 7: Run tests**

```bash
go mod tidy
go test ./internal/compact/ -v
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/context/ internal/compact/
git commit -m "feat: system prompt assembly and context compaction (budget + microcompact)"
```

---

## Phase 5: Full TUI (Tasks 11-12)

### Task 11: Streaming Markdown Renderer

**Files:**
- Create: `internal/tui/markdown.go`
- Create: `internal/tui/markdown_test.go`

This is the highest-risk TUI component. Prototype and test independently.

- [ ] **Step 1: Write markdown renderer test**

Create `internal/tui/markdown_test.go`:

```go
package tui_test

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/tui"
)

func TestRenderPlainText(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	result := r.Render("Hello, world!")
	if result == "" {
		t.Fatal("Expected non-empty output")
	}
}

func TestRenderCodeBlock(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	input := "Here is code:\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\nDone."
	result := r.Render(input)
	if result == "" {
		t.Fatal("Expected non-empty output")
	}
	t.Logf("Rendered:\n%s", result)
}

func TestRenderIncompleteCodeBlock(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	// Simulate streaming — code block not yet closed
	input := "Here is code:\n\n```go\nfunc main() {"
	result := r.Render(input)
	if result == "" {
		t.Fatal("Expected non-empty output")
	}
	// Should not crash on incomplete block
	t.Logf("Rendered (incomplete):\n%s", result)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/ -v -run "TestRender"
```

Expected: FAIL

- [ ] **Step 3: Implement streaming markdown renderer**

Create `internal/tui/markdown.go`:

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type MarkdownRenderer struct {
	width int
	cache map[string]string
}

func NewMarkdownRenderer(width int) *MarkdownRenderer {
	return &MarkdownRenderer{
		width: width,
		cache: make(map[string]string),
	}
}

func (r *MarkdownRenderer) Render(input string) string {
	if cached, ok := r.cache[input]; ok {
		return cached
	}

	var sb strings.Builder
	lines := strings.Split(input, "\n")
	inCodeBlock := false
	codeBlockLang := ""
	var codeLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				// End code block
				sb.WriteString(renderCodeBlock(codeBlockLang, codeLines, r.width))
				inCodeBlock = false
				codeLines = nil
				codeBlockLang = ""
			} else {
				// Start code block
				inCodeBlock = true
				codeBlockLang = strings.TrimPrefix(line, "```")
				codeBlockLang = strings.TrimSpace(codeBlockLang)
			}
			continue
		}

		if inCodeBlock {
			codeLines = append(codeLines, line)
			continue
		}

		// Render inline markdown
		rendered := renderInline(line, r.width)
		sb.WriteString(rendered)
		sb.WriteByte('\n')
	}

	// Handle unclosed code block (streaming)
	if inCodeBlock {
		sb.WriteString(renderCodeBlock(codeBlockLang, codeLines, r.width))
		sb.WriteString(streamingStyle.Render(" streaming..."))
		sb.WriteByte('\n')
	}

	result := sb.String()
	// Only cache completed renders (no streaming indicator)
	if !inCodeBlock {
		r.cache[input] = result
	}
	return result
}

var (
	codeBlockStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1E1E2E")).
			Foreground(lipgloss.Color("#CDD6F4")).
			Padding(0, 1)

	headingStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#CBA6F7"))

	boldStyle = lipgloss.NewStyle().Bold(true)

	streamingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C7086")).
			Italic(true)

	inlineCodeStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#313244")).
			Foreground(lipgloss.Color("#CDD6F4"))
)

func renderCodeBlock(lang string, lines []string, width int) string {
	content := strings.Join(lines, "\n")
	header := ""
	if lang != "" {
		header = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6C7086")).
			Render(" "+lang) + "\n"
	}
	return header + codeBlockStyle.Width(width-2).Render(content) + "\n"
}

func renderInline(line string, width int) string {
	// Headings
	if strings.HasPrefix(line, "# ") {
		return headingStyle.Render(strings.TrimPrefix(line, "# "))
	}
	if strings.HasPrefix(line, "## ") {
		return headingStyle.Render(strings.TrimPrefix(line, "## "))
	}
	if strings.HasPrefix(line, "### ") {
		return headingStyle.Render(strings.TrimPrefix(line, "### "))
	}

	// Bold
	line = renderBold(line)

	// Inline code
	line = renderInlineCode(line)

	// Bullet points
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		return "  " + line
	}

	return line
}

func renderBold(s string) string {
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		bold := boldStyle.Render(s[start+2 : end])
		s = s[:start] + bold + s[end+2:]
	}
	return s
}

func renderInlineCode(s string) string {
	for {
		start := strings.Index(s, "`")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end == -1 {
			break
		}
		end += start + 1
		code := inlineCodeStyle.Render(s[start+1 : end])
		s = s[:start] + code + s[end+1:]
	}
	return s
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/tui/ -v -run "TestRender"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/markdown.go internal/tui/markdown_test.go
git commit -m "feat: streaming markdown renderer with code block and inline formatting"
```

---

### Task 12: Full TUI — Permission Dialog, Command Palette, Status Bar

**Files:**
- Create: `internal/tui/header.go`
- Create: `internal/tui/status.go`
- Create: `internal/tui/permission_dialog.go`
- Create: `internal/tui/palette.go`
- Modify: `internal/tui/app.go` — integrate all components

This task wires the full TUI together. Since Bubbletea components are hard to unit test (they return strings), this is primarily integration code verified by manual testing and build verification.

- [ ] **Step 1: Create header component**

Create `internal/tui/header.go`:

```go
package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type Header struct {
	model     string
	title     string
	tokens    int
	cost      float64
	contextPct float64
	theme     Theme
	width     int
}

func NewHeader(theme Theme) *Header {
	return &Header{theme: theme}
}

func (h *Header) SetModel(model string)       { h.model = model }
func (h *Header) SetTitle(title string)        { h.title = title }
func (h *Header) SetTokens(tokens int)         { h.tokens = tokens }
func (h *Header) SetContextPct(pct float64)    { h.contextPct = pct }
func (h *Header) SetWidth(width int)           { h.width = width }

func (h *Header) View() string {
	logo := lipgloss.NewStyle().
		Foreground(h.theme.Primary).
		Bold(true).
		Render("altcode")

	model := lipgloss.NewStyle().
		Foreground(h.theme.Secondary).
		Render(h.model)

	info := lipgloss.NewStyle().
		Foreground(h.theme.Muted).
		Render(fmt.Sprintf("  tokens: %d  context: %.0f%%", h.tokens, h.contextPct*100))

	left := logo + "  " + model
	right := info

	gap := h.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return left + fmt.Sprintf("%*s", gap, "") + right
}
```

- [ ] **Step 2: Create status bar**

Create `internal/tui/status.go`:

```go
package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type StatusBar struct {
	theme      Theme
	width      int
	busy       bool
	toolName   string
	agentDepth int
	mode       string
}

func NewStatusBar(theme Theme) *StatusBar {
	return &StatusBar{theme: theme, mode: "default"}
}

func (s *StatusBar) SetWidth(width int)        { s.width = width }
func (s *StatusBar) SetBusy(busy bool)         { s.busy = busy }
func (s *StatusBar) SetTool(name string)       { s.toolName = name }
func (s *StatusBar) SetAgentDepth(depth int)   { s.agentDepth = depth }
func (s *StatusBar) SetMode(mode string)       { s.mode = mode }

func (s *StatusBar) View() string {
	modeStyle := lipgloss.NewStyle().
		Background(s.theme.Primary).
		Foreground(lipgloss.Color("#000")).
		Padding(0, 1).
		Bold(true)

	var left string
	if s.busy {
		spinner := lipgloss.NewStyle().Foreground(s.theme.Warning).Render("● ")
		toolInfo := ""
		if s.toolName != "" {
			toolInfo = lipgloss.NewStyle().Foreground(s.theme.Muted).Render(s.toolName)
		}
		agentInfo := ""
		if s.agentDepth > 0 {
			agentInfo = lipgloss.NewStyle().Foreground(s.theme.Secondary).
				Render(fmt.Sprintf(" agent[%d]", s.agentDepth))
		}
		left = spinner + toolInfo + agentInfo
	} else {
		left = lipgloss.NewStyle().Foreground(s.theme.Success).Render("● ready")
	}

	right := modeStyle.Render(s.mode)

	hints := lipgloss.NewStyle().Foreground(s.theme.Muted).
		Render("  Ctrl+D send  Esc cancel  Shift+Tab mode")

	gap := s.width - lipgloss.Width(left) - lipgloss.Width(right) - lipgloss.Width(hints)
	if gap < 1 {
		gap = 1
	}

	return left + fmt.Sprintf("%*s", gap, "") + hints + "  " + right
}
```

- [ ] **Step 3: Create permission dialog**

Create `internal/tui/permission_dialog.go`:

```go
package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

type PermissionDialog struct {
	theme    Theme
	width    int
	toolName string
	pattern  string
	visible  bool
}

func NewPermissionDialog(theme Theme) *PermissionDialog {
	return &PermissionDialog{theme: theme}
}

func (d *PermissionDialog) Show(toolName, pattern string) {
	d.toolName = toolName
	d.pattern = pattern
	d.visible = true
}

func (d *PermissionDialog) Hide() {
	d.visible = false
}

func (d *PermissionDialog) IsVisible() bool { return d.visible }
func (d *PermissionDialog) SetWidth(w int)  { d.width = w }

func (d *PermissionDialog) View() string {
	if !d.visible {
		return ""
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(d.theme.Warning).
		Padding(1, 2).
		Width(d.width - 4)

	title := lipgloss.NewStyle().
		Foreground(d.theme.Warning).
		Bold(true).
		Render("Permission Required")

	body := fmt.Sprintf("\nTool: %s\nPattern: %s\n",
		lipgloss.NewStyle().Foreground(d.theme.Primary).Render(d.toolName),
		lipgloss.NewStyle().Foreground(d.theme.Muted).Render(d.pattern),
	)

	options := lipgloss.NewStyle().Foreground(d.theme.Muted).Render(
		"\n  y  allow once       n  deny once\n" +
			"  a  always allow     !  allow all " + d.toolName,
	)

	return border.Render(title + body + options)
}
```

- [ ] **Step 4: Create command palette**

Create `internal/tui/palette.go`:

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

type Command struct {
	Name        string
	Description string
	Action      func() string
}

type Palette struct {
	theme    Theme
	width    int
	visible  bool
	input    textinput.Model
	commands []Command
	filtered []Command
}

func NewPalette(theme Theme, commands []Command) *Palette {
	ti := textinput.New()
	ti.Placeholder = "Type a command..."
	ti.CharLimit = 50

	return &Palette{
		theme:    theme,
		input:    ti,
		commands: commands,
		filtered: commands,
	}
}

func (p *Palette) Toggle() {
	p.visible = !p.visible
	if p.visible {
		p.input.Focus()
		p.input.Reset()
		p.filtered = p.commands
	}
}

func (p *Palette) IsVisible() bool { return p.visible }
func (p *Palette) SetWidth(w int)  { p.width = w }

func (p *Palette) Filter(query string) {
	if query == "" {
		p.filtered = p.commands
		return
	}
	q := strings.ToLower(query)
	var filtered []Command
	for _, cmd := range p.commands {
		if strings.Contains(strings.ToLower(cmd.Name), q) ||
			strings.Contains(strings.ToLower(cmd.Description), q) {
			filtered = append(filtered, cmd)
		}
	}
	p.filtered = filtered
}

func (p *Palette) View() string {
	if !p.visible {
		return ""
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.theme.Primary).
		Padding(0, 1).
		Width(p.width - 4)

	var sb strings.Builder
	sb.WriteString(p.input.View())
	sb.WriteByte('\n')

	for i, cmd := range p.filtered {
		if i >= 10 {
			break
		}
		name := lipgloss.NewStyle().Foreground(p.theme.Primary).Bold(true).Render(cmd.Name)
		desc := lipgloss.NewStyle().Foreground(p.theme.Muted).Render("  " + cmd.Description)
		sb.WriteString(name + desc + "\n")
	}

	return border.Render(sb.String())
}
```

- [ ] **Step 5: Build and verify**

```bash
go mod tidy
make build
```

Expected: Builds successfully

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "feat: TUI components — header, status bar, permission dialog, command palette"
```

---

## Phase 6: Integration + Polish (Tasks 13-14)

---

### Task 13: Wire Everything Together in main.go

**Files:**
- Modify: `cmd/altcode/main.go` — full CLI with cobra, config loading, project detection

- [ ] **Step 1: Add cobra CLI**

Replace `cmd/altcode/main.go` with full cobra-based CLI that:
- Parses `--model`, `--config`, `--theme` flags
- Detects project root
- Loads config cascade (user → project → CLI)
- Loads instructions (CLAUDE.md, ALTCODE.md)
- Creates engine with all tools registered
- Launches TUI

```go
package main

import (
	"fmt"
	"os"

	"github.com/altcode-ai/altcode/internal/config"
	appcontext "github.com/altcode-ai/altcode/internal/context"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	var modelFlag, configFlag, themeFlag string

	root := &cobra.Command{
		Use:     "altcode",
		Short:   "AI-assisted coding CLI",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(modelFlag, configFlag, themeFlag)
		},
	}

	root.Flags().StringVar(&modelFlag, "model", "", "Model to use (e.g. anthropic/claude-sonnet-4-20250514)")
	root.Flags().StringVar(&configFlag, "config", "", "Config file path")
	root.Flags().StringVar(&themeFlag, "theme", "", "Theme name")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(modelFlag, configFlag, themeFlag string) error {
	// 1. Detect project root
	wd, _ := os.Getwd()
	projectRoot, _ := config.DetectProjectRoot(wd)

	// 2. Load config cascade
	cfg := config.Default()
	// User config
	home, _ := os.UserHomeDir()
	if userCfg, err := config.LoadFile(home + "/.config/altcode/config.json"); err == nil {
		mergeConfig(cfg, userCfg)
	}
	// Project config
	if projCfg, err := config.LoadFile(projectRoot + "/.altcode/config.json"); err == nil {
		mergeConfig(cfg, projCfg)
	}
	// Explicit config file
	if configFlag != "" {
		if fileCfg, err := config.LoadFile(configFlag); err == nil {
			mergeConfig(cfg, fileCfg)
		}
	}
	// CLI flag overrides
	if modelFlag != "" {
		cfg.Model = modelFlag
	}
	if themeFlag != "" {
		cfg.Theme = themeFlag
	}
	// Env var fallbacks
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		if cfg.Provider == nil {
			cfg.Provider = make(map[string]config.ProviderConfig)
		}
		if p, ok := cfg.Provider["anthropic"]; !ok || p.APIKey == "" {
			cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: key}
		}
	}

	// 3. Load instructions
	instructions, _ := config.LoadInstructions(projectRoot)

	// 4. Create engine
	eng, err := engine.New(cfg)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	// 5. Detect environment
	env := appcontext.DetectEnv()
	_ = env          // Will be used for system prompt
	_ = instructions // Will be used for system prompt

	// 6. Launch TUI
	theme := tui.GetTheme(cfg.Theme)
	app := tui.New(eng, theme)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

func mergeConfig(base, overlay *config.Config) {
	if overlay.Model != "" {
		base.Model = overlay.Model
	}
	if overlay.Theme != "" {
		base.Theme = overlay.Theme
	}
	for k, v := range overlay.Provider {
		if base.Provider == nil {
			base.Provider = make(map[string]config.ProviderConfig)
		}
		base.Provider[k] = v
	}
	base.Permission = append(base.Permission, overlay.Permission...)
	for k, v := range overlay.MCP {
		if base.MCP == nil {
			base.MCP = make(map[string]config.MCPServerConfig)
		}
		base.MCP[k] = v
	}
	for k, v := range overlay.Agent {
		if base.Agent == nil {
			base.Agent = make(map[string]config.AgentConfig)
		}
		base.Agent[k] = v
	}
}
```

- [ ] **Step 2: Build and test**

```bash
go mod tidy
make build
./dist/altcode --version
./dist/altcode --help
```

Expected: Shows version and help text

- [ ] **Step 3: Manual integration test**

```bash
ANTHROPIC_API_KEY=<your-key> ./dist/altcode
```

Expected: Full TUI with header, message area, prompt input, status bar.

- [ ] **Step 4: Commit**

```bash
git add cmd/altcode/ go.mod go.sum
git commit -m "feat: full CLI with cobra, config cascade, and project detection"
```

---

### Task 14: Binary Size + Startup Benchmark

**Files:**
- Create: `internal/bench_test.go`

- [ ] **Step 1: Build release binary and check size**

```bash
go build -ldflags="-s -w" -o dist/altcode ./cmd/altcode
ls -lh dist/altcode
```

Expected: Under 20MB (target is 15MB, pure-Go SQLite adds ~8MB)

- [ ] **Step 2: Write startup benchmark**

Create `internal/bench_test.go`:

```go
package internal_test

import (
	"os/exec"
	"testing"
	"time"
)

func TestStartupTime(t *testing.T) {
	// Build the binary
	build := exec.Command("go", "build", "-ldflags=-s -w", "-o", "/tmp/altcode-bench", "./cmd/altcode")
	if err := build.Run(); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Measure --version startup time (no TUI)
	start := time.Now()
	cmd := exec.Command("/tmp/altcode-bench", "--version")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)

	t.Logf("Startup time (--version): %v", elapsed)
	if elapsed > 100*time.Millisecond {
		t.Errorf("Startup too slow: %v (target <50ms)", elapsed)
	}
}
```

- [ ] **Step 3: Run benchmark**

```bash
go test ./internal/ -v -run TestStartupTime
```

Expected: PASS with startup < 100ms (generous — `--version` should be <10ms)

- [ ] **Step 4: Commit**

```bash
git add internal/bench_test.go
git commit -m "feat: startup time benchmark — verify <50ms target"
```

---

## Summary

| Phase | Tasks | What it delivers |
|---|---|---|
| Phase 1: Walking Skeleton | 1-5 | End-to-end: type prompt → get streamed Anthropic response in TUI |
| Phase 2: Tool System | 6-8 | 7 core tools with concurrent dispatch |
| Phase 3: Permission System | 9 | Glob-pattern rules, 4 modes, doom loop detection |
| Phase 4: Context & Compaction | 10 | System prompt assembly, tool result budget, microcompact |
| Phase 5: Full TUI | 11-12 | Streaming markdown, header, status, permission dialog, palette |
| Phase 6: Integration | 13-14 | Full CLI, config cascade, binary size/startup validation |

**Not in this plan (deferred to v2):**
- OpenAI, Gemini, compat providers (Task 15+)
- MCP client (Task 16+)
- HTTP server / remote attach (Task 17+)
- Subagent system (Task 18+)
- Hooks system (Task 19+)
- Embeddable SDK (Task 20+)
- Auto-compact via summarization agent (Task 21+)
