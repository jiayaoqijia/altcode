package permission

import (
	"path/filepath"
	"strings"
)

func matchRule(rule Rule, toolName, pattern string) bool {
	if rule.Tool != "*" && rule.Tool != toolName {
		return false
	}

	arg := pattern
	if idx := strings.Index(pattern, ":"); idx >= 0 {
		arg = pattern[idx+1:]
	}

	return globMatch(rule.Pattern, arg)
}

func globMatch(pattern, value string) bool {
	if pattern == "*" {
		return true
	}

	matched, err := filepath.Match(pattern, value)
	if err == nil && matched {
		return true
	}

	if strings.Contains(pattern, "**") {
		prefix := strings.Split(pattern, "**")[0]
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}

	// Handle space-separated command patterns (e.g. "git *" matches "git status")
	if strings.Contains(pattern, " ") {
		parts := strings.SplitN(pattern, " ", 2)
		valueParts := strings.SplitN(value, " ", 2)
		if len(valueParts) >= 1 && parts[0] == valueParts[0] {
			if len(parts) > 1 && parts[1] == "*" {
				return true
			}
			if len(parts) > 1 && len(valueParts) > 1 {
				return globMatch(parts[1], valueParts[1])
			}
		}
	}

	return pattern == value
}
