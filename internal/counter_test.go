package internal

import (
	"sync"
	"testing"
)

// TestCounter_SingleGoroutine tests the counter with sequential calls
func TestCounter_SingleGoroutine(t *testing.T) {
	counter := Counter()

	for i := 1; i <= 5; i++ {
		if got := counter(); got != i {
			t.Errorf("counter() = %d, want %d", got, i)
		}
	}
}

// TestCounter_MultipleGoroutines tests the counter with concurrent access
// This would fail with the original non-thread-safe implementation
func TestCounter_MultipleGoroutines(t *testing.T) {
	counter := Counter()
	numGoroutines := 100
	callsPerGoroutine := 100
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[int]int)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				val := counter()
				mu.Lock()
				results[val]++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Verify we got all unique values from 1 to numGoroutines*callsPerGoroutine
	expectedTotal := numGoroutines * callsPerGoroutine
	if len(results) != expectedTotal {
		t.Errorf("got %d unique values, want %d", len(results), expectedTotal)
	}

	// Verify each value appears exactly once (no duplicates or missing values)
	for i := 1; i <= expectedTotal; i++ {
		if count, exists := results[i]; !exists || count != 1 {
			t.Errorf("value %d: count=%d, want 1", i, count)
		}
	}
}

// TestCounter_ConcurrentIncrement tests that increments are atomic
func TestCounter_ConcurrentIncrement(t *testing.T) {
	counter := Counter()
	numGoroutines := 50
	callsPerGoroutine := 200
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				counter()
			}
		}()
	}

	wg.Wait()

	// Final call should return the total number of increments
	finalValue := counter()
	expectedValue := numGoroutines*callsPerGoroutine + 1
	if finalValue != expectedValue {
		t.Errorf("final counter value = %d, want %d", finalValue, expectedValue)
	}
}
