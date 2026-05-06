package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/config"
	"github.com/jiayaoqijia/altcode/internal/engine"
	"github.com/jiayaoqijia/altcode/internal/event"
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

// runSingleTurn handles interview/plan modes: save state, inject prompt, run once.
func runSingleTurn(ctx context.Context, p RunParams, mode Mode, label, task, sysPrompt string, maxIter int, w io.Writer) error {
	fmt.Fprintf(w, "[workflow] Starting %s for: %s\n\n", label, truncate(task, 80))
	saveStateOrWarn(w, p.ProjectRoot, &State{
		Mode: mode, Phase: PhaseActive,
		StartedAt: time.Now(), MaxIter: maxIter,
	})
	params := p.EngineParams
	params.Instructions = appendInstruction(params.Instructions, "workflow/"+string(mode), sysPrompt)
	return drainEngine(ctx, params, task, w)
}

// saveStateOrWarn persists state and surfaces any error to the writer.
// Workflow execution continues even if state persistence fails — losing
// state observability is preferable to aborting an in-flight task.
func saveStateOrWarn(w io.Writer, root string, st *State) {
	if err := SaveState(root, st); err != nil {
		fmt.Fprintf(w, "[workflow] warning: failed to save %s state: %v\n", st.Mode, err)
	}
}

func runInterview(ctx context.Context, p RunParams, task string, w io.Writer) error {
	return runSingleTurn(ctx, p, ModeInterview, "deep-interview", task, InterviewPrompt(task), 10, w)
}

func runPlan(ctx context.Context, p RunParams, task string, w io.Writer) error {
	return runSingleTurn(ctx, p, ModePlan, "consensus planning", task, PlanPrompt(task), 1, w)
}

func runRalph(ctx context.Context, p RunParams, task string, maxIter int, w io.Writer) error {
	fmt.Fprintf(w, "[workflow] Starting persistent execution (ralph) for: %s\n", truncate(task, 80))
	fmt.Fprintf(w, "[workflow] Max iterations: %d — will not stop until complete or blocked.\n\n", maxIter)

	startedAt := time.Now()
	var lastText string
	for i := 1; i <= maxIter; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Re-read disk state at the top of every iteration so
		// external /wf-pause / /wf-cancel / /wf-resume commands are
		// actually honoured. Previously the runner blindly rewrote
		// PhaseActive every turn, losing operator transitions —
		// Codex round-P/Q adversarial finding. The operator-set
		// phase (paused/cancelled) short-circuits the iteration.
		if existing, err := LoadState(p.ProjectRoot, ModeRalph); err == nil && existing != nil {
			switch existing.Phase {
			case PhaseCancelled:
				fmt.Fprintf(w, "[ralph] Cancelled by operator at iteration %d.\n", i)
				return nil
			case PhasePaused:
				// Wait for pause to lift (polled every 2s) or ctx to
				// cancel. This is cooperative — we don't force a
				// transition to active; the operator calls /wf-resume.
				fmt.Fprintf(w, "[ralph] Paused by operator at iteration %d; waiting for /wf-resume...\n", i)
				for {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(2 * time.Second):
					}
					current, err := LoadState(p.ProjectRoot, ModeRalph)
					if err != nil || current == nil {
						break
					}
					if current.Phase == PhaseCancelled {
						fmt.Fprintf(w, "[ralph] Cancelled during pause.\n")
						return nil
					}
					if current.Phase != PhasePaused {
						fmt.Fprintf(w, "[ralph] Resumed at iteration %d.\n", i)
						break
					}
				}
			}
		}

		st := &State{
			Mode: ModeRalph, Phase: PhaseActive,
			StartedAt: startedAt, Iteration: i, MaxIter: maxIter,
		}
		saveStateOrWarn(w, p.ProjectRoot, st)

		fmt.Fprintf(w, "[ralph] Iteration %d/%d\n", i, maxIter)

		sysPrompt := RalphPrompt(task, i, maxIter)
		params := p.EngineParams
		params.Instructions = appendInstruction(params.Instructions, "workflow/ralph", sysPrompt)
		params.Messages = nil // fresh conversation each iteration

		text, err := drainEngineCapture(ctx, params, task, w)
		lastText = text
		if err != nil {
			fmt.Fprintf(w, "\n[ralph] Iteration %d error: %v\n", i, err)
			// Persist the iteration error so /wf-status can surface it
			// instead of showing a stale "active" with no explanation.
			st.Error = fmt.Sprintf("iteration %d: %v", i, err)
			st.Context = text
			saveStateOrWarn(w, p.ProjectRoot, st)
			continue
		}
		fmt.Fprintf(w, "\n[ralph] Iteration %d complete.\n\n", i)

		// Stop early if model signals task is done
		if isRalphComplete(text) {
			st.Phase = PhaseComplete
			st.Iteration = i
			st.Context = text
			st.Error = ""
			saveStateOrWarn(w, p.ProjectRoot, st)
			fmt.Fprintf(w, "[ralph] Task verified complete after %d iteration(s).\n", i)
			return nil
		}
	}

	// Reached max iterations without an explicit complete signal — record
	// the final iteration's output so the user can inspect what happened.
	saveStateOrWarn(w, p.ProjectRoot, &State{
		Mode: ModeRalph, Phase: PhaseComplete,
		StartedAt: startedAt, Iteration: maxIter, MaxIter: maxIter,
		Context: lastText,
	})

	fmt.Fprintf(w, "[ralph] Reached max iterations (%d).\n", maxIter)
	return nil
}

// isRalphComplete checks if the model's response indicates the task is done.
// Primary: looks for structured JSON signal {"done": true|false}.
// Fallback: text-based signals for models that don't emit JSON.
func isRalphComplete(text string) bool {
	// Primary: parse JSON signal
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "\"done\"") {
			continue
		}
		var sig struct {
			Done   bool   `json:"done"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal([]byte(line), &sig) == nil {
			return sig.Done
		}
	}

	// Fallback: text signals for models that didn't emit JSON
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
