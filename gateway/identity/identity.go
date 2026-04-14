// Package identity provides unified user identity utilities.
// Adapted from github.com/jiayaoqijia/ottie/pkg/identity — MIT License.
package identity

import "strings"

// SenderInfo holds sender identification for allow-list matching.
type SenderInfo struct {
	CanonicalID string
	PlatformID  string
	Username    string
	Platform    string
}

// BuildCanonicalID constructs a canonical "platform:id" identifier.
func BuildCanonicalID(platform, platformID string) string {
	p := strings.ToLower(strings.TrimSpace(platform))
	id := strings.TrimSpace(platformID)
	if p == "" || id == "" {
		return ""
	}
	return p + ":" + id
}

// ParseCanonicalID splits a canonical ID ("platform:id") into its parts.
func ParseCanonicalID(canonical string) (platform, id string, ok bool) {
	canonical = strings.TrimSpace(canonical)
	idx := strings.Index(canonical, ":")
	if idx <= 0 || idx == len(canonical)-1 {
		return "", "", false
	}
	return canonical[:idx], canonical[idx+1:], true
}

// MatchAllowed checks whether the given sender matches a single allow-list entry.
func MatchAllowed(sender SenderInfo, allowed string) bool {
	allowed = strings.TrimSpace(allowed)
	if allowed == "" {
		return false
	}

	if platform, id, ok := ParseCanonicalID(allowed); ok {
		if !isNumeric(platform) {
			candidate := BuildCanonicalID(platform, id)
			if candidate != "" && sender.CanonicalID != "" {
				return strings.EqualFold(sender.CanonicalID, candidate)
			}
			return strings.EqualFold(platform, sender.Platform) &&
				sender.PlatformID == id
		}
	}

	isAtUsername := strings.HasPrefix(allowed, "@")
	trimmed := strings.TrimPrefix(allowed, "@")

	allowedID := trimmed
	allowedUser := ""
	if idx := strings.Index(trimmed, "|"); idx > 0 {
		allowedID = trimmed[:idx]
		allowedUser = trimmed[idx+1:]
	}

	if sender.PlatformID != "" && sender.PlatformID == allowedID {
		return true
	}
	if isAtUsername && sender.Username != "" && sender.Username == trimmed {
		return true
	}
	if allowedUser != "" && sender.PlatformID != "" && sender.PlatformID == allowedID {
		return true
	}
	if allowedUser != "" && sender.Username != "" && sender.Username == allowedUser {
		return true
	}

	return false
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
