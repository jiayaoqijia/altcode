package internal

import "sync"

func Counter() func() int {
	n := 0
	mu := &sync.Mutex{}
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		n++
		return n
	}
}
