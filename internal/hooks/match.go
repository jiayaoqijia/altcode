package hooks

import "strings"

// matchTool checks if toolName matches the matcher pattern.
// Supports: exact ("Bash"), pipe-separated ("Write|Edit"), glob ("*").
func matchTool(matcher, toolName string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}

	parts := strings.Split(matcher, "|")
	for _, p := range parts {
		if strings.TrimSpace(p) == toolName {
			return true
		}
	}
	return false
}
