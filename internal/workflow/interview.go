package workflow

import "fmt"

// InterviewPrompt returns the system prompt for deep-interview mode.
// The model runs a Socratic clarification loop with ambiguity scoring
// before handing off to planning or execution.
func InterviewPrompt(task string) string {
	return fmt.Sprintf(`You are running a deep-interview to clarify a task before execution.

## Goal
Turn the following request into an execution-ready spec by asking Socratic questions.

## Task
%s

## Rules
1. Ask ONE question per round. Never batch multiple questions.
2. Target the weakest clarity dimension: intent → outcome → scope → constraints → success criteria.
3. After each answer, re-score ambiguity (0.0 = perfectly clear, 1.0 = totally vague).
4. Stop when ambiguity drops below 0.20 OR after 10 rounds.
5. Use challenge modes when stuck:
   - Contrarian: "What if the opposite were true?"
   - Simplifier: "What's the absolute minimum version?"
   - Ontologist: "What is this really about?"

## Ambiguity Scoring
Rate each dimension 0-1:
- Intent: What are we trying to accomplish?
- Outcome: What does success look like?
- Scope: What's in and out of bounds?
- Constraints: What limits exist (time, tech, resources)?
- Success: How do we verify it worked?

Ambiguity = 1 - (intent×0.30 + outcome×0.25 + scope×0.20 + constraints×0.15 + success×0.10)

## Output
After reaching clarity, produce:
1. A one-paragraph execution-ready spec
2. Acceptance criteria (testable)
3. Non-goals (explicit)
4. Recommended next step: /plan, /execute, or /ralph`, task)
}

// InterviewSpec formats the interview result as a spec file.
func InterviewSpec(task, spec, criteria, nonGoals string) string {
	return fmt.Sprintf(`# Interview Spec

## Task
%s

## Execution-Ready Spec
%s

## Acceptance Criteria
%s

## Non-Goals
%s
`, task, spec, criteria, nonGoals)
}
