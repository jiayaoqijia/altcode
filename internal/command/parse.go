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
// Handles both simple (key: value) and YAML block/list formats:
//
//	description: |
//	  Multi-line text
//	allowed-tools:
//	  - Bash
//	  - Read
func parseFrontmatterFields(fm string, cmd *Command) {
	lines := strings.Split(fm, "\n")
	for i := 0; i < len(lines); i++ {
		key, val, ok := splitKV(lines[i])
		if !ok {
			continue
		}
		switch key {
		case "name":
			if val != "" {
				cmd.Name = val
			}
		case "description":
			if val == "|" || val == ">" {
				// Collect indented block
				var block []string
				for i+1 < len(lines) && len(lines[i+1]) > 0 && (lines[i+1][0] == ' ' || lines[i+1][0] == '\t') {
					i++
					block = append(block, strings.TrimSpace(lines[i]))
				}
				cmd.Description = strings.Join(block, " ")
			} else {
				cmd.Description = val
			}
		case "argument-hint":
			cmd.ArgumentHint = val
		case "allowed-tools":
			if val == "" {
				// YAML list format:  - Tool
				var tools []string
				for i+1 < len(lines) {
					next := strings.TrimSpace(lines[i+1])
					if !strings.HasPrefix(next, "- ") {
						break
					}
					i++
					tools = append(tools, strings.TrimPrefix(next, "- "))
				}
				cmd.AllowedTools = tools
			} else {
				cmd.AllowedTools = parseCSV(val)
			}
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
