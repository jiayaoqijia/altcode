package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/agent"
	"github.com/jiayaoqijia/altcode/internal/config"
	"github.com/jiayaoqijia/altcode/internal/engine"
)

func makeTestParent(t *testing.T, response string) *engine.Engine {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(textSSE(response)))
	}))
	t.Cleanup(srv.Close)

	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{
		APIKey: "test-key", BaseURL: srv.URL,
	}
	e, err := engine.New(engine.EngineParams{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestTeam_NewTeam(t *testing.T) {
	tm := agent.NewTeam("test-team")
	if tm.Name() != "test-team" {
		t.Errorf("Name: %q", tm.Name())
	}
	status := tm.Status()
	if len(status) != 0 {
		t.Error("New team should have no agents")
	}
}

func TestTeam_SpawnAndWait(t *testing.T) {
	parent := makeTestParent(t, "hello from agent")
	tm := agent.NewTeam("spawn-test")

	ag := &agent.Agent{
		Name: "worker", Model: "inherit", SystemPrompt: "Work.",
	}

	id := tm.SpawnAgent(context.Background(), parent, ag, "do work")
	if id == "" {
		t.Fatal("Expected non-empty agent ID")
	}

	results := tm.WaitAll(5 * time.Second)
	if _, ok := results[id]; !ok {
		t.Fatalf("Missing result for %s", id)
	}
}

func TestTeam_MultipleAgents(t *testing.T) {
	parent := makeTestParent(t, "result")
	tm := agent.NewTeam("multi")

	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ag := &agent.Agent{
			Name:         fmt.Sprintf("agent-%d", i),
			Model:        "inherit",
			SystemPrompt: "Work.",
		}
		ids[i] = tm.SpawnAgent(context.Background(), parent, ag, "task")
	}

	results := tm.WaitAll(5 * time.Second)
	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}
}

func TestTeam_StatusTransitions(t *testing.T) {
	parent := makeTestParent(t, "done")
	tm := agent.NewTeam("status")

	ag := &agent.Agent{
		Name: "checker", Model: "inherit", SystemPrompt: "Check.",
	}

	tm.SpawnAgent(context.Background(), parent, ag, "check")
	tm.WaitAll(5 * time.Second)

	status := tm.Status()
	for _, s := range status {
		if s != "done" {
			t.Errorf("Expected done, got %q", s)
		}
	}
}

func TestTeam_WaitAllTimeout(t *testing.T) {
	// Use a server that hangs to trigger timeout
	srvDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		select {
		case <-r.Context().Done():
		case <-srvDone:
		}
	}))
	t.Cleanup(func() { close(srvDone); srv.Close() })

	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{
		APIKey: "k", BaseURL: srv.URL,
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	parent, _ := engine.New(engine.EngineParams{Config: cfg})
	tm := agent.NewTeam("timeout-test")
	ag := &agent.Agent{
		Name: "slow", Model: "inherit", SystemPrompt: "Wait.",
	}

	id := tm.SpawnAgent(ctx, parent, ag, "wait")
	start := time.Now()
	results := tm.WaitAll(100 * time.Millisecond)
	elapsed := time.Since(start)

	// WaitAll must return shortly after the deadline (cancel + 2s grace),
	// not block forever waiting for the hanging server.
	if elapsed > 5*time.Second {
		t.Errorf("WaitAll took too long: %s", elapsed)
	}
	// The result must exist for this id — either "timeout" (if the
	// child engine never produced output before the grace window) or
	// some captured text from the cancel propagation. Either way the
	// key must be present.
	if _, ok := results[id]; !ok {
		t.Errorf("Expected result for %s, got %v", id, results)
	}
	cancel() // unblock the hanging agent
}

func TestTeam_SendMessage(t *testing.T) {
	parent := makeTestParent(t, "ok")
	tm := agent.NewTeam("msg-test")

	ag := &agent.Agent{
		Name: "receiver", Model: "inherit", SystemPrompt: "Listen.",
	}

	id := tm.SpawnAgent(context.Background(), parent, ag, "listen")

	err := tm.SendMessage("sender", id, "hello there")
	if err != nil {
		t.Fatal(err)
	}

	msgs := tm.PendingMessages(id)
	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(msgs))
	}
	if msgs[0] != "[from sender]: hello there" {
		t.Errorf("Message: %q", msgs[0])
	}

	// Second read should be empty
	msgs = tm.PendingMessages(id)
	if len(msgs) != 0 {
		t.Error("Messages should be cleared after read")
	}
}

func TestTeam_SendMessageUnknownAgent(t *testing.T) {
	tm := agent.NewTeam("err-test")
	err := tm.SendMessage("a", "nonexistent", "msg")
	if err == nil {
		t.Error("Expected error for unknown agent")
	}
}

func TestTeam_UniqueIDs(t *testing.T) {
	parent := makeTestParent(t, "ok")
	tm := agent.NewTeam("id-test")

	ag := &agent.Agent{
		Name: "worker", Model: "inherit", SystemPrompt: "Work.",
	}

	id1 := tm.SpawnAgent(context.Background(), parent, ag, "task1")
	id2 := tm.SpawnAgent(context.Background(), parent, ag, "task2")

	if id1 == id2 {
		t.Errorf("IDs should be unique: %s == %s", id1, id2)
	}

	tm.WaitAll(5 * time.Second)
}
