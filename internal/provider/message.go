package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// ContentPart represents a single block in a multi-part message.
// Supported types: "text", "tool_use", "tool_result", "image".
type ContentPart struct {
	Type      string          `json:"type"`                  // "text", "tool_use", "tool_result", "image"
	Text      string          `json:"text,omitempty"`        // for type "text"
	ID        string          `json:"id,omitempty"`          // tool_use ID
	Name      string          `json:"name,omitempty"`        // tool name (tool_use)
	Input     json.RawMessage `json:"input,omitempty"`       // tool input JSON (tool_use)
	ToolUseID string          `json:"tool_use_id,omitempty"` // reference to tool_use ID (tool_result)
	Content   string          `json:"content,omitempty"`     // result text (tool_result)
	Source    *ImageSource    `json:"source,omitempty"`      // for type "image" — Anthropic shape
}

// ImageSource matches Anthropic's image source schema. The struct is
// named after the JSON field and serializes directly through the
// anthropic.go toAnthropicMessage path. OpenAI's format is different
// ({"type":"image_url","image_url":{"url":"..."}}) — an OpenAI
// translator is future work (Phase 5 ships Anthropic multimodal only).
type ImageSource struct {
	Type      string `json:"type"`                 // "base64" or "url"
	MediaType string `json:"media_type,omitempty"` // "image/png", "image/jpeg", ...
	Data      string `json:"data,omitempty"`       // base64-encoded bytes (for type "base64")
	URL       string `json:"url,omitempty"`        // image URL (for type "url")
}

// Message is a single turn in the conversation history.
// If Parts is non-empty, it takes precedence over Content for serialization.
type Message struct {
	Role    string        `json:"role"`
	Content string        `json:"content,omitempty"`
	Parts   []ContentPart `json:"parts,omitempty"`
}

// TextMessage creates a simple text message.
func TextMessage(role, text string) Message {
	return Message{Role: role, Content: text}
}

// ToolResultMessage creates a message containing tool results.
func ToolResultMessage(results []ContentPart) Message {
	return Message{Role: "user", Parts: results}
}

// NewToolResultPart creates a tool_result content part.
func NewToolResultPart(toolUseID, content string) ContentPart {
	return ContentPart{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
	}
}

// NewTextPart creates a text content part.
func NewTextPart(text string) ContentPart {
	return ContentPart{Type: "text", Text: text}
}

// NewImagePartFromFile reads an image file, detects its MIME type
// from the first 512 bytes (net/http.DetectContentType), base64-encodes
// the contents, and returns an Anthropic-shaped image ContentPart.
//
// Max supported size is Phase 5's cap of 20 MB; anything larger errors
// out so users don't accidentally send a 100 MB screenshot through the
// API. Anthropic's own limit is 5 MB per image at time of writing, but
// they may raise it — we leave headroom.
func NewImagePartFromFile(path string) (ContentPart, error) {
	const maxImageBytes = 20 * 1024 * 1024
	data, err := os.ReadFile(path)
	if err != nil {
		return ContentPart{}, fmt.Errorf("read image %q: %w", path, err)
	}
	if len(data) > maxImageBytes {
		return ContentPart{}, fmt.Errorf(
			"image %q is %d bytes (max %d bytes / 20 MB)",
			path, len(data), maxImageBytes)
	}
	return NewImagePartFromBytes(data)
}

// NewImagePartFromBytes builds an image ContentPart from raw bytes.
// Used by NewImagePartFromFile and by stdin-fed images.
func NewImagePartFromBytes(data []byte) (ContentPart, error) {
	if len(data) == 0 {
		return ContentPart{}, fmt.Errorf("image data is empty")
	}
	// DetectContentType uses the first 512 bytes and returns
	// e.g. "image/png", "image/jpeg", "image/gif", "image/webp".
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	mime := http.DetectContentType(head)
	if !strings.HasPrefix(mime, "image/") {
		return ContentPart{}, fmt.Errorf(
			"not an image (detected %q); supported: image/png, image/jpeg, image/gif, image/webp",
			mime)
	}
	return ContentPart{
		Type: "image",
		Source: &ImageSource{
			Type:      "base64",
			MediaType: mime,
			Data:      base64.StdEncoding.EncodeToString(data),
		},
	}, nil
}
