package workflow

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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
	return runSingleTurn(ctx, p, task, ModeInterview, 10, "workflow/interview", InterviewPrompt(task), w)
}

func runPlan(ctx context.Context, p RunParams, task string, w io.Writer) error {
	fmt.Fprintf(w, "[workflow] Starting consensus planning for: %s\n\n", truncate(task, 80))
	return runSingleTurn(ctx, p, task, ModePlan, 1, "workflow/plan", PlanPrompt(task), w)
}

func runSingleTurn(
	ctx context.Context,
	p RunParams,
	task string,
	mode Mode,
	maxIter int,
	instructionPath string,
	sysPrompt string,
	w io.Writer,
) error {
	st := &State{
		Mode: mode, Phase: PhaseActive,
		StartedAt: time.Now(), MaxIter: maxIter,
	}
	SaveState(p.ProjectRoot, st)

	params := p.EngineParams
	params.Instructions = appendInstruction(params.Instructions, instructionPath, sysPrompt)

	return drainEngine(ctx, params, task, w)
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
		params.Instructions = appendInstruction(params.Instructions, "workflow/ralph", sysPrompt)
		params.Messages = nil // fresh conversation each iteration

		text, err := drainEngineCapture(ctx, params, task, w)
		if err != nil {
			fmt.Fprintf(w, "\n[ralph] Iteration %d error: %v\n", i, err)
			continue
		}
		fmt.Fprintf(w, "\n[ralph] Iteration %d complete.\n\n", i)

		// Stop early if model signals task is done
		if isRalphComplete(text) {
			st.Phase = PhaseComplete
			st.Iteration = i
			SaveState(p.ProjectRoot, st)
			fmt.Fprintf(w, "[ralph] Task verified complete after %d iteration(s).\n", i)
			return nil
		}
	}

	st := &State{
		Mode: ModeRalph, Phase: PhaseComplete,
		StartedAt: time.Now(), Iteration: maxIter, MaxIter: maxIter,
	}
	SaveState(p.ProjectRoot, st)

	fmt.Fprintf(w, "[ralph] Reached max iterations (%d).\n", maxIter)
	return nil
}

// isRalphComplete checks if the model's response indicates the task is done.
// Looks for verification language in the output.
func isRalphComplete(text string) bool {
	lower := strings.ToLower(text)
	signals := []string{
		"all tests pass",
		"already pass",
		"nothing to fix",
		"no failing tests",
		"task complete",
		"all verified",
		"everything passes",
		"no issues found",
		"already green",
		"verified",
		"output is exactly",
		"no code changes were needed",
		"completed successfully",
	}
	for _, s := range signals {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

func runExecute(ctx context.Context, p RunParams, task string, w io.Writer) error {
	return drainEngine(ctx, p.EngineParams, task, w)
}

func drainEngine(ctx context.Context, params engine.EngineParams, prompt string, w io.Writer) error {
	_, err := drainEngineCapture(ctx, params, prompt, w)
	return err
}

func drainEngineCapture(ctx context.Context, params engine.EngineParams, prompt string, w io.Writer) (string, error) {
	eng, err := engine.New(params)
	if err != nil {
		return "", fmt.Errorf("create engine: %w", err)
	}

	ch := eng.Run(ctx, prompt)
	var lastErr string
	var sb strings.Builder
	for ev := range ch {
		switch ev.Type {
		case event.TextDelta:
			fmt.Fprint(w, ev.Text)
			sb.WriteString(ev.Text)
		case event.ErrorEvent:
			lastErr = ev.Error
		case event.Done:
			fmt.Fprintln(w)
		}
	}
	if lastErr != "" {
		return sb.String(), fmt.Errorf("%s", lastErr)
	}
	return sb.String(), nil
}

func appendInstruction(base []config.Instruction, path, content string) []config.Instruction {
	cp := make([]config.Instruction, len(base), len(base)+1)
	copy(cp, base)
	return append(cp, config.Instruction{Path: path, Content: content})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
