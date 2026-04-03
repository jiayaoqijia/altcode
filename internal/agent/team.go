package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

// Team coordinates multiple agents running in parallel.
type Team struct {
	name    string
	agents  map[string]*RunningAgent
	results map[string]string
	mailbox map[string][]string // agent name -> pending messages
	mu      sync.Mutex
	nextID  int
}

// NewTeam creates a team with the given name.
func NewTeam(name string) *Team {
	return &Team{
		name:    name,
		agents:  make(map[string]*RunningAgent),
		results: make(map[string]string),
		mailbox: make(map[string][]string),
	}
}

// Name returns the team's name.
func (t *Team) Name() string { return t.name }

// SpawnAgent starts an agent in its own goroutine and returns an ID.
func (t *Team) SpawnAgent(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	input string,
) string {
	t.mu.Lock()
	id := fmt.Sprintf("%s-%d", ag.Name, t.nextID)
	t.nextID++
	ra := &RunningAgent{Agent: ag, Done: make(chan struct{})}
	t.agents[id] = ra
	t.mu.Unlock()

	go t.runAgent(ctx, parent, ag, id, input)
	return id
}

func (t *Team) runAgent(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	id, input string,
) {
	defer t.markDone(id)
	ch := Spawn(ctx, parent, ag, input)
	output := collectOutput(ch)
	t.storeResult(id, output)
}

func (t *Team) markDone(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ra, ok := t.agents[id]; ok {
		close(ra.Done)
	}
}

func (t *Team) storeResult(id, output string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.results[id] = output
}

// collectOutput drains an event channel and extracts text.
func collectOutput(ch <-chan event.Event) string {
	var text string
	for ev := range ch {
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
	}
	return text
}

// SendMessage appends a message to the target agent's mailbox.
func (t *Team) SendMessage(from, to, message string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.agents[to]; !ok {
		return fmt.Errorf("agent %q not found", to)
	}
	t.mailbox[to] = append(t.mailbox[to],
		fmt.Sprintf("[from %s]: %s", from, message))
	return nil
}

// PendingMessages returns and clears messages for an agent.
func (t *Team) PendingMessages(id string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	msgs := t.mailbox[id]
	delete(t.mailbox, id)
	return msgs
}

// WaitAll blocks until all agents finish or timeout, returning results.
func (t *Team) WaitAll(timeout time.Duration) map[string]string {
	t.mu.Lock()
	agents := make(map[string]*RunningAgent, len(t.agents))
	for k, v := range t.agents {
		agents[k] = v
	}
	t.mu.Unlock()

	deadline := time.After(timeout)
	for id, ra := range agents {
		select {
		case <-ra.Done:
		case <-deadline:
			t.storeResult(id, "timeout")
		}
	}
	return t.copyResults()
}

func (t *Team) copyResults() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]string, len(t.results))
	for k, v := range t.results {
		out[k] = v
	}
	return out
}

// Status returns the state of each agent: "running", "done", or "error".
func (t *Team) Status() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	status := make(map[string]string, len(t.agents))
	for id, ra := range t.agents {
		if isDone(ra.Done) {
			status[id] = "done"
		} else {
			status[id] = "running"
		}
	}
	return status
}

func isDone(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
