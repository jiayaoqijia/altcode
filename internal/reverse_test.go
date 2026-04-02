package main

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "single char", input: "a", expected: "a"},
		{name: "ascii", input: "hello", expected: "olleh"},
		{name: "unicode", input: "こんにちは", expected: "はちにんこ"},
		{name: "palindrome", input: "racecar", expected: "racecar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reverse(tt.input)
			if got != tt.expected {
				t.Errorf("Reverse(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
