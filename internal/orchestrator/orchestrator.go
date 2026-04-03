// Package orchestrator coordinates multiple AI models for design, thinking,
// and evaluation. Each model plays a role (architect, implementer, reviewer)
// and they cross-check each other's work.
package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

// Role defines what perspective a model brings.
type Role string

const (
	RoleArchitect  Role = "architect"
	RoleImplementer Role = "implementer"
	RoleReviewer   Role = "reviewer"
	RoleChallenger Role = "challenger"
	RoleEvaluator  Role = "evaluator"
)

// ModelAssignment maps a role to a specific model.
type ModelAssignment struct {
	Role   Role
	Model  string // e.g. "openai/gpt-5.4", "anthropic/claude-haiku-4-5-20251001"
	Config *config.Config
}

// Finding is a single observation from a model.
type Finding struct {
	Model      string
	Role       Role
	Type       string // "suggestion", "concern", "approval", "rejection"
	Content    string
	Confidence float64 // 0-1
}

// Verdict is the synthesized result from all models.
type Verdict struct {
	Decision    string    // "approve", "iterate", "reject"
	Findings    []Finding
	Agreement   float64   // 0-1, how much models agree
	Summary     string
	Timestamp   time.Time
}

// Session orchestrates a multi-model conversation.
type Session struct {
	assignments []ModelAssignment
	findings    []Finding
	mu          sync.Mutex
}

// NewSession creates an orchestration session with model assignments.
func NewSession(assignments []ModelAssignment) *Session {
	return &Session{assignments: assignments}
}

// RunParallel sends a prompt to all assigned models in parallel,
// collects their responses, and returns findings.
func (s *Session) RunParallel(ctx context.Context, prompt string) ([]Finding, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var findings []Finding

	for _, a := range s.assignments {
		wg.Add(1)
		go func(assignment ModelAssignment) {
			defer wg.Done()
			rolePrompt := roleSystemPrompt(assignment.Role)
			fullPrompt := rolePrompt + "\n\n" + prompt

			eng, err := engine.New(engine.EngineParams{Config: assignment.Config})
			if err != nil {
				mu.Lock()
				findings = append(findings, Finding{
					Model: assignment.Model, Role: assignment.Role,
					Type: "error", Content: err.Error(),
				})
				mu.Unlock()
				return
			}

			var text string
			for ev := range eng.Run(ctx, fullPrompt) {
				if ev.Type == event.TextDelta {
					text += ev.Text
				}
			}

			mu.Lock()
			findings = append(findings, Finding{
				Model: assignment.Model, Role: assignment.Role,
				Type: classifyResponse(text), Content: text,
				Confidence: 0.8,
			})
			mu.Unlock()
		}(a)
	}

	wg.Wait()

	s.mu.Lock()
	s.findings = append(s.findings, findings...)
	s.mu.Unlock()

	return findings, nil
}

// CrossCheck sends each model's findings to the others for validation.
func (s *Session) CrossCheck(ctx context.Context) ([]Finding, error) {
	s.mu.Lock()
	existing := make([]Finding, len(s.findings))
	copy(existing, s.findings)
	s.mu.Unlock()

	summary := formatFindings(existing)
	prompt := "Other AI models produced these findings:\n\n" + summary +
		"\n\nDo you agree or disagree with each finding? " +
		"Point out anything they missed or got wrong. Be specific."

	return s.RunParallel(ctx, prompt)
}

// Synthesize produces a verdict from all findings.
func (s *Session) Synthesize() *Verdict {
	s.mu.Lock()
	defer s.mu.Unlock()

	approvals, concerns := 0, 0
	for _, f := range s.findings {
		switch f.Type {
		case "approval":
			approvals++
		case "concern", "rejection":
			concerns++
		}
	}

	total := approvals + concerns
	agreement := 0.5
	if total > 0 {
		agreement = float64(approvals) / float64(total)
	}

	decision := "iterate"
	if agreement >= 0.8 {
		decision = "approve"
	} else if agreement < 0.3 {
		decision = "reject"
	}

	return &Verdict{
		Decision:  decision,
		Findings:  s.findings,
		Agreement: agreement,
		Summary:   formatVerdict(s.findings, decision, agreement),
		Timestamp: time.Now(),
	}
}

// Findings returns all collected findings.
func (s *Session) Findings() []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Finding, len(s.findings))
	copy(out, s.findings)
	return out
}

func roleSystemPrompt(role Role) string {
	switch role {
	case RoleArchitect:
		return "You are a senior software architect. Focus on design, patterns, scalability, and maintainability. Be decisive — pick one approach."
	case RoleImplementer:
		return "You are an expert implementer. Focus on concrete code, correctness, edge cases, and efficiency. Show working code."
	case RoleReviewer:
		return "You are a code reviewer. Find bugs, security issues, style problems, and missing tests. Be thorough but fair."
	case RoleChallenger:
		return "You are an adversarial reviewer. Try to break the code. Find race conditions, injection vectors, resource leaks, and failure modes. No compliments."
	case RoleEvaluator:
		return "You are an evaluator. Score the work on correctness, completeness, quality, and security (1-10 each). Give a verdict: PASS, ITERATE, or FAIL."
	default:
		return "You are a helpful coding assistant."
	}
}

func classifyResponse(text string) string {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "lgtm") || strings.Contains(lower, "approve") || strings.Contains(lower, "pass") {
		return "approval"
	}
	if strings.Contains(lower, "reject") || strings.Contains(lower, "fail") || strings.Contains(lower, "block") {
		return "rejection"
	}
	if strings.Contains(lower, "concern") || strings.Contains(lower, "issue") || strings.Contains(lower, "bug") || strings.Contains(lower, "problem") {
		return "concern"
	}
	return "suggestion"
}

func formatFindings(findings []Finding) string {
	var sb strings.Builder
	for _, f := range findings {
		sb.WriteString(fmt.Sprintf("[%s / %s] (%s):\n%s\n\n",
			f.Model, f.Role, f.Type, f.Content))
	}
	return sb.String()
}

func formatVerdict(findings []Finding, decision string, agreement float64) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Decision: %s (%.0f%% agreement)\n\n", strings.ToUpper(decision), agreement*100))

	byModel := make(map[string][]Finding)
	for _, f := range findings {
		byModel[f.Model] = append(byModel[f.Model], f)
	}

	for model, fs := range byModel {
		sb.WriteString(fmt.Sprintf("— %s:\n", model))
		for _, f := range fs {
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", f.Type, truncateStr(f.Content, 100)))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
