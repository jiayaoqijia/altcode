// Package orchestra executes phased workflows using external CLI agents.
// It reads workflow definitions, spawns agents per phase, streams typed
// events to the TUI, and supports manual override at phase boundaries.
package orchestra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/agent"
	"github.com/jiayaoqijia/altcode/internal/wfdef"
)

// RunParams configures a workflow execution.
type RunParams struct {
	Def      *wfdef.WorkflowDef
	Task     string
	WorkDir  string
	Events   chan<- PhaseEvent
	Override <-chan OverrideCmd
}

// Run executes a workflow definition phase by phase.
func Run(ctx context.Context, p RunParams) error {
	order, err := p.Def.TopoSort()
	if err != nil {
		return fmt.Errorf("topo sort: %w", err)
	}

	results := map[string]*PhaseResult{}

	for _, phaseName := range order {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Check for override before starting phase
		if cmd, ok := checkOverride(p.Override); ok {
			switch cmd.Op {
			case OpAbort:
				return fmt.Errorf("workflow aborted by user")
			case OpSkip:
				trySendEventCtx(ctx, p.Events, PhaseEvent{Phase: phaseName, Type: KindPhaseDone, Text: "skipped"})
				results[phaseName] = &PhaseResult{PhaseID: phaseName, Verdict: VerdictSkipped}
				continue
			case OpPause:
				// Block until resume or abort
				trySendEvent(p.Events, PhaseEvent{Phase: phaseName, Type: KindText, Text: "[paused — waiting for resume]"})
				if resumeCmd, ok := waitOverride(ctx, p.Override); ok {
					if resumeCmd.Op == OpAbort {
						return fmt.Errorf("workflow aborted by user")
					}
					// OpResume or anything else: continue
				}
			}
		}

		phase := p.Def.PhaseByName(phaseName)
		if phase == nil {
			continue
		}

		// Check dependencies passed
		skip := false
		for _, dep := range phase.DependsOn {
			if r, ok := results[dep]; ok && r.Verdict == VerdictFail {
				trySendEventCtx(ctx, p.Events, PhaseEvent{Phase: phaseName, Type: KindPhaseDone, Text: "skipped (dependency failed)"})
				results[phaseName] = &PhaseResult{PhaseID: phaseName, Verdict: VerdictSkipped}
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		// Build prior outputs keyed by phase name (concatenate all role outputs)
		priorOutputs := map[string]string{}
		for name, r := range results {
			if r.Outputs == nil {
				continue
			}
			var combined strings.Builder
			for _, out := range r.Outputs {
				combined.WriteString(out)
				combined.WriteString("\n")
			}
			priorOutputs[name] = combined.String()
		}

		result := runPhase(ctx, p, phase, priorOutputs)
		results[phaseName] = result

		trySendEventCtx(ctx, p.Events, PhaseEvent{
			Phase: phaseName,
			Type:  KindPhaseDone,
			Text:  result.Verdict.String(),
		})

		if result.Verdict == VerdictFail {
			switch phase.OnFailure {
			case wfdef.FailureAbort:
				return fmt.Errorf("phase %q failed (abort policy)", phaseName)
			case wfdef.FailureHuman:
				// Block until user decides. Loop on commands the gate
				// doesn't understand (Pause, Inject) so they aren't
				// silently consumed and reinterpreted as continue.
				trySendEvent(p.Events, PhaseEvent{
					Phase: phaseName, Type: KindError,
					Text: fmt.Sprintf("Phase %q failed. Send Skip/Resume to continue or Abort to stop.", phaseName),
				})
				for {
					cmd, ok := waitOverride(ctx, p.Override)
					if !ok {
						break
					}
					switch cmd.Op {
					case OpAbort:
						return fmt.Errorf("workflow aborted by user")
					case OpSkip, OpResume:
						// continue to next phase
					default:
						// Unknown / Pause / Inject — wait for the
						// operator to send a real continue/abort
						// instead of consuming and reinterpreting.
						trySendEvent(p.Events, PhaseEvent{
							Phase: phaseName, Type: KindError,
							Text: fmt.Sprintf("Failure-gate ignored %q; send Skip, Resume, or Abort.", cmd.Op),
						})
						continue
					}
					break
				}
			case wfdef.FailureSkip:
				// continue
			}
		}
	}
	return nil
}

func runPhase(ctx context.Context, p RunParams, phase *wfdef.PhaseDef, priorOutputs map[string]string) *PhaseResult {
	timeout := phase.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	phaseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	outputs := map[string]string{}
	var lastSessionID string
	allPassed := true

	// Run agents — parallel or sequential based on phase config.
	// Pass the resolved timeout (with default applied) so the parallel
	// path doesn't read phase.Timeout again and lose the default,
	// which would let phases without an explicit timeout spawn agents
	// with Timeout: 0 (effectively unbounded).
	if phase.Parallel && len(phase.Agents) > 1 {
		return runPhaseParallel(phaseCtx, p, phase, priorOutputs, timeout)
	}

	for _, ag := range phase.Agents {
		// Surface InjectContext failures via a phase event so the
		// user knows the agent is launching with stale or missing
		// CLAUDE.md/AGENTS.md context. Previously the error was
		// silently dropped and agents produced confidently wrong
		// work with no indication anything was off.
		if err := InjectContext(p.WorkDir, ag.Backend, ag.Role, p.Task, priorOutputs); err != nil {
			trySendEventCtx(ctx, p.Events, PhaseEvent{
				Phase: phase.Name, Role: ag.Role,
				Type: KindError,
				Text: fmt.Sprintf("context injection failed: %v", err),
			})
		}

		prompt := expandPrompt(ag.Prompt, p.Task, priorOutputs)

		cfg := agent.ExternalAgentConfig{
			Backend: agent.CLIBackend(ag.Backend),
			Role:    ag.Role,
			Model:   ag.Model,
			Timeout: timeout,
			WorkDir: p.WorkDir,
		}

		stream := agent.SpawnExternal(phaseCtx, cfg, prompt)

		// Drain events
		for ev := range stream.Events {
			trySendEvent(p.Events, PhaseEvent{
				Phase: phase.Name, Role: ag.Role,
				Type: mapEventType(ev.Type), Text: ev.Content, Tool: ev.Tool,
			})
		}

		result := <-stream.Result
		outputs[ag.Role] = result.Output
		if result.SessionID != "" {
			lastSessionID = result.SessionID
		}
		if result.Error != nil {
			allPassed = false
		}
	}

	verdict := VerdictPass
	if !allPassed {
		verdict = VerdictFail
	}
	return &PhaseResult{PhaseID: phase.Name, Verdict: verdict, Outputs: outputs, SessionID: lastSessionID}
}

func runPhaseParallel(ctx context.Context, p RunParams, phase *wfdef.PhaseDef, priorOutputs map[string]string, timeout time.Duration) *PhaseResult {
	type agentResult struct {
		role      string
		output    string
		sessionID string
		err       error
	}

	ch := make(chan agentResult, len(phase.Agents))

	// Inject context sequentially BEFORE launching goroutines to avoid file races
	prompts := make(map[string]string, len(phase.Agents))
	for _, ag := range phase.Agents {
		// Same surfacing as the sequential path: don't silently
		// launch agents with stale context.
		if err := InjectContext(p.WorkDir, ag.Backend, ag.Role, p.Task, priorOutputs); err != nil {
			trySendEventCtx(ctx, p.Events, PhaseEvent{
				Phase: phase.Name, Role: ag.Role,
				Type: KindError,
				Text: fmt.Sprintf("context injection failed: %v", err),
			})
		}
		prompts[ag.Role] = expandPrompt(ag.Prompt, p.Task, priorOutputs)
	}

	for _, ag := range phase.Agents {
		go func(ag wfdef.AgentAssignment) {
			prompt := prompts[ag.Role]

			cfg := agent.ExternalAgentConfig{
				Backend: agent.CLIBackend(ag.Backend),
				Role:    ag.Role,
				Model:   ag.Model,
				Timeout: timeout,
				WorkDir: p.WorkDir,
			}

			stream := agent.SpawnExternal(ctx, cfg, prompt)
			for ev := range stream.Events {
				trySendEvent(p.Events, PhaseEvent{
					Phase: phase.Name, Role: ag.Role,
					Type: mapEventType(ev.Type), Text: ev.Content, Tool: ev.Tool,
				})
			}

			result := <-stream.Result
			ch <- agentResult{role: ag.Role, output: result.Output, sessionID: result.SessionID, err: result.Error}
		}(ag)
	}

	outputs := map[string]string{}
	var lastSessionID string
	allPassed := true

	for range phase.Agents {
		r := <-ch
		outputs[r.role] = r.output
		if r.sessionID != "" {
			lastSessionID = r.sessionID
		}
		if r.err != nil {
			allPassed = false
		}
	}

	verdict := VerdictPass
	if !allPassed {
		verdict = VerdictFail
	}
	return &PhaseResult{PhaseID: phase.Name, Verdict: verdict, Outputs: outputs, SessionID: lastSessionID}
}

func expandPrompt(prompt, task string, priorOutputs map[string]string) string {
	if prompt == "" {
		return task
	}
	result := strings.ReplaceAll(prompt, "{{.Task}}", task)
	// Simple template: {{.PhaseOutput "design"}} → concatenated outputs from that phase
	for key, val := range priorOutputs {
		result = strings.ReplaceAll(result, "{{.PhaseOutput \""+key+"\"}}", val)
	}
	return result
}

func mapEventType(t agent.AgentEventType) PhaseEventKind {
	switch t {
	case agent.EventText:
		return KindText
	case agent.EventThinking:
		return KindThinking
	case agent.EventToolUse:
		return KindToolStart
	case agent.EventToolResult:
		return KindToolDone
	case agent.EventError:
		return KindError
	default:
		return KindText
	}
}

// trySendEvent forwards a phase event to the consumer channel.
//
// PhaseDone events are NEVER dropped — phase transition state in the
// TUI depends on seeing them. We block until the channel can accept
// the event, falling out only when ctx is cancelled. Lossy non-blocking
// sends silently desynced the workflow header (a phase would never
// "complete" because the TUI missed the KindPhaseDone).
//
// Other event types still use a non-blocking send so a slow consumer
// can drop chatty per-tool events without blocking the workflow.
//
// Pass a real ctx through tsCtx so a closed/dead consumer can't hang
// the orchestra goroutine forever — the previous unconditional send
// on PhaseDone would block forever if the TUI was already gone.
func trySendEvent(ch chan<- PhaseEvent, ev PhaseEvent) {
	trySendEventCtx(context.Background(), ch, ev)
}

// trySendEventCtx is the ctx-aware variant. Most call sites have a
// real workflow context — they should use this directly instead of
// the legacy trySendEvent shim.
func trySendEventCtx(ctx context.Context, ch chan<- PhaseEvent, ev PhaseEvent) {
	if ev.Type == KindPhaseDone {
		select {
		case ch <- ev:
		case <-ctx.Done():
		}
		return
	}
	select {
	case ch <- ev:
	default:
	}
}

func checkOverride(ch <-chan OverrideCmd) (OverrideCmd, bool) {
	select {
	case cmd := <-ch:
		return cmd, true
	default:
		return OverrideCmd{}, false
	}
}

func waitOverride(ctx context.Context, ch <-chan OverrideCmd) (OverrideCmd, bool) {
	select {
	case cmd := <-ch:
		return cmd, true
	case <-ctx.Done():
		return OverrideCmd{}, false
	}
}
