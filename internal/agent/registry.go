package agent

import "sync"

// Registry tracks running agents with names and depth limits.
type Registry struct {
	mu       sync.Mutex
	agents   map[string]*RunningAgent
	maxDepth int
}

// AgentStatus tracks the lifecycle state of a running agent.
type AgentStatus string

const (
	StatusRunning   AgentStatus = "running"
	StatusSucceeded AgentStatus = "succeeded"
	StatusFailed    AgentStatus = "failed"
	StatusCanceled  AgentStatus = "canceled"
)

// RunningAgent tracks a spawned agent's state.
type RunningAgent struct {
	Agent    *Agent
	Depth    int
	Path     string      // e.g. "/root/worker/researcher"
	Nickname string      // human-friendly name
	Status   AgentStatus
	Task     string      // current task description
	Done     chan struct{}
}

// NewRegistry creates an agent registry with a max spawn depth.
func NewRegistry(maxDepth int) *Registry {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	return &Registry{
		agents:   make(map[string]*RunningAgent),
		maxDepth: maxDepth,
	}
}

// nicknames are assigned to agents for human-friendly identification.
var nicknames = []string{"Alice", "Bob", "Carol", "Dave", "Eve", "Frank", "Grace", "Hank"}

// Register adds a running agent. Returns false if depth exceeded.
func (r *Registry) Register(name string, ag *Agent, depth int, parentPath string) (*RunningAgent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if depth > r.maxDepth {
		return nil, false
	}

	path := parentPath + "/" + name
	nick := nicknames[len(r.agents)%len(nicknames)]

	ra := &RunningAgent{
		Agent:    ag,
		Depth:    depth,
		Path:     path,
		Nickname: nick,
		Status:   StatusRunning,
		Done:     make(chan struct{}),
	}
	r.agents[name] = ra
	return ra, true
}

// Release marks an agent as completed and removes it.
func (r *Registry) Release(name string, status AgentStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ra, ok := r.agents[name]; ok {
		ra.Status = status
		close(ra.Done)
		delete(r.agents, name)
	}
}

// LiveAgents returns all running agents with their metadata.
func (r *Registry) LiveAgents() []RunningAgent {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]RunningAgent, 0, len(r.agents))
	for _, ra := range r.agents {
		result = append(result, *ra)
	}
	return result
}

// Get returns a running agent by name.
func (r *Registry) Get(name string) (*RunningAgent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ra, ok := r.agents[name]
	return ra, ok
}

// List returns all running agent names.
func (r *Registry) List() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.agents))
	for name := range r.agents {
		names = append(names, name)
	}
	return names
}

// Count returns the number of running agents.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.agents)
}
