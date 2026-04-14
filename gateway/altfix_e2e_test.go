//go:build e2e

package gateway

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func daemonURL() string {
	if u := os.Getenv("DAEMON_URL"); u != "" {
		return u
	}
	return "http://localhost:9200"
}

func daemonToken() string {
	if t := os.Getenv("DAEMON_TOKEN"); t != "" {
		return t
	}
	return "gw-test-token"
}

func skipIfNoDaemon(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(daemonURL() + "/health")
	if err != nil || resp.StatusCode != 200 {
		t.Skip("daemon not running at " + daemonURL())
	}
}

func testBridge(t *testing.T) *AltFixBridge {
	t.Helper()
	skipIfNoDaemon(t)
	return &AltFixBridge{
		daemonURL: daemonURL(),
		authToken: daemonToken(),
		repoURL:   "https://github.com/test/gateway-e2e",
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// extractTaskID pulls the task ID from the "Task created: <id>" reply.
// The daemon returns hex IDs of 16+ chars.
func extractTaskID(reply string) string {
	// Reply format: "Task created: <hex-id>\nStatus: pending\n..."
	for _, line := range strings.Split(reply, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Task created:") {
			id := strings.TrimSpace(
				strings.TrimPrefix(line, "Task created:"),
			)
			return id
		}
	}
	return ""
}

func TestE2E_CreateTask(t *testing.T) {
	b := testBridge(t)
	reply, err := b.createTask(
		context.Background(), "Gateway E2E: fix auth bug",
	)
	if err != nil {
		t.Fatalf("createTask: %v", err)
	}
	if !strings.Contains(reply, "Task created:") {
		t.Errorf("unexpected reply: %s", reply)
	}
	id := extractTaskID(reply)
	if id == "" {
		t.Errorf("reply missing task ID: %s", reply)
	}
	if !strings.Contains(reply, "pending") {
		t.Errorf("reply missing pending status: %s", reply)
	}
}

func TestE2E_ListTasks(t *testing.T) {
	b := testBridge(t)
	ctx := context.Background()

	// Create a task first so there is at least one.
	_, err := b.createTask(ctx, "Gateway E2E: list test")
	if err != nil {
		t.Fatalf("setup createTask: %v", err)
	}

	reply, err := b.listTasks(ctx)
	if err != nil {
		t.Fatalf("listTasks: %v", err)
	}
	if reply == "No active tasks." {
		t.Error("expected at least one task after create")
	}
	// listTasks formats as "Tasks (N):\n\n[status] id..."
	if !strings.Contains(reply, "Tasks (") {
		t.Errorf("unexpected format in reply: %s", reply)
	}
}

func TestE2E_SteerTask(t *testing.T) {
	b := testBridge(t)
	ctx := context.Background()

	reply, err := b.createTask(ctx, "Gateway E2E: steer test")
	if err != nil {
		t.Fatalf("setup createTask: %v", err)
	}
	taskID := extractTaskID(reply)
	if taskID == "" {
		t.Skip("could not extract task ID from reply")
	}

	steerReply, err := b.steerTask(
		ctx, "/steer "+taskID+" focus on tests",
	)
	if err != nil {
		t.Fatalf("steerTask: %v", err)
	}
	if !strings.Contains(steerReply, "Steer sent") {
		t.Errorf("unexpected steer reply: %s", steerReply)
	}
}

func TestE2E_StopTask(t *testing.T) {
	b := testBridge(t)
	ctx := context.Background()

	reply, err := b.createTask(ctx, "Gateway E2E: stop test")
	if err != nil {
		t.Fatalf("setup createTask: %v", err)
	}
	taskID := extractTaskID(reply)
	if taskID == "" {
		t.Skip("could not extract task ID")
	}

	stopReply, err := b.stopTask(ctx, taskID)
	if err != nil {
		t.Fatalf("stopTask: %v", err)
	}
	if !strings.Contains(stopReply, "Stop requested") {
		t.Errorf("unexpected stop reply: %s", stopReply)
	}
}

func TestE2E_ShowCost(t *testing.T) {
	b := testBridge(t)
	reply, err := b.showCost(context.Background())
	if err != nil {
		t.Fatalf("showCost: %v", err)
	}
	if !strings.Contains(reply, "$") {
		t.Errorf("cost reply missing dollar sign: %s", reply)
	}
	if !strings.Contains(reply, "tasks") {
		t.Errorf("cost reply missing task count: %s", reply)
	}
}

func TestE2E_HelpText(t *testing.T) {
	reply := helpText()
	for _, cmd := range []string{
		"/fix", "/status", "/stop", "/steer", "/cost", "/help",
	} {
		if !strings.Contains(reply, cmd) {
			t.Errorf("help text missing %s", cmd)
		}
	}
}

func TestE2E_StopNonexistent(t *testing.T) {
	b := testBridge(t)
	_, err := b.stopTask(
		context.Background(), "nonexistent-id-abc",
	)
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got: %v", err)
	}
}

func TestE2E_SteerNonexistent(t *testing.T) {
	b := testBridge(t)
	_, err := b.steerTask(
		context.Background(),
		"/steer nonexistent-id focus on nothing",
	)
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got: %v", err)
	}
}

func TestE2E_UnauthorizedRequest(t *testing.T) {
	skipIfNoDaemon(t)
	b := &AltFixBridge{
		daemonURL: daemonURL(),
		authToken: "wrong-token",
		repoURL:   "https://github.com/test/gateway-e2e",
		client:    &http.Client{Timeout: 10 * time.Second},
	}
	_, err := b.listTasks(context.Background())
	if err == nil {
		t.Error("expected error with wrong auth token")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got: %v", err)
	}
}

func TestE2E_CreateTaskEmptyDescription(t *testing.T) {
	b := testBridge(t)
	_, err := b.createTask(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty task description")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 error, got: %v", err)
	}
}

func TestE2E_FullLifecycle(t *testing.T) {
	b := testBridge(t)
	ctx := context.Background()

	// 1. Create
	reply, err := b.createTask(ctx, "Full lifecycle E2E test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Logf("Create: %s", reply)

	taskID := extractTaskID(reply)
	if taskID == "" {
		t.Fatal("could not extract task ID from create reply")
	}

	// 2. List (should include new task)
	listReply, err := b.listTasks(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// The list shows first 8 chars of each ID
	shortID := taskID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	if !strings.Contains(listReply, shortID) {
		t.Errorf("list should contain task %s: %s",
			shortID, listReply)
	}

	// 3. Cost (should include at least 1 task)
	costReply, err := b.showCost(ctx)
	if err != nil {
		t.Fatalf("cost: %v", err)
	}
	t.Logf("Cost: %s", costReply)

	// 4. Steer the task
	steerReply, err := b.steerTask(
		ctx, "/steer "+taskID+" wrap up quickly",
	)
	if err != nil {
		t.Fatalf("steer: %v", err)
	}
	t.Logf("Steer: %s", steerReply)

	// 5. Stop the task
	stopReply, err := b.stopTask(ctx, taskID)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	t.Logf("Stop: %s", stopReply)

	// 6. Verify cost still works after stop
	costReply2, err := b.showCost(ctx)
	if err != nil {
		t.Fatalf("cost after stop: %v", err)
	}
	if !strings.Contains(costReply2, "$") {
		t.Errorf("cost reply after stop missing $: %s", costReply2)
	}
}
