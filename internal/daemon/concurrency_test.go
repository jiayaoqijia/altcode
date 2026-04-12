package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// testLogger is defined in lifecycle_test.go.

func TestConcurrencyManager_AcquireRelease(t *testing.T) {
	cm := NewConcurrencyManager(2, testLogger())
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !cm.TryAcquire("t1", cancel) {
		t.Fatal("TryAcquire should succeed for first slot")
	}
	if cm.ActiveCount() != 1 {
		t.Errorf("active = %d, want 1", cm.ActiveCount())
	}

	cm.Release("t1")
	if cm.ActiveCount() != 0 {
		t.Errorf("active after release = %d, want 0",
			cm.ActiveCount())
	}
}

func TestConcurrencyManager_MaxConcurrency(t *testing.T) {
	cm := NewConcurrencyManager(2, testLogger())

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	_, cancel3 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()
	defer cancel3()
	_ = ctx1
	_ = ctx2

	if !cm.TryAcquire("t1", cancel1) {
		t.Fatal("slot 1 should succeed")
	}
	if !cm.TryAcquire("t2", cancel2) {
		t.Fatal("slot 2 should succeed")
	}
	if cm.TryAcquire("t3", cancel3) {
		t.Fatal("slot 3 should fail when max=2")
	}
}

func TestConcurrencyManager_ReleaseIdempotent(t *testing.T) {
	cm := NewConcurrencyManager(2, testLogger())
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !cm.TryAcquire("t1", cancel) {
		t.Fatal("TryAcquire should succeed")
	}

	// First release frees the slot.
	cm.Release("t1")
	// Second release is a no-op (no panic, no double-drain).
	cm.Release("t1")

	if cm.ActiveCount() != 0 {
		t.Errorf("active = %d, want 0", cm.ActiveCount())
	}

	// Verify semaphore is still usable: we should be able to
	// acquire 2 slots after the double release.
	_, c1 := context.WithCancel(context.Background())
	_, c2 := context.WithCancel(context.Background())
	defer c1()
	defer c2()
	if !cm.TryAcquire("a", c1) {
		t.Fatal("acquire after double release should work (1)")
	}
	if !cm.TryAcquire("b", c2) {
		t.Fatal("acquire after double release should work (2)")
	}
	cm.Release("a")
	cm.Release("b")
}

func TestConcurrencyManager_Cancel(t *testing.T) {
	cm := NewConcurrencyManager(2, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	if !cm.TryAcquire("t1", cancel) {
		t.Fatal("TryAcquire should succeed")
	}

	cm.Cancel("t1")

	if ctx.Err() != context.Canceled {
		t.Errorf("context should be cancelled, got %v", ctx.Err())
	}

	// Cancel on unknown task is a no-op.
	cm.Cancel("unknown")

	cm.Release("t1")
}

func TestConcurrencyManager_ActiveCount(t *testing.T) {
	cm := NewConcurrencyManager(5, testLogger())

	cancels := make([]context.CancelFunc, 3)
	for i := range cancels {
		_, cancels[i] = context.WithCancel(context.Background())
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	cm.TryAcquire("a", cancels[0])
	cm.TryAcquire("b", cancels[1])
	cm.TryAcquire("c", cancels[2])

	if cm.ActiveCount() != 3 {
		t.Errorf("active = %d, want 3", cm.ActiveCount())
	}

	cm.Release("b")
	if cm.ActiveCount() != 2 {
		t.Errorf("active after release = %d, want 2",
			cm.ActiveCount())
	}

	cm.Release("a")
	cm.Release("c")
	if cm.ActiveCount() != 0 {
		t.Errorf("active after all released = %d, want 0",
			cm.ActiveCount())
	}
}

func TestConcurrencyManager_ConcurrentAccess(t *testing.T) {
	cm := NewConcurrencyManager(5, testLogger())

	var wg sync.WaitGroup
	var acquired atomic.Int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, cancel := context.WithCancel(context.Background())
			taskID := "task-" + string(rune('A'+id))
			if cm.TryAcquire(taskID, cancel) {
				acquired.Add(1)
				// Simulate work.
				cm.Release(taskID)
				acquired.Add(-1)
			} else {
				cancel()
			}
		}(i)
	}

	wg.Wait()

	if cm.ActiveCount() != 0 {
		t.Errorf("active after concurrent test = %d, want 0",
			cm.ActiveCount())
	}
}

func TestConcurrencyManager_QueuePosition(t *testing.T) {
	cm := NewConcurrencyManager(2, testLogger())

	// No tasks: position is 0.
	if pos := cm.QueuePosition(); pos != 0 {
		t.Errorf("empty queue position = %d, want 0", pos)
	}

	_, c1 := context.WithCancel(context.Background())
	_, c2 := context.WithCancel(context.Background())
	defer c1()
	defer c2()

	cm.TryAcquire("t1", c1)
	// 1 active, 2 max: still 0.
	if pos := cm.QueuePosition(); pos != 0 {
		t.Errorf("1 active queue position = %d, want 0", pos)
	}

	cm.TryAcquire("t2", c2)
	// 2 active = max: position is 0 (at capacity, not over).
	if pos := cm.QueuePosition(); pos != 0 {
		t.Errorf("at-capacity queue position = %d, want 0", pos)
	}

	cm.Release("t1")
	cm.Release("t2")
}
