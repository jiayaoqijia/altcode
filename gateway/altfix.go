package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AltFixBridge translates channel messages into altcode daemon API calls.
type AltFixBridge struct {
	daemonURL   string
	authToken   string
	repoURL     string // default repo for /fix commands
	client      *http.Client
	manager     *Manager
	rateLimiter *RateLimiter
}

// BridgeConfig configures the AltFix bridge.
type BridgeConfig struct {
	DaemonURL string
	AuthToken string
	RepoURL   string // default repo URL for tasks
}

// NewAltFixBridge creates a bridge between channels and the daemon.
func NewAltFixBridge(cfg BridgeConfig, mgr *Manager) *AltFixBridge {
	return &AltFixBridge{
		daemonURL: strings.TrimRight(cfg.DaemonURL, "/"),
		authToken: cfg.AuthToken,
		repoURL:   cfg.RepoURL,
		client:    &http.Client{Timeout: 30 * time.Second},
		manager:   mgr,
		rateLimiter: NewRateLimiter(RateLimitConfig{
			MaxAttempts:    5,
			WindowSeconds:  60,
			LockoutSeconds: 60,
		}),
	}
}

// HandleMessage is the MessageHandler callback for channels.
// It parses commands and calls the daemon API accordingly.
func (b *AltFixBridge) HandleMessage(
	ctx context.Context, msg InboundMessage,
) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Rate limit per sender.
	if b.rateLimiter != nil && !b.rateLimiter.Allow("msg", msg.SenderID) {
		b.reply(ctx, msg, "Rate limited. Please wait before sending more commands.")
		return
	}

	var reply string
	var err error

	switch {
	case strings.HasPrefix(text, "/fix "):
		desc := strings.TrimSpace(strings.TrimPrefix(text, "/fix "))
		reply, err = b.createTask(ctx, desc)
	case text == "/status":
		reply, err = b.listTasks(ctx)
	case strings.HasPrefix(text, "/stop "):
		id := strings.TrimSpace(strings.TrimPrefix(text, "/stop "))
		reply, err = b.stopTask(ctx, id)
	case strings.HasPrefix(text, "/steer "):
		reply, err = b.steerTask(ctx, text)
	case text == "/cost":
		reply, err = b.showCost(ctx)
	case text == "/help" || text == "/start":
		reply = helpText()
	default:
		// Not a recognized command; treat as a /fix
		reply, err = b.createTask(ctx, text)
	}

	if err != nil {
		log.Printf("gateway error: %v", err)
		reply = "Error: " + sanitizeError(err)
	}

	if reply != "" {
		b.reply(ctx, msg, reply)
	}
}

func (b *AltFixBridge) reply(
	ctx context.Context, msg InboundMessage, text string,
) {
	_ = b.manager.Send(ctx, OutboundMessage{
		Channel: msg.ChannelName,
		ChatID:  msg.ChatID,
		Text:    text,
		ReplyTo: msg.MessageID,
	})
}

// sanitizeError strips internal details (URLs, paths, response bodies)
// from daemon errors before they reach chat users.
func sanitizeError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "daemon returned") {
		// Extract just the status code portion, e.g. "daemon returned 404"
		if idx := strings.Index(msg, ":"); idx > 0 {
			return msg[:idx]
		}
	}
	return "internal error"
}

func (b *AltFixBridge) createTask(
	ctx context.Context, description string,
) (string, error) {
	body := map[string]string{
		"repo_url": b.repoURL,
		"task":     description,
	}
	data, _ := json.Marshal(body)

	resp, err := b.doRequest(ctx, "POST", "/tasks", data)
	if err != nil {
		return "", err
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return fmt.Sprintf(
		"Task created: %s\nStatus: %s\nTrack with /status",
		result.ID, result.Status,
	), nil
}

func (b *AltFixBridge) listTasks(
	ctx context.Context,
) (string, error) {
	resp, err := b.doRequest(ctx, "GET", "/tasks", nil)
	if err != nil {
		return "", err
	}

	var tasks []struct {
		ID              string  `json:"id"`
		TaskDescription string  `json:"task_description"`
		Status          string  `json:"status"`
		APICostUSD      float64 `json:"api_cost_usd"`
	}
	if err := json.Unmarshal(resp, &tasks); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(tasks) == 0 {
		return "No active tasks.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tasks (%d):\n", len(tasks)))
	for _, t := range tasks {
		desc := t.TaskDescription
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf(
			"\n[%s] %s\n  %s ($%.4f)\n",
			t.Status, safeTrunc(t.ID, 8), desc, t.APICostUSD,
		))
	}
	return sb.String(), nil
}

func (b *AltFixBridge) stopTask(
	ctx context.Context, id string,
) (string, error) {
	_, err := b.doRequest(ctx, "POST", "/tasks/"+id+"/stop", nil)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Stop requested for task %s", id), nil
}

func (b *AltFixBridge) steerTask(
	ctx context.Context, text string,
) (string, error) {
	// Format: /steer <id> <message>
	parts := strings.SplitN(
		strings.TrimPrefix(text, "/steer "), " ", 2,
	)
	if len(parts) < 2 {
		return "Usage: /steer <task-id> <message>", nil
	}
	id := parts[0]
	message := parts[1]

	body := map[string]string{"message": message}
	data, _ := json.Marshal(body)

	_, err := b.doRequest(ctx, "POST", "/tasks/"+id+"/steer", data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Steer sent to task %s", id), nil
}

func (b *AltFixBridge) showCost(
	ctx context.Context,
) (string, error) {
	resp, err := b.doRequest(ctx, "GET", "/tasks", nil)
	if err != nil {
		return "", err
	}

	var tasks []struct {
		APICostUSD float64 `json:"api_cost_usd"`
	}
	if err := json.Unmarshal(resp, &tasks); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	var total float64
	for _, t := range tasks {
		total += t.APICostUSD
	}
	return fmt.Sprintf("Total cost: $%.4f (%d tasks)", total, len(tasks)), nil
}

func (b *AltFixBridge) doRequest(
	ctx context.Context, method, path string, body []byte,
) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(
		ctx, method, b.daemonURL+path, bodyReader,
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if b.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+b.authToken)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf(
			"daemon returned %d: %s", resp.StatusCode, string(data),
		)
	}

	return data, nil
}

func helpText() string {
	return `AltFix Gateway Commands:

/fix <description>  - Create a new task
/status             - List all tasks
/stop <task-id>     - Cancel a task
/steer <id> <msg>   - Steer an agent
/cost               - Show total cost
/help               - Show this help

Or just send a message to create a task.`
}

// safeTrunc returns s truncated to n characters, or s itself if
// len(s) <= n. Prevents panics on short IDs.
func safeTrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
