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
		reply, err = b.createTaskWithOpts(ctx, desc)
	case text == "/status":
		reply, err = b.listTasks(ctx)
	case strings.HasPrefix(text, "/task "):
		id := strings.TrimSpace(strings.TrimPrefix(text, "/task "))
		reply, err = b.getTask(ctx, id)
	case text == "/active":
		reply, err = b.listTasksByStatus(ctx, "active")
	case text == "/failed":
		reply, err = b.listTasksByStatus(ctx, "failed")
	case text == "/completed":
		reply, err = b.listTasksByStatus(ctx, "completed")
	case text == "/prs":
		reply, err = b.listPRs(ctx)
	case strings.HasPrefix(text, "/stop "):
		id := strings.TrimSpace(strings.TrimPrefix(text, "/stop "))
		reply, err = b.stopTask(ctx, id)
	case strings.HasPrefix(text, "/steer "):
		reply, err = b.steerTask(ctx, text)
	case strings.HasPrefix(text, "/share "):
		id := strings.TrimSpace(strings.TrimPrefix(text, "/share "))
		reply, err = b.generateShareLink(ctx, id)
	case strings.HasPrefix(text, "/checkpoints "):
		id := strings.TrimSpace(strings.TrimPrefix(text, "/checkpoints "))
		reply, err = b.listCheckpoints(ctx, id)
	case strings.HasPrefix(text, "/retry "):
		id := strings.TrimSpace(strings.TrimPrefix(text, "/retry "))
		reply, err = b.retryTask(ctx, id)
	case text == "/health":
		reply, err = b.checkHealth(ctx)
	case text == "/dashboard":
		reply, err = b.dashboard(ctx)
	case text == "/cost":
		reply, err = b.showCost(ctx)
	case text == "/help" || text == "/start":
		reply = helpText()
	default:
		// Not a recognized command; treat as a /fix
		reply, err = b.createTaskWithOpts(ctx, text)
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

// parseFixOpts extracts optional --repo, --model, --cost flags from a
// /fix description string. Returns the cleaned description and a map
// of parsed options.
func parseFixOpts(raw string) (string, map[string]string) {
	opts := make(map[string]string)
	words := strings.Fields(raw)
	var desc []string
	for i := 0; i < len(words); i++ {
		switch words[i] {
		case "--repo", "--model", "--cost":
			key := strings.TrimPrefix(words[i], "--")
			if i+1 < len(words) {
				opts[key] = words[i+1]
				i++
			}
		default:
			desc = append(desc, words[i])
		}
	}
	return strings.Join(desc, " "), opts
}

