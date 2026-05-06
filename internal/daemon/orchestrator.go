package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// taskBudget is a lightweight per-task budget tracker. Constructed
// once per RunTask invocation. maxTurns is counted at every phase /
// retry; zero means unlimited. maxCostUSD is checked against the
// rolling sum of phase costs reported via recordCost; zero means
// unlimited. The cost ceiling only bites once agents actually report
// cost back — until then, enforcement is defence-in-depth via the
// 201 response's `budget_enforced: {max_cost_usd: false}` signal
// and autoresearch iteration 4 below: accepts user-provided cost
// samples so SSE steer messages or downstream wrappers that do have
// cost telemetry can feed them in.
type taskBudget struct {
	maxTurns   int
	maxCostUSD float64
	turns      int
	costUSD    float64
}

func newTaskBudget(maxTurns int, maxCostUSD float64) *taskBudget {
	return &taskBudget{maxTurns: maxTurns, maxCostUSD: maxCostUSD}
}

func (b *taskBudget) recordTurn() error {
	b.turns++
	if b.maxTurns > 0 && b.turns > b.maxTurns {
		return fmt.Errorf(
			"budget: max_turns %d exceeded (current=%d)",
			b.maxTurns, b.turns,
		)
	}
	return nil
}

// recordCost accumulates a reported phase cost (USD) and returns an
// error if the running total exceeds maxCostUSD. Negative samples
// are clamped to 0 so a misbehaving agent can't reduce the running
// total. Autoresearch iteration 4 closes the path.
func (b *taskBudget) recordCost(usd float64) error {
	if usd < 0 {
		usd = 0
	}
	b.costUSD += usd
	if b.maxCostUSD > 0 && b.costUSD > b.maxCostUSD {
		return fmt.Errorf(
			"budget: max_cost_usd %.4f exceeded (current=%.4f)",
			b.maxCostUSD, b.costUSD,
		)
	}
	return nil
}

// failBudget marks a task failed with a budget_exceeded signal and
// appends an event so SSE/UI clients can distinguish budget-driven
// failure from a crashed agent.
func (o *Orchestrator) failBudget(taskID, phase string, budgetErr error) {
	data, _ := json.Marshal(map[string]string{
		"phase":  phase,
		"reason": budgetErr.Error(),
	})
	if err := o.store.AppendEvent(taskID, "budget_exceeded", string(data)); err != nil {
		o.logger.Warn("append budget event", "task", taskID, "err", err)
	}
	if err := o.store.MarkFailed(taskID, budgetErr.Error()); err != nil {
		o.logger.Warn("mark budget failed", "task", taskID, "err", err)
	}
}

// SpawnFunc is the function signature for spawning an agent and
// collecting its output. Tests inject a mock; production uses
// SpawnAndCollect which delegates to subprocess.go.
type SpawnFunc func(ctx context.Context, cfg AgentConfig) (output string, err error)

// OrchestratorConfig holds orchestrator parameters.
type OrchestratorConfig struct {
	SpawnFunc   SpawnFunc
	MaxFixRetry int // default 3
	Logger      *slog.Logger
	PlanModel   string // model for plan phase; default "altllm-basic"
	ImplModel   string // model for implement phase; default "altllm-basic"
	ReviewModel string // model for review phase; default "altllm-basic"
	WorkDir     string // base working directory for agent spawns
}

// Orchestrator drives the Plan->Implement->Review->Finalize loop.
type Orchestrator struct {
	store  *Store
	cfg    OrchestratorConfig
	logger *slog.Logger
}

// NewOrchestrator creates an orchestrator.
// defaultModel is the fallback model when none is configured.
const defaultModel = "altllm-basic"

