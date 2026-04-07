package engine

import (
	"strings"
)

// TaskClass identifies the type of task from the user's prompt.
type TaskClass int

const (
	TaskQA              TaskClass = iota // explanation, question
	TaskReadOnlyReview                   // review, audit, inspect
	TaskLocalFix                         // small bug fix, typo
	TaskRefactor                         // rename, restructure
	TaskFeature                          // new functionality
	TaskDebug                            // investigate, trace, diagnose
	TaskArchitecture                     // large multi-file change
	TaskResume                           // --last, continue
)

// RiskLevel indicates how much caution the task needs.
type RiskLevel int

const (
	RiskLow    RiskLevel = iota // read-only or single-file edit
	RiskMedium                  // multi-file, tests needed
	RiskHigh                    // auth/security/migration/destructive
)

// TaskIntent holds the classified task properties.
type TaskIntent struct {
	Class        TaskClass
	Risk         RiskLevel
	NeedsReview  bool   // should auto-trigger reviewer pass
	NeedsPlan    bool   // should create a plan before executing
	NeedsTests   bool   // should write tests
	Description  string // short classification label
}

// ClassifyIntent routes a user prompt to the appropriate task class and risk level.
// Implements Section 8 of the world-class design doc.
func ClassifyIntent(prompt string) TaskIntent {
	lower := strings.ToLower(prompt)

	intent := TaskIntent{
		Class:       TaskFeature,
		Risk:        RiskMedium,
		NeedsTests:  true,
		Description: "feature implementation",
	}

	// Q&A / explanation
	if matchesAny(lower, "explain", "what is", "what does", "how does", "why does", "what's the") {
		intent.Class = TaskQA
		intent.Risk = RiskLow
		intent.NeedsTests = false
		intent.Description = "explanation"
		return intent
	}

	// Read-only review
	if matchesAny(lower, "review", "audit", "inspect", "check for", "find bugs", "security review") {
		intent.Class = TaskReadOnlyReview
		intent.Risk = RiskLow
		intent.NeedsTests = false
		intent.NeedsReview = false
		intent.Description = "review"
		return intent
	}

	// Local bug fix
	if matchesAny(lower, "fix the", "fix this", "fix bug", "fix error", "fix crash", "fix nil", "fix panic") {
		intent.Class = TaskLocalFix
		intent.Risk = RiskLow
		intent.NeedsTests = true
		intent.Description = "bug fix"
		return intent
	}

	// Debug / investigate
	if matchesAny(lower, "debug", "investigate", "why is", "trace", "diagnose", "root cause") {
		intent.Class = TaskDebug
		intent.Risk = RiskLow
		intent.NeedsTests = false
		intent.Description = "investigation"
		return intent
	}

	// Refactor
	if matchesAny(lower, "refactor", "rename", "extract", "move", "reorganize", "clean up") {
		intent.Class = TaskRefactor
		intent.Risk = RiskMedium
		intent.NeedsTests = true
		intent.NeedsReview = true
		intent.Description = "refactoring"
		return intent
	}

	// High risk indicators
	if matchesAny(lower, "auth", "security", "permission", "migration", "database", "deploy",
		"production", "api key", "credential", "delete all", "drop table") {
		intent.Risk = RiskHigh
		intent.NeedsReview = true
		intent.NeedsPlan = true
		intent.Description = "high-risk change"
	}

	// Large / architecture
	if matchesAny(lower, "redesign", "architect", "implement the full", "build a complete",
		"add a new system", "rewrite") {
		intent.Class = TaskArchitecture
		intent.Risk = RiskHigh
		intent.NeedsPlan = true
		intent.NeedsReview = true
		intent.Description = "architectural change"
	}

	// Multi-file indicators
	if matchesAny(lower, "across", "all files", "every", "multiple", "and also") {
		intent.Risk = RiskMedium
		intent.NeedsReview = true
	}

	return intent
}

func matchesAny(text string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}
