package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

// RunParams configures a workflow run.
type RunParams struct {
	EngineParams engine.EngineParams
	ProjectRoot  string
	Mode         Mode   // explicit mode, or empty for auto-detect
	Prompt       string // user's task description
	MaxIter      int    // max iterations for ralph (default 10)
	Writer       io.Writer
}

// Run executes the workflow pipeline. Routes to the appropriate mode
// based on keywords or explicit mode selection.
func Run(ctx context.Context, p RunParams) error {
	w := p.Writer
	if w == nil {
		w = os.Stdout
	}

	mode := p.Mode
	prompt := p.Prompt
	if mode == "" {
		mode, prompt = Route(p.Prompt)
	}
	if mode == "" {
		mode = ModeExecute // default: just execute
	}

	maxIter := p.MaxIter
	if maxIter <= 0 {
		maxIter = 10
	}

	switch mode {
	case ModeInterview:
		return runInterview(ctx, p, prompt, w)
	case ModePlan:
		return runPlan(ctx, p, prompt, w)
	case ModeRalph:
		return runRalph(ctx, p, prompt, maxIter, w)
	case ModeExecute:
		return runExecute(ctx, p, prompt, w)
	default:
		return fmt.Errorf("unknown workflow mode: %s", mode)
	}
}

func runInterview(ctx context.Context, p RunParams, task string, w io.Writer) error {
	fmt.Fprintf(w, "[workflow] Starting deep-interview for: %s\n\n", truncate(task, 80))

	st := &State{
		Mode: ModeInterview, Phase: PhaseActive,
		StartedAt: time.Now(), MaxIter: 10,
	}
	SaveState(p.ProjectRoot, st)

	sysPrompt := InterviewPrompt(task)
	p.EngineParams.Instructions = append(p.EngineParams.Instructions, config.Instruction{
		Path: "workflow/interview", Content: sysPrompt,
	})

	return drainEngine(ctx, p.EngineParams, task, w)
}

func runPlan(ctx context.Context, p RunParams, task string, w io.Writer) error {
	fmt.Fprintf(w, "[workflow] Starting consensus planning for: %s\n\n", truncate(task, 80))

	st := &State{
		Mode: ModePlan, Phase: PhaseActive,
		StartedAt: time.Now(), MaxIter: 1,
	}
	SaveState(p.ProjectRoot, st)

	sysPrompt := PlanPrompt(task)
	p.EngineParams.Instructions = append(p.EngineParams.Instructions, config.Instruction{
		Path: "workflow/plan", Content: sysPrompt,
	})

	return drainEngine(ctx, p.EngineParams, task, w)
}

func runRalph(ctx context.Context, p RunParams, task string, maxIter int, w io.Writer) error {
	fmt.Fprintf(w, "[workflow] Starting persistent execution (ralph) for: %s\n", truncate(task, 80))
	fmt.Fprintf(w, "[workflow] Max iterations: %d — will not stop until complete or blocked.\n\n", maxIter)

	for i := 1; i <= maxIter; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		st := &State{
			Mode: ModeRalph, Phase: PhaseActive,
			StartedAt: time.Now(), Iteration: i, MaxIter: maxIter,
		}
		SaveState(p.ProjectRoot, st)

		fmt.Fprintf(w, "[ralph] Iteration %d/%d\n", i, maxIter)

		sysPrompt := RalphPrompt(task, i, maxIter)
		params := p.EngineParams
		params.Instructions = append(params.Instructions, config.Instruction{
			Path: "workflow/ralph", Content: sysPrompt,
		})
		params.Messages = nil // fresh conversation each iteration

		if err := drainEngine(ctx, params, task, w); err != nil {
			fmt.Fprintf(w, "\n[ralph] Iteration %d error: %v\n", i, err)
			continue // try next iteration
		}
		fmt.Fprintf(w, "\n[ralph] Iteration %d complete.\n\n", i)
	}

	st := &State{
		Mode: ModeRalph, Phase: PhaseComplete,
		StartedAt: time.Now(), Iteration: maxIter, MaxIter: maxIter,
	}
	SaveState(p.ProjectRoot, st)

	fmt.Fprintf(w, "[ralph] All %d iterations complete.\n", maxIter)
	return nil
}

func runExecute(ctx context.Context, p RunParams, task string, w io.Writer) error {
	return drainEngine(ctx, p.EngineParams, task, w)
}

func drainEngine(ctx context.Context, params engine.EngineParams, prompt string, w io.Writer) error {
	eng, err := engine.New(params)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	ch := eng.Run(ctx, prompt)
	var lastErr string
	for ev := range ch {
		switch ev.Type {
		case event.TextDelta:
			fmt.Fprint(w, ev.Text)
		case event.ErrorEvent:
			lastErr = ev.Error
		case event.Done:
			fmt.Fprintln(w)
		}
	}
	if lastErr != "" {
		return fmt.Errorf("%s", lastErr)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