func NewOrchestrator(store *Store, cfg OrchestratorConfig) *Orchestrator {
	if cfg.MaxFixRetry <= 0 {
		cfg.MaxFixRetry = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	if cfg.PlanModel == "" {
		cfg.PlanModel = defaultModel
	}
	if cfg.ImplModel == "" {
		cfg.ImplModel = defaultModel
	}
	if cfg.ReviewModel == "" {
		cfg.ReviewModel = defaultModel
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
// steerCh delivers user guidance messages from the steer API.
// The orchestrator drains pending messages before each implement
// step and prepends them to the agent prompt.
func (o *Orchestrator) RunTask(ctx context.Context, task *Task, steerCh <-chan string) error {
	if err := o.store.MarkStarted(task.ID); err != nil {
		return fmt.Errorf("mark started: %w", err)
	}

	// Per-task budget enforcement. MaxTurns is counted across every
	// SpawnFunc invocation the orchestrator makes (plan, each
	// implement step + retry, review, finalize). Zero means unlimited
	// — preserves the historical behaviour for callers that don't
	// opt into limits. MaxCostUSD is stored for future use but not
	// yet enforced because altcode subprocesses don't report cost
	// back to the daemon; the handler's 201 response surfaces that
	// gap explicitly. Autoresearch iteration 1.
	budget := newTaskBudget(task.MaxTurns, task.MaxCostUSD)

	// Per-task overrides from Task.AgentConfig (JSON). Blank fields
	// fall back to OrchestratorConfig defaults. This honours the
	// `model` field the API has always accepted but previously
	// ignored — Codex round-D flagged that as a real bug.
	_, overrideModel := decodeAgentConfig(task.AgentConfig)
	planModel := o.cfg.PlanModel
	implModel := o.cfg.ImplModel
	reviewModel := o.cfg.ReviewModel
	if overrideModel != "" {
		planModel = overrideModel
		implModel = overrideModel
		reviewModel = overrideModel
	}

	// Phase 1: Plan
	if err := budget.recordTurn(); err != nil {
		o.failBudget(task.ID, "plan", err)
		return err
	}
	o.emitPhase(task.ID, "plan", "started")
	if err := o.store.UpdateStatus(task.ID, "planning"); err != nil {
		o.logger.Warn("update status", "err", err)
	}

	planOutput, err := o.cfg.SpawnFunc(ctx, AgentConfig{
		Binary: "altcode",
		Args: []string{
			"--model", planModel,
			"You are a lead architect. Analyze this task and output " +
				"a JSON object with \"steps\" (array of objects with " +
				"\"description\" and \"prompt\" fields).\n\n" +
				WrapAsUserContent(task.TaskDescription, "TASK"),
		},
		Dir:  o.cfg.WorkDir,
		Env:  o.taskEnv(task),
		Role: "lead",
	})
	if err != nil {
		if ferr := o.store.MarkFailed(task.ID, fmt.Sprintf("plan failed: %v", err)); ferr != nil {
			o.logger.Warn("mark failed", "err", ferr)
		}
		return fmt.Errorf("plan phase: %w", err)
	}
	o.emitPhase(task.ID, "plan", "completed")
	if err := o.emitCost(task.ID, "plan", planOutput, budget); err != nil {
		o.failBudget(task.ID, "plan", err)
		return err
	}

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
		// Drain pending steer messages and prepend to prompt. Steer
		// text comes from the user API, step.Prompt comes from the
		// planner LLM — both are wrapped so the implementer treats
		// them as data, not instructions.
		stepPrompt := WrapAsUserContent(step.Prompt, "PLAN_STEP")
		if steer := o.drainSteer(steerCh); steer != "" {
			stepPrompt = WrapAsUserContent(steer, "USER_STEER") +
				"\n\n" + stepPrompt
			o.store.AppendEvent(task.ID, "steer_applied", steer)
		}
		step.Prompt = stepPrompt

		var lastErr error
		var stepOutput string
		for attempt := 0; attempt < o.cfg.MaxFixRetry; attempt++ {
			if err := budget.recordTurn(); err != nil {
				o.failBudget(task.ID, fmt.Sprintf("implement_step_%d", i), err)
				return err
			}
			stepOutput, lastErr = o.cfg.SpawnFunc(ctx, AgentConfig{
				Binary: "altcode",
				Args: []string{
					"--model", implModel,
					"--permission-mode", "auto",
				"--allow-tool", "Read",
				"--allow-tool", "Write",
				"--allow-tool", "Edit",
				"--allow-tool", "Bash",
				"--allow-tool", "Glob",
				"--allow-tool", "Grep",
					step.Prompt,
				},
				Dir:  o.cfg.WorkDir,
				Env:  o.taskEnv(task),
				Role: "implementer",
			})
			if lastErr == nil {
				o.logger.Info("step completed",
					"task", task.ID, "step", i, "attempt", attempt)
				phaseName := fmt.Sprintf("implement_step_%d", i)
				// Iter 6/7: step output contributes to the cost
				// proxy; iter 7 threads the recordCost error back up
				// so a single path handles both record + abort.
				if err := o.emitCost(task.ID, phaseName, stepOutput, budget); err != nil {
					o.failBudget(task.ID, phaseName, err)
					return err
				}
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
	if err := budget.recordTurn(); err != nil {
		o.failBudget(task.ID, "review", err)
		return err
	}
	o.emitPhase(task.ID, "review", "started")
	if err := o.store.UpdateStatus(task.ID, "reviewing"); err != nil {
		o.logger.Warn("update status", "err", err)
	}

	reviewPrompt := "Review the recent changes for bugs, security issues, " +
		"and code quality. Be concise."
	if steer := o.drainSteer(steerCh); steer != "" {
		// Wrap user steer so the reviewer treats it as guidance data
		// rather than executable instructions. Iteration 2 fix.
		reviewPrompt = WrapAsUserContent(steer, "USER_STEER") +
			"\n\n" + reviewPrompt
		o.store.AppendEvent(task.ID, "steer_applied", steer)
	}

	reviewOutput, err := o.cfg.SpawnFunc(ctx, AgentConfig{
		Binary: "altcode",
		Args: []string{
			"--model", reviewModel,
			reviewPrompt,
		},
		Dir:  o.cfg.WorkDir,
		Env:  o.taskEnv(task),
		Role: "reviewer",
	})
	if err != nil {
		// Review failure must not be swallowed — a task whose reviewer
		// crashed/timed-out should not be reported as successfully
		// merged. Persist the failure and abort finalize.
		if ferr := o.store.MarkFailed(task.ID,
			fmt.Sprintf("review phase: %v", err)); ferr != nil {
			o.logger.Warn("mark review-failed", "task", task.ID, "err", ferr)
		}
		return fmt.Errorf("review phase: %w", err)
	}
	o.emitPhase(task.ID, "review", "completed")
	// Iter 6/7: review output contributes to the cost proxy; a single
	// emitCost call both records and returns any budget overflow.
	if err := o.emitCost(task.ID, "review", reviewOutput, budget); err != nil {
		o.failBudget(task.ID, "review", err)
		return err
	}

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
	if err := o.store.AppendEvent(taskID, "phase_"+action, string(data)); err != nil {
		o.logger.Warn("emit phase event", "task", taskID, "phase", phase, "err", err)
	}
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
	if err := o.store.AppendEvent(taskID, "spec", string(data)); err != nil {
		o.logger.Warn("emit spec event", "task", taskID, "err", err)
	}
}

// emitCost records a cost attribution event after a phase completes
// AND feeds the cost sample into the running task budget. For now the
// cost is best-estimated from output_bytes (agents don't report USD
// directly); the helper is priced with a conservative rate so the
// running total is a proxy rather than a ground-truth cost.
//
// The budget.recordCost return is surfaced to the caller so the
// orchestrator can abort at the same boundary the overflow was
// detected — a single recorded-and-returned contract rather than
// record + separate probe. CC iter-6 review called out the prior
// dual-path as a contract smell; CC iter-7 caught the second probe
// then became dead code, so it's been removed entirely.
func (o *Orchestrator) emitCost(taskID, phase, output string, budget *taskBudget) error {
	// Rough proxy: output_bytes × $1e-6 is deliberately an order of
	// magnitude smaller than any real LLM rate, so zero-cost agents
	// trip only true overages. When an agent reports cost directly
	// (future hook) it flows through this same path.
	estUSD := float64(len(output)) * 1e-6
	data, _ := json.Marshal(map[string]any{
		"phase":        phase,
		"output_bytes": len(output),
		"est_usd":      estUSD,
	})
	if err := o.store.AppendEvent(taskID, "phase_cost", string(data)); err != nil {
		o.logger.Warn("emit cost", "task", taskID, "err", err)
	}
	if budget != nil {
		if err := budget.recordCost(estUSD); err != nil {
			return err
		}
	}
	return nil
}


// taskEnv returns environment variables to inject into every agent
// spawn for the given task. Always includes ALTFIX_REPO_URL.
func (o *Orchestrator) taskEnv(task *Task) []string {
	if task.RepoURL == "" {
		return nil
	}
	return []string{"ALTFIX_REPO_URL=" + task.RepoURL}
}

// drainSteer reads all pending steer messages from the channel
// and joins them into a single "User guidance: ..." string.
// Returns empty string if no messages are pending.
func (o *Orchestrator) drainSteer(ch <-chan string) string {
	if ch == nil {
		return ""
	}
	var msgs []string
	for {
		select {
		case msg := <-ch:
			msgs = append(msgs, msg)
		default:
			if len(msgs) == 0 {
				return ""
			}
			return "User guidance: " + strings.Join(msgs, "; ")
		}
	}
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
