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
	name     string
	agents   map[string]*RunningAgent
	results  map[string]string
	registry *Registry
	mailbox  *Mailbox // shared mailbox for all team agents
	mu       sync.Mutex
	nextID   int
}

// NewTeam creates a team with the given name.
func NewTeam(name string) *Team {
	return &Team{
		name:     name,
		agents:   make(map[string]*RunningAgent),
		results:  make(map[string]string),
		registry: NewRegistry(5),
		mailbox:  NewMailbox(),
	}
}

// Name returns the team's name.
func (t *Team) Name() string { return t.name }

// SpawnAgent starts an agent in its own goroutine and returns an ID.
// Uses the Registry for lifecycle tracking and Mailbox for IPC.
func (t *Team) SpawnAgent(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	input string,
) string {
	t.mu.Lock()
	id := fmt.Sprintf("%s-%d", ag.Name, t.nextID)
	t.nextID++

	// Register with the team's registry for lifecycle tracking
	ra, ok := t.registry.Register(id, ag, 1, "/"+t.name)
	if !ok {
		t.mu.Unlock()
		return ""
	}
	ra.Task = input
	t.agents[id] = ra
	t.mu.Unlock()

	go t.runAgent(ctx, parent, ag, id, input)
	return id
}

// Registry returns the team's agent registry.
func (t *Team) Registry() *Registry { return t.registry }

// Mailbox returns the team's shared mailbox.
func (t *Team) SharedMailbox() *Mailbox { return t.mailbox }

func (t *Team) runAgent(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	id, input string,
) {
	defer t.markDone(id)
	// Use ForkFullHistory for team workers so they share context
	ch := SpawnWithOptions(ctx, parent, ag, input, SpawnOptions{
		ForkMode: ForkFullHistory,
		Mailbox:  t.mailbox,
	})
	output := collectOutput(ch)
	t.storeResult(id, output)
}

func (t *Team) markDone(id string) {
	t.mu.Lock()
	if ra, ok := t.agents[id]; ok {
		ra.Status = StatusSucceeded
		select {
		case <-ra.Done:
			// already closed
		default:
			close(ra.Done)
		}
	}
	t.mu.Unlock()
	// Registry.Release closes its own copy — safe because it checks internally
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

// SendMessage sends a message to another agent via the shared mailbox.
func (t *Team) SendMessage(from, to, message string) error {
	t.mu.Lock()
	if _, ok := t.agents[to]; !ok {
		t.mu.Unlock()
		return fmt.Errorf("agent %q not found", to)
	}
	t.mu.Unlock()
	t.mailbox.Send(InterAgentMessage{
		From:        from,
		To:          to,
		Content:     message,
		TriggerTurn: true,
	})
	return nil
}

// PendingMessages returns and clears messages for an agent.
func (t *Team) PendingMessages(id string) []string {
	msgs := t.mailbox.Drain()
	var result []string
	var remaining []InterAgentMessage
	for _, m := range msgs {
		if m.To == id {
			result = append(result, fmt.Sprintf("[from %s]: %s", m.From, m.Content))
		} else {
			remaining = append(remaining, m)
		}
	}
	// Put back messages for other agents
	for _, m := range remaining {
		t.mailbox.Send(m)
	}
	return result
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
