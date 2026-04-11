package tool

import (
	"encoding/json"
	"sort"
	"sync"
)

// Registry holds named tools and provides lookup and schema generation.
//
// Concurrency: protected by RWMutex. Register/Subset take the write
// lock; Get/All/Schemas take the read lock. Previously the registry
// was an unguarded map, so any concurrent Register call (e.g. plugin
// discovery racing with engine startup) could panic with 'concurrent
// map read and map write'.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry. Last write wins for duplicate
// names — callers that need to detect collisions should check Get
// first.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns the named tool, if it exists.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All returns every registered tool, sorted by name for deterministic
// output. Map iteration would otherwise return tools in random order
// and break any caller that relies on the list being byte-stable
// (system prompts, prompt-cache hashing, test diffs).
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name() < tools[j].Name()
	})
	return tools
}

// Subset returns a new Registry containing only the named tools.
// Tools not found in the source registry are silently skipped.
func (r *Registry) Subset(names []string) *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sub := NewRegistry()
	for _, name := range names {
		if t, ok := r.tools[name]; ok {
			sub.Register(t)
		}
	}
	return sub
}

// Schemas returns JSON schemas for every registered tool, sorted by
// name for deterministic prompt content.
func (r *Registry) Schemas() []Schema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]Schema, 0, len(r.tools))
	for _, t := range r.tools {
		schemas = append(schemas, Schema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		})
	}
	sort.Slice(schemas, func(i, j int) bool {
		return schemas[i].Name < schemas[j].Name
	})
	return schemas
}

// Schema is the tool definition sent to the model.
type Schema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}
