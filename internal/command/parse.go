package command

import (
	"os"
	"path/filepath"
	"strings"
)

// ParseFile reads a markdown file and returns a Command.
//
// Normalizes CRLF -> LF before any frontmatter parsing so .md files
// authored on Windows don't leave stray \r in field values or fail
// to find the closing --- delimiter (which the previous version
// matched as exactly "\n---" and so missed "\r\n---").
func ParseFile(path string) (*Command, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSuffix(filepath.Base(path), ".md")
	body := strings.ReplaceAll(string(data), "\r\n", "\n")

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
//
// The closing delimiter must match a whole '---' line — i.e. the
// next character after the three dashes is either end-of-string,
// a newline, or whitespace. The previous version matched any
// '\n---' substring, so a multiline body containing a line that
// merely *starts* with --- (e.g. a horizontal rule mid-doc) would
// prematurely terminate frontmatter parsing.
func splitFrontmatter(content string) (frontmatter, body string, ok bool) {
	if !strings.HasPrefix(content, "---") {
		return "", content, false
	}
	rest := content[3:]
	// Skip optional whitespace after the opening --- on its line.
	for {
		idx := strings.Index(rest, "\n---")
		if idx < 0 {
			return "", content, false
		}
		// Verify the match is on its own line: char after "---" must
		// be EOL, EOF, or whitespace.
		end := idx + 4
		if end == len(rest) || rest[end] == '\n' || rest[end] == ' ' || rest[end] == '\t' {
			fm := strings.TrimSpace(rest[:idx])
			body = strings.TrimSpace(rest[end:])
			return fm, body, true
		}
		// False match; advance past it and keep looking.
		rest = rest[idx+4:]
		// Stop if no more newlines.
		if !strings.Contains(rest, "\n---") {
			return "", content, false
		}
	}
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
