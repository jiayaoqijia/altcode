package agent

import "sync"

// Registry tracks running agents with names and depth limits.
type Registry struct {
	mu       sync.Mutex
	agents   map[string]*RunningAgent
	maxDepth int
}

// RunningAgent tracks a spawned agent's state.
type RunningAgent struct {
	Agent *Agent
	Depth int
	Done  chan struct{}
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

// Register adds a running agent. Returns false if depth exceeded.
func (r *Registry) Register(name string, ag *Agent, depth int) (*RunningAgent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if depth > r.maxDepth {
		return nil, false
	}

	ra := &RunningAgent{Agent: ag, Depth: depth, Done: make(chan struct{})}
	r.agents[name] = ra
	return ra, true
}

// Release removes a running agent.
func (r *Registry) Release(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ra, ok := r.agents[name]; ok {
		close(ra.Done)
		delete(r.agents, name)
	}
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
