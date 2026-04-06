package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Role defines an agent's capabilities and config overrides.
// Can be loaded from JSON files in .agents/roles/ or .altcode/roles/.
type Role struct {
	Name        string   `json:"name"`
	Model       string   `json:"model,omitempty"`       // override model
	Tools       []string `json:"tools,omitempty"`       // restrict tool set
	MaxTokens   int      `json:"max_tokens,omitempty"`  // override max tokens
	Temperature *float64 `json:"temperature,omitempty"` // override temperature
	Prompt      string   `json:"prompt,omitempty"`      // additional system instructions
}

// LoadRole reads a role definition from a JSON file.
func LoadRole(path string) (*Role, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Role
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.Name == "" {
		r.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return &r, nil
}

// DiscoverRoles finds all role JSON files in the given directories.
func DiscoverRoles(dirs ...string) map[string]*Role {
	roles := make(map[string]*Role)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			r, err := LoadRole(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			roles[r.Name] = r
		}
	}
	return roles
}

// ApplyToAgent merges role config into an agent definition.
// Role fields override agent fields where present.
func (r *Role) ApplyToAgent(ag *Agent) {
	if r.Model != "" {
		ag.Model = r.Model
	}
	if len(r.Tools) > 0 {
		ag.Tools = r.Tools
	}
	if r.Prompt != "" {
		ag.SystemPrompt = r.Prompt + "\n\n" + ag.SystemPrompt
	}
}
