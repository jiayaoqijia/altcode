package tool

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

// runOne executes a single tool call defensively. It traps panics so
// one buggy tool can't crash the engine, and turns nil-result returns
// into a clear error instead of dereferencing nil. Used by both the
// sequential and the concurrent dispatch branches.
func runOne(ctx context.Context, call Call) (out Result) {
	if call.EagerResult != nil {
		return *call.EagerResult
	}
	defer func() {
		if r := recover(); r != nil {
			name := "unknown"
			if call.Tool != nil {
				name = call.Tool.Name()
			}
			out = Result{
				Title:  name,
				Output: fmt.Sprintf("Error: tool panicked: %v", r),
				Error:  fmt.Errorf("tool %q panicked: %v\n%s", name, r, debug.Stack()),
			}
		}
	}()
	r, err := call.Tool.Execute(ctx, call.Input)
	if err != nil {
		return Result{Error: err, Title: call.Tool.Name()}
	}
	if r == nil {
		return Result{
			Error: fmt.Errorf("tool %q returned nil result without error", call.Tool.Name()),
			Title: call.Tool.Name(),
		}
	}
	return *r
}

// PartitionByConcurrency groups calls into batches where concurrent-safe
// calls are batched together and non-safe calls form single-item batches.
func isConcurrencySafe(c Call) bool {
	if c.Tool == nil || c.EagerResult != nil {
		return false // nil tools and eager results run sequentially
	}
	return c.Tool.IsConcurrencySafe()
}

func PartitionByConcurrency(calls []Call) [][]Call {
	if len(calls) == 0 {
		return nil
	}

	var batches [][]Call
	current := []Call{calls[0]}
	currentSafe := isConcurrencySafe(calls[0])

	for _, call := range calls[1:] {
		safe := isConcurrencySafe(call)
		if safe && currentSafe {
			current = append(current, call)
		} else {
			batches = append(batches, current)
			current = []Call{call}
			currentSafe = safe
		}
	}
	batches = append(batches, current)
	return batches
}

// Dispatch executes tool calls respecting concurrency constraints.
// Both branches go through runOne so panic recovery and nil-result
// handling apply uniformly.
func Dispatch(ctx context.Context, calls []Call) []Result {
	batches := PartitionByConcurrency(calls)
	var results []Result

	for _, batch := range batches {
		if len(batch) == 1 || !isConcurrencySafe(batch[0]) {
			for _, call := range batch {
				results = append(results, runOne(ctx, call))
			}
			continue
		}
		batchResults := make([]Result, len(batch))
		var wg sync.WaitGroup
		for i, call := range batch {
			wg.Add(1)
			go func(idx int, c Call) {
				defer wg.Done()
				batchResults[idx] = runOne(ctx, c)
			}(i, call)
		}
		wg.Wait()
		results = append(results, batchResults...)
	}
	return results
}
