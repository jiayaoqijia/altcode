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

	// Proper '**' handling: pattern 'a/**/b' must match prefix 'a/' AND
	// suffix 'b'. Previously we only checked the prefix and threw away
	// the suffix, so '/safe/**/file.go' permitted '/safe/anything.txt'.
	// That's a security bug for any path-scoped rule.
	if strings.Contains(pattern, "**") {
		segs := strings.Split(pattern, "**")
		if len(segs) == 2 {
			prefix, suffix := segs[0], segs[1]
			if strings.HasPrefix(value, prefix) && strings.HasSuffix(value, suffix) &&
				len(value) >= len(prefix)+len(suffix) {
				return true
			}
		}
	}

	// Handle space-separated command patterns (e.g. "git *" matches "git status").
	// Use Fields (not SplitN) so multiple spaces / tabs collapse correctly —
	// 'git   diff' was previously a false negative against rule 'git diff *'.
	if strings.Contains(pattern, " ") {
		parts := strings.Fields(pattern)
		valueParts := strings.Fields(value)
		if len(valueParts) >= 1 && len(parts) >= 1 && parts[0] == valueParts[0] {
			if len(parts) == 1 {
				return true
			}
			if len(parts) > 1 && parts[1] == "*" {
				return true
			}
			if len(parts) > 1 && len(valueParts) > 1 {
				// Recurse on the joined tail so multi-token patterns
				// like 'git diff --cached' still work.
				return globMatch(strings.Join(parts[1:], " "), strings.Join(valueParts[1:], " "))
			}
		}
	}

	return pattern == value
}
