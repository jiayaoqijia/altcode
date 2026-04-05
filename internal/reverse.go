package internal

import "unicode/utf8"

// Reverse returns a new string with the runes in reverse order.
// It properly handles Unicode characters.
func Reverse(s string) string {
	runes := make([]rune, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		runes = append(runes, r)
	}
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}
