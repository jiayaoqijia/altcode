package engine

import "testing"

func TestParseModel(t *testing.T) {
	tests := []struct {
		input    string
		wantProv string
		wantMod  string
	}{
		{"claude-3-opus", "anthropic", "claude-3-opus"},
		{"altllm/altllm-basic", "altllm", "altllm-basic"},
		{"altllm-basic", "altllm", "altllm-basic"},
		{"altllm-standard", "altllm", "altllm-standard"},
		{"deepseek-v3", "deepseek", "deepseek-v3"},
		{"deepseek/deepseek-chat", "deepseek", "deepseek-chat"},
		{"moonshot-v1-8k", "moonshot", "moonshot-v1-8k"},
		{"minimax-01", "minimax", "minimax-01"},
		{"qwen-turbo", "qwen", "qwen-turbo"},
		{"glm-4", "glm", "glm-4"},
		{"openai/gpt-4o", "openai", "gpt-4o"},
		{"gpt-4o", "anthropic", "gpt-4o"}, // no prefix match → default
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			prov, mod := parseModel(tt.input)
			if prov != tt.wantProv || mod != tt.wantMod {
				t.Errorf("parseModel(%q) = (%q, %q), want (%q, %q)",
					tt.input, prov, mod, tt.wantProv, tt.wantMod)
			}
		})
	}
}
