package internal

import "sync"

// Counter returns a thread-safe function that counts up each time it's called.
func Counter() func() int {
	var mu sync.Mutex
	n := 0
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		n++
		return n
	}
}
