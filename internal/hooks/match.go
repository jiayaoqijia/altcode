package hooks

import (
	"path/filepath"
	"strings"
)

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

// matchCondition checks if an "if" condition matches the input.
// Format: "ToolName(pattern)" where pattern is a glob matched
// against the serialized tool input. Example: "Bash(git *)".
func matchCondition(cond string, input Input) bool {
	toolName, pattern := parseCondition(cond)
	if toolName == "" {
		return true
	}
	if !strings.EqualFold(toolName, input.ToolName) {
		return false
	}
	if pattern == "" || pattern == "*" {
		return true
	}
	inputStr := string(input.ToolInput)
	matched, _ := filepath.Match(pattern, inputStr)
	if matched {
		return true
	}
	return strings.Contains(inputStr, strings.TrimSuffix(pattern, "*"))
}

// parseCondition extracts "ToolName" and "pattern" from "ToolName(pattern)".
func parseCondition(cond string) (string, string) {
	idx := strings.Index(cond, "(")
	if idx < 0 {
		return cond, ""
	}
	toolName := cond[:idx]
	rest := cond[idx+1:]
	rest = strings.TrimSuffix(rest, ")")
	return toolName, rest
}
