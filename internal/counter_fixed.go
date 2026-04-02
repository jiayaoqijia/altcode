package altcode

import "sync"

func Counter() func() int {
	count := 0
	mu := sync.Mutex{}
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		count++
		return count
	}
}
