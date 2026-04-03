package internal

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{
			name: "empty string",
			s:    "",
			want: "",
		},
		{
			name: "single character",
			s:    "a",
			want: "a",
		},
		{
			name: "simple string",
			s:    "hello",
			want: "olleh",
		},
		{
			name: "string with spaces",
			s:    "hello world",
			want: "dlrow olleh",
		},
		{
			name: "unicode characters",
			s:    "こんにちは",
			want: "はんちんこ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reverse(tt.s)
			if got != tt.want {
				t.Errorf("Reverse(%q) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}
