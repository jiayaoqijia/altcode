package tui

import "testing"

func TestLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"/help", "/history", "/h"},
		{"/compact", "/cost", "/co"},
		{"/status", "/stats", "/stat"},
		{"/help", "/help", "/help"},
		{"", "/help", ""},
		{"/a", "/b", "/"},
	}
	for _, tt := range tests {
		got := longestCommonPrefix(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("longestCommonPrefix(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}
