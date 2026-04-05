package internal

import "sync"

// Counter returns a thread-safe counter function.
// The returned function increments and returns the counter value.
// Safe for concurrent use by multiple goroutines.
func Counter() func() int {
	n := 0
	var mu sync.Mutex
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		n++
		return n
	}
}
