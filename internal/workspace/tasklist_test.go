package workspace

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestTaskList_AddAndList(t *testing.T) {
	dir := t.TempDir()
	tl := NewTaskList(filepath.Join(dir, "tasks.json"))

	if err := tl.Add(&Task{ID: "t1", Title: "setup"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := tl.Add(&Task{ID: "t2", Title: "build"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	tasks := tl.List()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Status != "pending" {
		t.Errorf("expected pending, got %s", tasks[0].Status)
	}

	// Duplicate ID rejected
	if err := tl.Add(&Task{ID: "t1", Title: "dup"}); err == nil {
		t.Error("expected error for duplicate ID")
	}
}

func TestTaskList_ClaimUnblocked(t *testing.T) {
	dir := t.TempDir()
	tl := NewTaskList(filepath.Join(dir, "tasks.json"))

	_ = tl.Add(&Task{ID: "t1", Title: "first"})
	_ = tl.Add(&Task{ID: "t2", Title: "blocked", DependsOn: []string{"t1"}})

	// Claim should skip t2 (blocked) and return t1.
	task, err := tl.Claim("worker")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task == nil || task.ID != "t1" {
		t.Fatalf("expected t1, got %v", task)
	}

	// t2 should still be blocked.
	if !tl.IsBlocked("t2") {
		t.Error("t2 should be blocked")
	}
}

func TestTaskList_Complete(t *testing.T) {
	dir := t.TempDir()
	tl := NewTaskList(filepath.Join(dir, "tasks.json"))

	_ = tl.Add(&Task{ID: "t1", Title: "first"})
	_ = tl.Add(&Task{ID: "t2", Title: "second", DependsOn: []string{"t1"}})

	_, _ = tl.Claim("w1")
	if err := tl.Complete("t1", "done"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// t2 should now be unblocked.
	if tl.IsBlocked("t2") {
		t.Error("t2 should be unblocked after t1 completed")
	}

	// Claim should now return t2.
	task, err := tl.Claim("w2")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task == nil || task.ID != "t2" {
		t.Fatalf("expected t2, got %v", task)
	}
}

func TestTaskList_ConcurrentClaim(t *testing.T) {
	dir := t.TempDir()
	tl := NewTaskList(filepath.Join(dir, "tasks.json"))
	_ = tl.Add(&Task{ID: "t1", Title: "sole task"})

	const goroutines = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := 0

	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			task, err := tl.Claim("worker")
			if err != nil {
				t.Errorf("Claim error from goroutine %d: %v", id, err)
				return
			}
			if task != nil {
				mu.Lock()
				claimed++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if claimed != 1 {
		t.Errorf("expected exactly 1 claim, got %d", claimed)
	}
}
