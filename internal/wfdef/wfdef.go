// Package wfdef defines workflow definitions parsed from YAML frontmatter
// markdown files. Workflows describe phased agent execution with dependencies.
package wfdef

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// WorkflowDef holds a parsed workflow definition.
type WorkflowDef struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Phases      []PhaseDef `yaml:"phases"`
	Path        string     `yaml:"-"`
}

// PhaseDef describes one phase in a workflow.
type PhaseDef struct {
	Name      string            `yaml:"name"`
	DependsOn []string          `yaml:"depends_on"`
	Parallel  bool              `yaml:"parallel"`
	Agents    []AgentAssignment `yaml:"agents"`
	Timeout   time.Duration     `yaml:"timeout"`
	Required  bool              `yaml:"required"`
	OnFailure FailurePolicy     `yaml:"on_failure"`
	Condition string            `yaml:"condition"`
}

// AgentAssignment maps a role to a CLI backend.
type AgentAssignment struct {
	Role    string `yaml:"role"`
	Backend string `yaml:"backend"`
	Model   string `yaml:"model"`
	Prompt  string `yaml:"prompt"`
}

// FailurePolicy controls what happens when a phase fails.
type FailurePolicy string

const (
	FailureRetry FailurePolicy = "retry"
	FailureSkip  FailurePolicy = "skip"
	FailureAbort FailurePolicy = "abort"
	FailureHuman FailurePolicy = "human"
)

// Parse reads a workflow definition from markdown with YAML frontmatter.
func Parse(data []byte) (*WorkflowDef, error) {
	fm, err := extractFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var def WorkflowDef
	if err := yaml.Unmarshal(fm, &def); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}
	if def.Name == "" {
		return nil, fmt.Errorf("workflow name is required")
	}
	// Validate phases up front: empty names, duplicates, and bad
	// depends_on references must fail at parse time, not deferred
	// to TopoSort during execution. A workflow with a typo in
	// 'depends_on' should be rejected when the user tries to load
	// it, not silently accepted and then explode mid-run.
	seen := map[string]bool{}
	for i := range def.Phases {
		if def.Phases[i].OnFailure == "" {
			def.Phases[i].OnFailure = FailureAbort
		}
		name := def.Phases[i].Name
		if name == "" {
			return nil, fmt.Errorf("phase %d has no name", i)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate phase name %q", name)
		}
		seen[name] = true
	}
	for i := range def.Phases {
		for _, dep := range def.Phases[i].DependsOn {
			if !seen[dep] {
				return nil, fmt.Errorf("phase %q depends on unknown phase %q", def.Phases[i].Name, dep)
			}
		}
	}
	// Cycle check: if TopoSort returns an error, the dependency
	// graph is invalid. Run it once at parse time so the user finds
	// out immediately instead of after agents have started.
	if _, err := def.TopoSort(); err != nil {
		return nil, fmt.Errorf("invalid workflow dependency graph: %w", err)
	}
	return &def, nil
}

// ParseFile reads a workflow from a markdown file.
func ParseFile(path string) (*WorkflowDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	def.Path = path
	return def, nil
}

// DiscoverWarnings collects per-file parse failures from the most
// recent Discover call so the TUI can surface why a workflow file
// failed to load instead of silently dropping it.
var DiscoverWarnings []string

// Discover finds all workflow files in the given directories. Parse
// failures are recorded in DiscoverWarnings rather than returned as
// errors so a single broken workflow doesn't hide all the others.
func Discover(dirs ...string) ([]*WorkflowDef, error) {
	DiscoverWarnings = DiscoverWarnings[:0]
	var defs []*WorkflowDef
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			def, err := ParseFile(path)
			if err != nil {
				DiscoverWarnings = append(DiscoverWarnings,
					fmt.Sprintf("workflow %s: %v", path, err))
				continue
			}
			defs = append(defs, def)
		}
	}
	return defs, nil
}

// TopoSort returns phase names in dependency order.
func (d *WorkflowDef) TopoSort() ([]string, error) {
	phaseMap := map[string]*PhaseDef{}
	for i := range d.Phases {
		phaseMap[d.Phases[i].Name] = &d.Phases[i]
	}
	visited := map[string]bool{}
	visiting := map[string]bool{}
	var order []string

	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("cycle detected at phase %q", name)
		}
		visiting[name] = true
		p, ok := phaseMap[name]
		if !ok {
			return fmt.Errorf("unknown phase %q", name)
		}
		for _, dep := range p.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		order = append(order, name)
		return nil
	}

	for _, p := range d.Phases {
		if err := visit(p.Name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// PhaseByName returns the phase with the given name, or nil.
func (d *WorkflowDef) PhaseByName(name string) *PhaseDef {
	for i := range d.Phases {
		if d.Phases[i].Name == name {
			return &d.Phases[i]
		}
	}
	return nil
}

func extractFrontmatter(data []byte) ([]byte, error) {
	s := strings.TrimSpace(string(data))
	const sep = "---"
	if !strings.HasPrefix(s, sep) {
		return nil, fmt.Errorf("no YAML frontmatter found")
	}
	rest := s[len(sep):]
	end := strings.Index(rest, "\n"+sep)
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	return []byte(strings.TrimSpace(rest[:end])), nil
}
