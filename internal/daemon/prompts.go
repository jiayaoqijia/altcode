package daemon

import "fmt"

// BuildPlanPrompt creates the system+user prompt for the lead/planner agent.
func BuildPlanPrompt(task *Task, repoProfile string) string {
	return fmt.Sprintf(`You are the lead planner for a coding task.

Repository: %s
Task: %s

%s

Analyze the task and produce a JSON plan:
{"steps": [{"description": "...", "prompt": "...", "success_criteria": "..."}], "complexity": "simple|medium|complex", "estimated_turns": N}

Rules:
- Each step must have machine-checkable success criteria
- Steps should be independent where possible
- Estimate complexity honestly`, task.RepoURL, task.TaskDescription, repoProfile)
}

// BuildImplementPrompt creates the prompt for an implementer agent.
func BuildImplementPrompt(step PlanStep, relevantFiles string) string {
	return fmt.Sprintf(`You are an implementer agent. Complete this step:

Step: %s
Prompt: %s

Relevant files:
%s

Rules:
- Make minimal, focused changes
- Do not modify files outside the scope of this step
- Run tests after making changes
- If a test fails, fix it before moving on`, step.Description, step.Prompt, relevantFiles)
}

// BuildReviewPrompt creates the prompt for a reviewer agent.
func BuildReviewPrompt(diff, plan, criteria string) string {
	return fmt.Sprintf(`You are a code reviewer. Review the following diff against the plan.

Plan:
%s

Success criteria:
%s

Diff:
%s

Output your review as JSON:
{"verdict": "pass" or "fail", "issues": [{"file": "...", "line": 0, "severity": "error|warning|info", "message": "..."}]}

Rules:
- Verdict must be "pass" or "fail"
- Only fail for real correctness or security issues
- Warnings do not block a pass verdict`, plan, criteria, diff)
}

// BuildTestPrompt creates the prompt for a tester agent.
func BuildTestPrompt(changedFiles, testFramework string) string {
	return fmt.Sprintf(`You are a test agent. Write and run tests for these changed files.

Changed files:
%s

Test framework: %s

Rules:
- Write tests that cover the happy path and at least one error case
- Run the tests and report results
- If tests fail, fix them until they pass`, changedFiles, testFramework)
}

// BuildAutofixPrompt creates the prompt for fixing CI failures.
func BuildAutofixPrompt(ciLogs, changedFiles string) string {
	return fmt.Sprintf(`You are an autofix agent. CI has failed. Fix the issues.

CI logs:
%s

Changed files:
%s

Rules:
- Read the CI logs carefully to identify the root cause
- Only modify files that are related to the failure
- Run the failing command locally to verify your fix
- Do not introduce new warnings or errors`, ciLogs, changedFiles)
}

// BuildSteerPrompt creates the prompt when a user steers mid-task.
func BuildSteerPrompt(userMessage, currentPlan, progress string) string {
	return fmt.Sprintf(`The user has sent a steering message mid-task. Adjust the plan.

User message: %s

Current plan:
%s

Progress so far:
%s

Produce an updated JSON plan incorporating the user's feedback:
{"steps": [{"description": "...", "prompt": "...", "success_criteria": "..."}], "complexity": "simple|medium|complex", "estimated_turns": N}

Rules:
- Preserve completed steps
- Adjust remaining steps to incorporate the user feedback
- If the user contradicts the original task, prioritize the user message`, userMessage, currentPlan, progress)
}
