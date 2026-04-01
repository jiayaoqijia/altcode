package provider

import "encoding/json"

// ContentPart represents a single block in a multi-part message.
type ContentPart struct {
	Type      string          `json:"type"`                        // "text", "tool_use", "tool_result"
	Text      string          `json:"text,omitempty"`              // for type "text"
	ID        string          `json:"id,omitempty"`                // tool_use ID
	Name      string          `json:"name,omitempty"`              // tool name (tool_use)
	Input     json.RawMessage `json:"input,omitempty"`             // tool input JSON (tool_use)
	ToolUseID string          `json:"tool_use_id,omitempty"`       // reference to tool_use ID (tool_result)
	Content   string          `json:"content,omitempty"`           // result text (tool_result)
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