func (b *AltFixBridge) createTaskWithOpts(
	ctx context.Context, raw string,
) (string, error) {
	description, opts := parseFixOpts(raw)
	if description == "" {
		return "Usage: /fix <description> [--repo owner/name] " +
			"[--model name] [--cost max]", nil
	}

	body := map[string]string{
		"repo_url": b.repoURL,
		"task":     description,
	}
	if v, ok := opts["repo"]; ok {
		body["repo_url"] = v
	}
	if v, ok := opts["model"]; ok {
		body["model"] = v
	}
	if v, ok := opts["cost"]; ok {
		body["max_cost"] = v
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

func (b *AltFixBridge) getTask(
	ctx context.Context, id string,
) (string, error) {
	resp, err := b.doRequest(ctx, "GET", "/tasks/"+id, nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Task struct {
			ID          string  `json:"id"`
			Description string  `json:"task_description"`
			Status      string  `json:"status"`
			APICostUSD  float64 `json:"api_cost_usd"`
			PRURL       string  `json:"pr_url"`
			ErrorMsg    string  `json:"error_message"`
			CreatedAt   string  `json:"created_at"`
		} `json:"task"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	t := result.Task
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task %s\n", safeTrunc(t.ID, 8)))
	sb.WriteString(fmt.Sprintf("Status: %s\n", t.Status))
	sb.WriteString(fmt.Sprintf("Description: %s\n", t.Description))
	sb.WriteString(fmt.Sprintf("Cost: $%.4f\n", t.APICostUSD))
	if t.PRURL != "" {
		sb.WriteString(fmt.Sprintf("PR: %s\n", t.PRURL))
	}
	if t.ErrorMsg != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", t.ErrorMsg))
	}
	if t.CreatedAt != "" {
		sb.WriteString(fmt.Sprintf("Created: %s\n", t.CreatedAt))
	}
	return sb.String(), nil
}

// taskEntry is the common shape returned by GET /tasks.
type taskEntry struct {
	ID          string  `json:"id"`
	Description string  `json:"task_description"`
	Status      string  `json:"status"`
	APICostUSD  float64 `json:"api_cost_usd"`
	PRURL       string  `json:"pr_url"`
}

func (b *AltFixBridge) listTasksByStatus(
	ctx context.Context, filter string,
) (string, error) {
	resp, err := b.doRequest(ctx, "GET", "/tasks", nil)
	if err != nil {
		return "", err
	}

	var tasks []taskEntry
	if err := json.Unmarshal(resp, &tasks); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	var filtered []taskEntry
	for _, t := range tasks {
		if matchFilter(t.Status, filter) {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) == 0 {
		return fmt.Sprintf("No %s tasks.", filter), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"%s tasks (%d):\n",
		strings.ToUpper(filter[:1])+filter[1:], len(filtered),
	))
	for _, t := range filtered {
		desc := t.Description
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

func matchFilter(status, filter string) bool {
	switch filter {
	case "active":
		return status == "pending" ||
			status == "planning" ||
			status == "implementing" ||
			status == "reviewing" ||
			status == "testing"
	case "failed":
		return status == "failed"
	case "completed":
		return status == "merged" ||
			status == "completed" ||
			status == "closed"
	}
	return false
}

func (b *AltFixBridge) listPRs(
	ctx context.Context,
) (string, error) {
	resp, err := b.doRequest(ctx, "GET", "/tasks", nil)
	if err != nil {
		return "", err
	}

	var tasks []taskEntry
	if err := json.Unmarshal(resp, &tasks); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	var sb strings.Builder
	var count int
	for _, t := range tasks {
		if t.PRURL == "" {
			continue
		}
		count++
		desc := t.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		sb.WriteString(fmt.Sprintf(
			"[%s] %s\n  %s\n  %s\n",
			t.Status, safeTrunc(t.ID, 8), desc, t.PRURL,
		))
	}

	if count == 0 {
		return "No PRs created yet.", nil
	}
	return fmt.Sprintf("PRs (%d):\n\n%s", count, sb.String()), nil
}

func (b *AltFixBridge) generateShareLink(
	_ context.Context, id string,
) (string, error) {
	// The gateway does not hold the daemon's signing key, so we
	// return the authenticated web UI URL instead of an HMAC link.
	base := strings.TrimSuffix(b.daemonURL, "/api/v1")
	base = strings.TrimSuffix(base, "/api")
	return fmt.Sprintf(
		"Share link (requires auth):\n%s/ui/tasks/%s", base, id,
	), nil
}

func (b *AltFixBridge) listCheckpoints(
	ctx context.Context, id string,
) (string, error) {
	resp, err := b.doRequest(
		ctx, "GET", "/tasks/"+id+"/checkpoints", nil,
	)
	if err != nil {
		return "", err
	}

	var checkpoints []struct {
		Phase     string `json:"phase"`
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(resp, &checkpoints); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(checkpoints) == 0 {
		return fmt.Sprintf(
			"No checkpoints for task %s.", safeTrunc(id, 8),
		), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"Checkpoints for %s (%d):\n\n",
		safeTrunc(id, 8), len(checkpoints),
	))
	for _, cp := range checkpoints {
		icon := phaseIcon(cp.Status)
		line := fmt.Sprintf(
			"%s %s [%s]", icon, cp.Phase, cp.Status,
		)
		if cp.Timestamp != "" {
			line += " " + cp.Timestamp
		}
		sb.WriteString(line + "\n")
		if cp.Message != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", cp.Message))
		}
	}
	return sb.String(), nil
}

func phaseIcon(status string) string {
	switch status {
	case "done", "completed", "passed":
		return "[ok]"
	case "running", "in_progress":
		return "[..]"
	case "failed":
		return "[!!]"
	default:
		return "[  ]"
	}
}

func (b *AltFixBridge) retryTask(
	ctx context.Context, id string,
) (string, error) {
	// Fetch the original task to get its description.
	resp, err := b.doRequest(ctx, "GET", "/tasks/"+id, nil)
	if err != nil {
		return "", fmt.Errorf("fetch task for retry: %w", err)
	}

	var original struct {
		Task struct {
			Description string `json:"task_description"`
			RepoURL     string `json:"repo_url"`
		} `json:"task"`
	}
	if err := json.Unmarshal(resp, &original); err != nil {
		return "", fmt.Errorf("parse task for retry: %w", err)
	}

	desc := original.Task.Description
	if desc == "" {
		return fmt.Sprintf(
			"Cannot retry task %s: empty description.",
			safeTrunc(id, 8),
		), nil
	}

	repo := original.Task.RepoURL
	if repo == "" {
		repo = b.repoURL
	}

	reply, err := b.createTask(ctx, desc)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"Retrying task %s:\n%s", safeTrunc(id, 8), reply,
	), nil
}

func (b *AltFixBridge) checkHealth(
	ctx context.Context,
) (string, error) {
	resp, err := b.doRequest(ctx, "GET", "/health", nil)
	if err != nil {
		return "", err
	}

	var health struct {
		Status  string `json:"status"`
		Uptime  string `json:"uptime"`
		Version string `json:"version"`
		Workers int    `json:"workers"`
	}
	if err := json.Unmarshal(resp, &health); err != nil {
		// Even if parsing fails, the daemon responded — it's up.
		return "Daemon is up (response not parseable).", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Daemon: %s\n", health.Status))
	if health.Version != "" {
		sb.WriteString(fmt.Sprintf("Version: %s\n", health.Version))
	}
	if health.Uptime != "" {
		sb.WriteString(fmt.Sprintf("Uptime: %s\n", health.Uptime))
	}
	if health.Workers > 0 {
		sb.WriteString(fmt.Sprintf("Workers: %d\n", health.Workers))
	}
	return sb.String(), nil
}

func (b *AltFixBridge) dashboard(
	ctx context.Context,
) (string, error) {
	resp, err := b.doRequest(ctx, "GET", "/tasks", nil)
	if err != nil {
		return "", err
	}

	var tasks []taskEntry
	if err := json.Unmarshal(resp, &tasks); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	var active, queued, succeeded, failed, prs int
	var totalCost float64

	for _, t := range tasks {
		totalCost += t.APICostUSD
		switch {
		case matchFilter(t.Status, "active"):
			active++
		case t.Status == "queued":
			queued++
		case matchFilter(t.Status, "completed"):
			succeeded++
		case matchFilter(t.Status, "failed"):
			failed++
		}
		if t.PRURL != "" {
			prs++
		}
	}

	total := succeeded + failed
	var rate float64
	if total > 0 {
		rate = float64(succeeded) / float64(total) * 100
	}

	var sb strings.Builder
	sb.WriteString("Dashboard\n")
	sb.WriteString(fmt.Sprintf("Active:       %d\n", active))
	sb.WriteString(fmt.Sprintf("Queued:       %d\n", queued))
	sb.WriteString(fmt.Sprintf("Succeeded:    %d\n", succeeded))
	sb.WriteString(fmt.Sprintf("Failed:       %d\n", failed))
	sb.WriteString(fmt.Sprintf("Success rate: %.0f%%\n", rate))
	sb.WriteString(fmt.Sprintf("PRs created:  %d\n", prs))
	sb.WriteString(fmt.Sprintf("Total cost:   $%.4f\n", totalCost))
	sb.WriteString(fmt.Sprintf("Total tasks:  %d\n", len(tasks)))
	return sb.String(), nil
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

/fix <desc> [opts]    - Create a task (--repo, --model, --cost)
/task <id>            - View single task detail
/status               - List all tasks
/active               - Show running tasks
/failed               - Show failed tasks
/completed            - Show completed tasks
/prs                  - Show PRs created by AltFix
/stop <id>            - Cancel a task
/steer <id> <msg>     - Steer an agent
/share <id>           - Get share link for a task
/checkpoints <id>     - List phase checkpoints
/retry <id>           - Re-run a failed task
/health               - Check daemon status
/dashboard            - KPI summary
/cost                 - Show total cost
/help                 - Show this help

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
