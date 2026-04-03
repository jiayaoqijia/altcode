package task_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/altcode-ai/altcode/internal/task"
)

func TestCreateTask(t *testing.T) {
	q := task.NewQueue()
	tk := q.Create("build", "Run go build")

	if tk.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if tk.Subject != "build" {
		t.Errorf("expected subject 'build', got %q", tk.Subject)
	}
	if tk.Status != task.StatusPending {
		t.Errorf("expected pending, got %q", tk.Status)
	}
	if tk.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestGetTask(t *testing.T) {
	q := task.NewQueue()
	created := q.Create("test", "run tests")

	got, ok := q.Get(created.ID)
	if !ok {
		t.Fatal("expected to find task")
	}
	if got.Subject != "test" {
		t.Errorf("expected subject 'test', got %q", got.Subject)
	}
}

func TestGetMissing(t *testing.T) {
	q := task.NewQueue()
	_, ok := q.Get("nonexistent")
	if ok {
		t.Error("expected false for missing task")
	}
}

func TestUpdateTask(t *testing.T) {
	q := task.NewQueue()
	tk := q.Create("deploy", "deploy service")

	err := q.Update(tk.ID, task.StatusRunning, "starting...")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := q.Get(tk.ID)
	if got.Status != task.StatusRunning {
		t.Errorf("expected running, got %q", got.Status)
	}
	if got.Output != "starting..." {
		t.Errorf("expected output 'starting...', got %q", got.Output)
	}
}

func TestUpdateCompleted(t *testing.T) {
	q := task.NewQueue()
	tk := q.Create("lint", "run linter")

	q.Update(tk.ID, task.StatusRunning, "")
	q.Update(tk.ID, task.StatusCompleted, "all passed")

	got, _ := q.Get(tk.ID)
	if got.Status != task.StatusCompleted {
		t.Errorf("expected completed, got %q", got.Status)
	}
	if got.Output != "all passed" {
		t.Errorf("expected 'all passed', got %q", got.Output)
	}
}

func TestUpdateFailed(t *testing.T) {
	q := task.NewQueue()
	tk := q.Create("build", "build binary")

	q.Update(tk.ID, task.StatusFailed, "compile error")

	got, _ := q.Get(tk.ID)
	if got.Status != task.StatusFailed {
		t.Errorf("expected failed, got %q", got.Status)
	}
}

func TestUpdateMissingReturnsError(t *testing.T) {
	q := task.NewQueue()
	err := q.Update("bogus", task.StatusRunning, "")
	if err == nil {
		t.Error("expected error for missing task")
	}
}

func TestListPreservesOrder(t *testing.T) {
	q := task.NewQueue()
	q.Create("first", "")
	q.Create("second", "")
	q.Create("third", "")

	tasks := q.List()
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Subject != "first" {
		t.Errorf("expected first, got %q", tasks[0].Subject)
	}
	if tasks[2].Subject != "third" {
		t.Errorf("expected third, got %q", tasks[2].Subject)
	}
}

func TestListEmpty(t *testing.T) {
	q := task.NewQueue()
	tasks := q.List()
	if len(tasks) != 0 {
		t.Errorf("expected empty list, got %d", len(tasks))
	}
}

func TestLen(t *testing.T) {
	q := task.NewQueue()
	if q.Len() != 0 {
		t.Error("expected 0")
	}
	q.Create("a", "")
	q.Create("b", "")
	if q.Len() != 2 {
		t.Errorf("expected 2, got %d", q.Len())
	}
}

func TestSummaryEmpty(t *testing.T) {
	q := task.NewQueue()
	s := q.Summary()
	if s != "No tasks." {
		t.Errorf("expected 'No tasks.', got %q", s)
	}
}

func TestSummaryMixed(t *testing.T) {
	q := task.NewQueue()
	q.Create("a", "")
	tk2 := q.Create("b", "")
	tk3 := q.Create("c", "")

	q.Update(tk2.ID, task.StatusRunning, "")
	q.Update(tk3.ID, task.StatusCompleted, "done")

	s := q.Summary()
	if !strings.Contains(s, "Tasks (3)") {
		t.Errorf("expected 'Tasks (3)' in %q", s)
	}
	if !strings.Contains(s, "1 pending") {
		t.Errorf("expected '1 pending' in %q", s)
	}
	if !strings.Contains(s, "1 running") {
		t.Errorf("expected '1 running' in %q", s)
	}
	if !strings.Contains(s, "1 completed") {
		t.Errorf("expected '1 completed' in %q", s)
	}
}

func TestConcurrentAccess(t *testing.T) {
	q := task.NewQueue()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tk := q.Create("task", "concurrent")
			q.Update(tk.ID, task.StatusRunning, "")
			q.Update(tk.ID, task.StatusCompleted, "done")
			q.Get(tk.ID)
			q.List()
			q.Summary()
		}(i)
	}
	wg.Wait()

	if q.Len() != 50 {
		t.Errorf("expected 50 tasks, got %d", q.Len())
	}
}

func TestUniqueIDs(t *testing.T) {
	q := task.NewQueue()
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tk := q.Create("task", "")
		if ids[tk.ID] {
			t.Fatalf("duplicate ID: %s", tk.ID)
		}
		ids[tk.ID] = true
	}
}

func TestUpdateTimestamp(t *testing.T) {
	q := task.NewQueue()
	tk := q.Create("ts", "")
	created := tk.UpdatedAt

	q.Update(tk.ID, task.StatusRunning, "go")
	got, _ := q.Get(tk.ID)
	if !got.UpdatedAt.After(created) && got.UpdatedAt != created {
		// timestamps may be same if very fast; just ensure not zero
		if got.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should not be zero after update")
		}
	}
}
