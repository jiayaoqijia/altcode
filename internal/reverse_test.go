package internal

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{
			name: "simple ASCII",
			s:    "hello",
			want: "olleh",
		},
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
			name: "Unicode characters",
			s:    "hello 世界",
			want: "界世 olleh",
		},
		{
			name: "palindrome",
			s:    "racecar",
			want: "racecar",
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
