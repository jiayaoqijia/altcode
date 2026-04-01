package command

import (
	"os"
	"path/filepath"
	"strings"
)

// ParseFile reads a markdown file and returns a Command.
func ParseFile(path string) (*Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSuffix(filepath.Base(path), ".md")
	body := string(data)

	cmd := &Command{
		Name: name,
		Path: path,
		Body: body,
	}

	fm, rest, ok := splitFrontmatter(body)
	if ok {
		cmd.Body = rest
		parseFrontmatterFields(fm, cmd)
	}

	return cmd, nil
}

// splitFrontmatter splits a markdown file at --- delimiters.
func splitFrontmatter(content string) (frontmatter, body string, ok bool) {
	if !strings.HasPrefix(content, "---") {
		return "", content, false
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content, false
	}
	fm := strings.TrimSpace(rest[:idx])
	body = strings.TrimSpace(rest[idx+4:])
	return fm, body, true
}

// parseFrontmatterFields extracts known fields from YAML-like frontmatter.
// Uses simple line parsing to avoid a yaml dependency.
func parseFrontmatterFields(fm string, cmd *Command) {
	for _, line := range strings.Split(fm, "\n") {
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		switch key {
		case "description":
			cmd.Description = val
		case "argument-hint":
			cmd.ArgumentHint = val
		case "allowed-tools":
			cmd.AllowedTools = parseCSV(val)
		}
	}
}

func splitKV(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	return key, value, true
}

func parseCSV(s string) []string {
	// Handle both "Read, Grep, Bash" and ["Read", "Grep", "Bash"]
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
