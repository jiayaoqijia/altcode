package feishu

import (
	"encoding/json"
	"regexp"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// mentionPlaceholderRegex matches @_user_N placeholders.
var mentionPlaceholderRegex = regexp.MustCompile(`@_user_\d+`)

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func buildMarkdownCard(content string) (string, error) {
	card := map[string]any{
		"schema": "2.0",
		"body": map[string]any{
			"elements": []map[string]any{
				{
					"tag":     "markdown",
					"content": content,
				},
			},
		},
	}
	data, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func extractJSONStringField(content, field string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func extractImageKey(content string) string {
	return extractJSONStringField(content, "image_key")
}

func extractFileKey(content string) string {
	return extractJSONStringField(content, "file_key")
}

func extractFileName(content string) string {
	return extractJSONStringField(content, "file_name")
}

func stripMentionPlaceholders(
	content string, mentions []*larkim.MentionEvent,
) string {
	if len(mentions) == 0 {
		return content
	}
	for _, m := range mentions {
		if m.Key != nil && *m.Key != "" {
			content = strings.ReplaceAll(content, *m.Key, "")
		}
	}
	content = mentionPlaceholderRegex.ReplaceAllString(content, "")
	return strings.TrimSpace(content)
}
