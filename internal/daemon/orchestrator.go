package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

// SpawnFunc is the function signature for spawning an agent and
// collecting its output. Tests inject a mock; production uses
// SpawnAndCollect which delegates to subprocess.go.
type SpawnFunc func(ctx context.Context, cfg AgentConfig) (output string, err error)

// OrchestratorConfig holds orchestrator parameters.
type OrchestratorConfig struct {
	SpawnFunc   SpawnFunc
	MaxFixRetry int // default 3
	Logger      *slog.Logger
}

// Orchestrator drives the Plan->Implement->Review->Finalize loop.
type Orchestrator struct {
	store  *Store
	cfg    OrchestratorConfig
	logger *slog.Logger
}

// NewOrchestrator creates an orchestrator.
func NewOrchestrator(store *Store, cfg OrchestratorConfig) *Orchestrator {
	if cfg.MaxFixRetry <= 0 {
		cfg.MaxFixRetry = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Orchestrator{store: store, cfg: cfg, logger: cfg.Logger}
}

// Plan is the structured output from the lead agent.
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// PlanStep is a single step in the plan.
type PlanStep struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// RunTask executes the full orchestration loop for a task.
func (o *Orchestrator) RunTask(ctx context.Context, task *Task) error {
	if err := o.store.MarkStarted(task.ID); err != nil {
		return fmt.Errorf("mark started: %w", err)
	}

	// Phase 1: Plan
	o.emitPhase(task.ID, "plan", "started")
	if err := o.store.UpdateStatus(task.ID, "planning"); err != nil {
		o.logger.Warn("update status", "err", err)
	}

	planOutput, err := o.cfg.SpawnFunc(ctx, AgentConfig{
		Binary: "echo",
		Args:   []string{task.TaskDescription},
		Role:   "lead",
	})
	if err != nil {
		if ferr := o.store.MarkFailed(task.ID, fmt.Sprintf("plan failed: %v", err)); ferr != nil {
			o.logger.Warn("mark failed", "err", ferr)
		}
		return fmt.Errorf("plan phase: %w", err)
	}
	o.emitPhase(task.ID, "plan", "completed")

	var plan Plan
	if jerr := json.Unmarshal([]byte(planOutput), &plan); jerr != nil || len(plan.Steps) == 0 {
		plan = Plan{Steps: []PlanStep{{
			Description: "implement",
			Prompt:      task.TaskDescription,
		}}}
	}

	// Emit spec event after plan phase for editable confirmation.
	o.emitSpec(task.ID, &plan)

	// Phase 2: Implement (per step with retry loop)
	o.emitPhase(task.ID, "implement", "started")
	if err := o.store.UpdateStatus(task.ID, "implementing"); err != nil {
		o.logger.Warn("update status", "err", err)
	}

	for i, step := range plan.Steps {
		var lastErr error
		for attempt := 0; attempt < o.cfg.MaxFixRetry; attempt++ {
			_, lastErr = o.cfg.SpawnFunc(ctx, AgentConfig{
				Role: "implementer",
				Args: []string{step.Prompt},
			})
			if lastErr == nil {
				o.logger.Info("step completed",
					"task", task.ID, "step", i, "attempt", attempt)
				break
			}
			o.logger.Warn("step attempt failed, retrying",
				"task", task.ID, "step", i, "attempt", attempt,
				"err", lastErr)
		}
		if lastErr != nil {
			msg := fmt.Sprintf("step %d failed after %d attempts: %v",
				i, o.cfg.MaxFixRetry, lastErr)
			if ferr := o.store.MarkFailed(task.ID, msg); ferr != nil {
				o.logger.Warn("mark failed", "err", ferr)
			}
			return fmt.Errorf("implement phase: %s", msg)
		}
	}
	o.emitPhase(task.ID, "implement", "completed")

	// Phase 3: Review
	o.emitPhase(task.ID, "review", "started")
	if err := o.store.UpdateStatus(task.ID, "reviewing"); err != nil {
		o.logger.Warn("update status", "err", err)
	}

	_, err = o.cfg.SpawnFunc(ctx, AgentConfig{Role: "reviewer"})
	if err != nil {
		o.logger.Warn("review failed, continuing", "err", err)
	}
	o.emitPhase(task.ID, "review", "completed")

	// Phase 4: Finalize
	o.emitPhase(task.ID, "finalize", "started")
	if err := o.store.MarkCompleted(task.ID); err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}
	o.emitPhase(task.ID, "finalize", "completed")

	return nil
}

func (o *Orchestrator) emitPhase(taskID, phase, action string) {
	data, _ := json.Marshal(map[string]string{
		"phase":  phase,
		"action": action,
	})
	o.store.AppendEvent(taskID, "phase_"+action, string(data))
}

// emitSpec records a spec event with current and target state
// extracted from the plan. The frontend uses this for the editable
// spec confirmation flow (#28).
func (o *Orchestrator) emitSpec(taskID string, plan *Plan) {
	spec := map[string]any{
		"current_state": []string{"Repository analyzed"},
		"target_state":  extractTargetState(plan),
	}
	data, _ := json.Marshal(spec)
	o.store.AppendEvent(taskID, "spec", string(data))
}

// extractTargetState builds a list of target descriptions from
// the plan steps.
func extractTargetState(plan *Plan) []string {
	if plan == nil || len(plan.Steps) == 0 {
		return []string{}
	}
	targets := make([]string, len(plan.Steps))
	for i, s := range plan.Steps {
		targets[i] = s.Description
	}
	return targets
}
