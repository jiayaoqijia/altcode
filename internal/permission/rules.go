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

	// Bash commands containing shell separators or substitution markers
	// must be split into segments and EVERY segment must independently
	// match an allow rule. Without this, a rule like "git status" would
	// approve "git status; rm -rf ~" because the matcher only saw the
	// first token. The wildcard rule "*" still matches anything.
	if toolName == "bash" && rule.Pattern != "*" && hasShellSeparator(arg) {
		segments := splitShellSegments(arg)
		for _, seg := range segments {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			if !globMatch(rule.Pattern, seg) {
				return false
			}
		}
		return len(segments) > 0
	}

	return globMatch(rule.Pattern, arg)
}

// hasShellSeparator reports whether s contains a shell command separator
// or substitution marker that would let an attacker chain extra commands
// past a permission check.
func hasShellSeparator(s string) bool {
	// Skip when the only "|" is inside a glob (e.g. {a,b|c}) — but in
	// practice glob extensions don't use |, and `find . -name "*.go"` is
	// fine because it doesn't use any separator.
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ';':
			return true
		case '|':
			return true
		case '&':
			// "&" alone is background; "&&" is and-list. Both bypass.
			return true
		case '`':
			return true
		case '\n':
			return true
		case '$':
			if i+1 < len(s) && s[i+1] == '(' {
				return true
			}
		}
	}
	return false
}

// splitShellSegments splits s on shell separators into individual
// command segments. The split is naive — it doesn't honor quoting —
// but the goal is conservative: if ANY split looks like a separate
// command, every segment must be allowlisted.
func splitShellSegments(s string) []string {
	var segments []string
	var current strings.Builder
	flush := func() {
		seg := current.String()
		current.Reset()
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ';', '\n':
			flush()
			continue
		case '|':
			// "||" or "|" both terminate a segment.
			flush()
			if i+1 < len(s) && s[i+1] == '|' {
				i++
			}
			continue
		case '&':
			flush()
			if i+1 < len(s) && s[i+1] == '&' {
				i++
			}
			continue
		}
		current.WriteByte(c)
	}
	flush()
	return segments
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
