package internal

import "strings"

// FilterByPrefix returns a slice of strings that start with the given prefix.
func FilterByPrefix(strs []string, prefix string) []string {
	var result []string
	for _, s := range strs {
		if strings.HasPrefix(s, prefix) {
			result = append(result, s)
		}
	}
	return result
}
