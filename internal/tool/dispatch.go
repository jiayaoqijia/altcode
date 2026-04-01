package tool

import (
	"context"
	"sync"
)

// PartitionByConcurrency groups calls into batches where concurrent-safe
// calls are batched together and non-safe calls form single-item batches.
func PartitionByConcurrency(calls []Call) [][]Call {
	if len(calls) == 0 {
		return nil
	}

	var batches [][]Call
	current := []Call{calls[0]}
	currentSafe := calls[0].Tool.IsConcurrencySafe()

	for _, call := range calls[1:] {
		safe := call.Tool.IsConcurrencySafe()
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
func Dispatch(ctx context.Context, calls []Call) []Result {
	batches := PartitionByConcurrency(calls)
	var results []Result

	for _, batch := range batches {
		if len(batch) == 1 || !batch[0].Tool.IsConcurrencySafe() {
			for _, call := range batch {
				if call.EagerResult != nil {
					results = append(results, *call.EagerResult)
					continue
				}
				r, err := call.Tool.Execute(ctx, call.Input)
				if err != nil {
					results = append(results, Result{
						Error: err, Title: call.Tool.Name(),
					})
				} else {
					results = append(results, *r)
				}
			}
		} else {
			batchResults := make([]Result, len(batch))
			var wg sync.WaitGroup
			for i, call := range batch {
				if call.EagerResult != nil {
					batchResults[i] = *call.EagerResult
					continue
				}
				wg.Add(1)
				go func(idx int, c Call) {
					defer wg.Done()
					r, err := c.Tool.Execute(ctx, c.Input)
					if err != nil {
						batchResults[idx] = Result{
							Error: err, Title: c.Tool.Name(),
						}
					} else {
						batchResults[idx] = *r
					}
				}(i, call)
			}
			wg.Wait()
			results = append(results, batchResults...)
		}
	}
	return results
}
