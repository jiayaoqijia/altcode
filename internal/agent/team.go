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
	cancels  map[string]context.CancelFunc // per-agent cancel funcs
	results  map[string]string
	registry *Registry
	mailbox  *Mailbox // shared mailbox for all team agents
	mu       sync.Mutex
	nextID   int
	depth    int // depth of this team in the agent hierarchy (root = 1)
}

// NewTeam creates a top-level team. Equivalent to NewSubTeam(name, 1).
func NewTeam(name string) *Team {
	return NewSubTeam(name, 1)
}

// NewSubTeam creates a team that knows its position in the agent
// hierarchy. The depth is propagated to the registry's depth check
// so a child team spawned at depth N can't recursively spawn beyond
// the registry's maxDepth — the previous code hardcoded depth=1
// at every Register call, so the depth check was effectively dead.
func NewSubTeam(name string, depth int) *Team {
	if depth < 1 {
		depth = 1
	}
	return &Team{
		name:     name,
		agents:   make(map[string]*RunningAgent),
		cancels:  make(map[string]context.CancelFunc),
		results:  make(map[string]string),
		registry: NewRegistry(5),
		mailbox:  NewMailbox(),
		depth:    depth,
	}
}

// NewChildTeam creates a sub-team that shares its parent's registry
// and inherits depth+1. The shared registry is what makes depth
// enforcement actually fire — if a grandchild team also tries to
// register at depth 3 in a registry whose maxDepth is 2, the
// Register call returns false and the spawn is rejected. Without
// sharing, each team had its own fresh registry and the maxDepth
// check could never trip across team boundaries.
//
// Use this whenever an agent inside a team wants to spin up its
// own sub-team. NewSubTeam (above) creates an isolated team for
// top-level callers; NewChildTeam links to the parent for proper
// depth accounting.
func NewChildTeam(parent *Team, name string) *Team {
	if parent == nil {
		return NewTeam(name)
	}
	return &Team{
		name:     name,
		agents:   make(map[string]*RunningAgent),
		cancels:  make(map[string]context.CancelFunc),
		results:  make(map[string]string),
		registry: parent.registry, // SHARED with parent
		mailbox:  parent.mailbox,  // shared mailbox so siblings can talk
		depth:    parent.depth + 1,
	}
}

// Name returns the team's name.
func (t *Team) Name() string { return t.name }

// SpawnAgent starts an agent in its own goroutine and returns an ID.
// Uses the Registry for lifecycle tracking and Mailbox for IPC.
//
// The team owns a per-agent cancel func so WaitAll can stop a stuck
// child engine on timeout instead of letting it drain naturally and
// burn tokens after the user already gave up.
func (t *Team) SpawnAgent(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	input string,
) string {
	t.mu.Lock()
	id := fmt.Sprintf("%s-%d", ag.Name, t.nextID)
	t.nextID++

	// Register with the team's registry for lifecycle tracking. Pass
	// `input` as the task so it lands in ra.Task under the registry
	// mutex — the previous code wrote ra.Task = input AFTER Register
	// returned, under t.mu, which races with LiveAgents() reading
	// ra.Task under r.mu (two mutexes protecting the same field).
	ra, ok := t.registry.Register(id, ag, t.depth, "/"+t.name, input)
	if !ok {
		t.mu.Unlock()
		return ""
	}
	t.agents[id] = ra

	agentCtx, cancel := context.WithCancel(ctx)
	t.cancels[id] = cancel
	t.mu.Unlock()

	go t.runAgent(agentCtx, parent, ag, id, input)
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
	// Use ForkFullHistory for team workers so they share context
	ch := SpawnWithOptions(ctx, parent, ag, input, SpawnOptions{
		ForkMode: ForkFullHistory,
		Mailbox:  t.mailbox,
	})
	result := collectOutput(ch)
	t.storeResult(id, result.text)
	t.markDoneWithStatus(id, result.status)
}

func (t *Team) markDoneWithStatus(id string, status AgentStatus) {
	t.mu.Lock()
	if ra, ok := t.agents[id]; ok {
		ra.Status = status
		select {
		case <-ra.Done:
		default:
			close(ra.Done)
		}
	}
	// Drop the cancel func so the team doesn't hold a reference to a
	// completed agent's context after WaitAll returns.
	if cancel, ok := t.cancels[id]; ok {
		delete(t.cancels, id)
		// Calling cancel() on an already-finished agent is safe and a
		// no-op for downstream consumers — but releases any resources
		// that the cancel func might still be holding (timer, etc).
		go cancel()
	}
	t.mu.Unlock()
	t.registry.Release(id, status)
}

func (t *Team) storeResult(id, output string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.results[id] = output
}

// agentResult holds the output + status from an agent run.
type agentResult struct {
	text   string
	status AgentStatus
}

// collectOutput drains an event channel, extracts text, and detects errors.
func collectOutput(ch <-chan event.Event) agentResult {
	var text string
	status := StatusSucceeded
	for ev := range ch {
		switch ev.Type {
		case event.TextDelta:
			text += ev.Text
		case event.ErrorEvent:
			status = StatusFailed
			text += "\n[error] " + ev.Error
		}
	}
	return agentResult{text: text, status: status}
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
// On timeout, the per-agent cancel func is invoked so the child engine
// stops processing instead of running to completion in the background.
//
// Uses a single ctx-with-deadline shared across iterations. The
// previous version used time.After() outside the loop, which fires
// exactly once — after the first stuck agent consumed the channel,
// every subsequent <-deadline blocked forever and the whole goroutine
// deadlocked when more than one agent was stuck.
func (t *Team) WaitAll(timeout time.Duration) map[string]string {
	t.mu.Lock()
	agents := make(map[string]*RunningAgent, len(t.agents))
	for k, v := range t.agents {
		agents[k] = v
	}
	t.mu.Unlock()

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), timeout)
	defer deadlineCancel()

	for id, ra := range agents {
		select {
		case <-ra.Done:
		case <-deadlineCtx.Done():
			// Cancel the spawned agent so its engine loop exits
			// promptly. Then wait briefly for the goroutine to finish
			// flushing its result; if it never does, record "timeout".
			t.cancelAgent(id)
			select {
			case <-ra.Done:
			case <-time.After(2 * time.Second):
				t.storeResult(id, "timeout")
			}
		}
	}
	return t.copyResults()
}

// cancelAgent invokes the per-agent cancel func so the child engine
// stops processing. Safe to call multiple times.
func (t *Team) cancelAgent(id string) {
	t.mu.Lock()
	cancel, ok := t.cancels[id]
	delete(t.cancels, id)
	t.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
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
