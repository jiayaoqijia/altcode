// Package agent provides subagent definitions and lifecycle management.
package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// Agent is a subagent definition loaded from a markdown file.
type Agent struct {
	Name         string   // identifier (kebab-case)
	Description  string   // triggering conditions
	Model        string   // "inherit", "sonnet", "opus", "haiku"
	Color        string   // visual identifier (unused for now)
	Tools        []string // restricted tool list; nil = all tools
	SystemPrompt string   // markdown body (instructions for the agent)
	Path         string   // source file path
}

// ParseFile reads a markdown agent definition.
func ParseFile(path string) (*Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSuffix(filepath.Base(path), ".md")
	body := string(data)
	a := &Agent{Name: name, Path: path, Model: "inherit", SystemPrompt: body}

	fm, rest, ok := splitFrontmatter(body)
	if ok {
		a.SystemPrompt = rest
		parseFrontmatter(fm, a)
	}
	return a, nil
}

// Discover finds all .md agent files in the given directories.
func Discover(dirs ...string) ([]*Agent, error) {
	byName := make(map[string]*Agent)
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			a, err := ParseFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			byName[a.Name] = a
		}
	}
	result := make([]*Agent, 0, len(byName))
	for _, a := range byName {
		result = append(result, a)
	}
	return result, nil
}

func splitFrontmatter(content string) (string, string, bool) {
	if !strings.HasPrefix(content, "---") {
		return "", content, false
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content, false
	}
	return strings.TrimSpace(rest[:idx]), strings.TrimSpace(rest[idx+4:]), true
}

func parseFrontmatter(fm string, a *Agent) {
	for _, line := range strings.Split(fm, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "name":
			a.Name = val
		case "description":
			a.Description = val
		case "model":
			a.Model = val
		case "color":
			a.Color = val
		case "tools":
			a.Tools = parseList(val)
		}
	}
}

func parseList(s string) []string {
	s = strings.Trim(s, "[]")
	var result []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
