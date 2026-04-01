package provider

// Message is a single turn in the conversation history.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
}
