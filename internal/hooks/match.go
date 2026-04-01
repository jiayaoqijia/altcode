package hooks

import "strings"

// matchTool checks if toolName matches the matcher pattern.
// Supports: exact ("Bash"), pipe-separated ("Write|Edit"), glob ("*").
// Matching is case-insensitive for Claude Code compatibility (PascalCase
// matchers like "Write|Edit" match altcode's lowercase tool names).
func matchTool(matcher, toolName string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}

	lowerTool := strings.ToLower(toolName)
	parts := strings.Split(matcher, "|")
	for _, p := range parts {
		if strings.ToLower(strings.TrimSpace(p)) == lowerTool {
			return true
		}
	}
	return false
}
