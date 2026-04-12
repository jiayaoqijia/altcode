package exec

// Phase 9: batch runner for --prompt-each / --parallel / --retry / --bail.
//
// The batch runner reads prompts line-by-line from a file, substitutes
// each line into `{{input}}` in the prompt template, and dispatches
// the resulting prompts through exec.Run. It supports parallel
// workers, per-line retry with exponential backoff, and a bail-on-
// first-failure mode.
//
// Each worker gets its own fresh exec.Params clone (so session IDs
// don't alias) and its own engine. The top-level RunBatch aggregates
// failures and returns a single error that reflects the batch as a
// whole.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// BatchResult reports the outcome of a single prompt line.
type BatchResult struct {
	Index   int    // 0-based line index in the prompt-each file
	Prompt  string // the substituted prompt
	Err     error  // nil on success
	Retries int    // number of retries attempted (0 = first try succeeded)
}

// RunBatch reads p.PromptEach line-by-line, substitutes each line
// into p.Prompt's {{input}} placeholder, and runs the resulting
// prompts as a batch. Parallelism, retry count, and bail behavior
// are taken from p. Returns nil on full success, or an error that
// wraps the per-line failures.
//
// p.Prompt may be empty — in that case the raw line is used as the
// prompt verbatim (matches the common "one-prompt-per-line" shape).
func RunBatch(ctx context.Context, p Params) error {
	if p.PromptEach == "" {
		return NewUsageError("RunBatch called without --prompt-each")
	}
	lines, err := readBatchLines(p.PromptEach)
	if err != nil {
		return NewUsageError("--prompt-each %q: %v", p.PromptEach, err)
	}
	if len(lines) == 0 {
		fmt.Fprintln(os.Stderr, "altcode: --prompt-each file has no non-empty lines")
		return nil
	}

	parallel := p.Parallel
	if parallel <= 0 {
		parallel = 1
	}
	if parallel > len(lines) {
		parallel = len(lines)
	}

	// Shared stdout writer is NOT shared across workers because
	// parallel text output would interleave. Each worker gets its
	// own buffer and we flush in-order at the end (if sequential)
	// or with a lock (if parallel). Simpler: give each worker an
	// os.Stdout direct write and let users pipe to a file per-index
	// via --save-transcript if they need ordering.
	//
	// For Phase 9 v1 we serialize output through a mutex so
	// interleaved lines are atomic-per-run.
	var outMu sync.Mutex

	// Channels for coordination.
	jobs := make(chan int, len(lines))
	results := make(chan BatchResult, len(lines))
	var wg sync.WaitGroup

	// Run worker goroutines.
	for w := 0; w < parallel; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				line := lines[i]
				prompt := substituteBatchPrompt(p.Prompt, line)

				res := BatchResult{Index: i, Prompt: prompt}
				for attempt := 0; attempt <= p.Retry; attempt++ {
					// Each run gets a fresh exec.Params clone so
					// session IDs, preRunDirty captures, and
					// pending input parts don't alias across workers.
					work := cloneBatchParams(p, prompt)

					outMu.Lock()
					fmt.Fprintf(os.Stderr,
						"[batch %d/%d attempt %d] %s\n",
						i+1, len(lines), attempt+1,
						truncatePrompt(prompt, 80))
					outMu.Unlock()

					err := Run(ctx, work)
					if err == nil {
						res.Retries = attempt
						break
					}
					res.Err = err
					res.Retries = attempt

					if attempt < p.Retry {
						backoff := time.Duration(1<<attempt) * time.Second
						if backoff > 30*time.Second {
							backoff = 30 * time.Second
						}
						outMu.Lock()
						fmt.Fprintf(os.Stderr,
							"[batch %d/%d] retry %d/%d in %s: %v\n",
							i+1, len(lines), attempt+1, p.Retry, backoff, err)
						outMu.Unlock()
						select {
						case <-time.After(backoff):
						case <-ctx.Done():
							res.Err = ctx.Err()
							results <- res
							return
						}
					}
				}
				results <- res
				if res.Err != nil && p.Bail {
					// Bail: drain remaining jobs and exit.
					// The caller sees the first failure plus
					// `remaining` skipped lines.
					for range jobs {
					}
					return
				}
			}
		}()
	}

	// Dispatch all jobs.
	for i := range lines {
		jobs <- i
	}
	close(jobs)

	wg.Wait()
	close(results)

	// Aggregate results preserving original line order.
	ordered := make([]*BatchResult, len(lines))
	for r := range results {
		rec := r
		ordered[r.Index] = &rec
	}

	// Summary.
	var failed int
	for _, r := range ordered {
		if r == nil {
			// Skipped due to bail or cancellation.
			continue
		}
		if r.Err != nil {
			failed++
			outMu.Lock()
			fmt.Fprintf(os.Stderr,
				"[batch %d/%d FAILED] %s: %v\n",
				r.Index+1, len(lines),
				truncatePrompt(r.Prompt, 60), r.Err)
			outMu.Unlock()
		}
	}
	if failed > 0 {
		return fmt.Errorf("batch: %d of %d prompts failed", failed, len(lines))
	}
	return nil
}

// readBatchLines slurps the file at path and returns non-empty,
// non-comment lines. `#` at start = comment.
func readBatchLines(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	var lines []string
	sc := bufio.NewScanner(r)
	// Larger buffer so multi-KB prompts don't truncate.
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}

// substituteBatchPrompt replaces {{input}} in the template with the
// line. If the template is empty, the line IS the prompt.
// If the template has no {{input}} placeholder, the line is
// appended with a newline.
func substituteBatchPrompt(template, line string) string {
	if template == "" {
		return line
	}
	if strings.Contains(template, "{{input}}") {
		return strings.ReplaceAll(template, "{{input}}", line)
	}
	return template + "\n\n" + line
}

// cloneBatchParams returns a fresh Params for a single batch
// iteration. Clears fields that must not alias across workers:
//   - Prompt (replaced with the current line)
//   - SessionID (each worker gets its own new session)
//   - Engine pointer (fresh engine per worker)
//   - preRunDirty (fresh snapshot per iteration)
//   - PendingInputParts (consumed on first Run; fresh for each line)
func cloneBatchParams(p Params, prompt string) Params {
	clone := p
	clone.Prompt = prompt
	clone.PromptEach = "" // prevent recursive batch
	clone.Engine = nil
	clone.EngineParams.SessionID = ""
	clone.EngineParams.Messages = nil
	clone.EngineParams.PendingInputParts = nil
	clone.preRunDirty = ""
	// Silence the text banner for batch runs — it's noisy at scale.
	clone.Quiet = true
	return clone
}

// unused imports guard
var _ = errors.New
