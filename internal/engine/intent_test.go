package engine

import "testing"

func TestClassifyIntent_QA(t *testing.T) {
	tests := []struct {
		input string
		class TaskClass
	}{
		{"explain how the auth system works", TaskQA},
		{"what is this function doing?", TaskQA},
		{"what does the loop in engine.go do?", TaskQA},
		{"how does compaction work?", TaskQA},
	}
	for _, tt := range tests {
		intent := ClassifyIntent(tt.input)
		if intent.Class != tt.class {
			t.Errorf("ClassifyIntent(%q) = %d, want %d", tt.input, intent.Class, tt.class)
		}
		if intent.NeedsTests {
			t.Errorf("QA tasks should not need tests: %q", tt.input)
		}
		if intent.Risk != RiskLow {
			t.Errorf("QA tasks should be low risk: %q", tt.input)
		}
	}
}

func TestClassifyIntent_BugFix(t *testing.T) {
	tests := []string{
		"fix the nil pointer in handler",
		"fix this crash on startup",
		"fix bug where tokens overflow",
	}
	for _, input := range tests {
		intent := ClassifyIntent(input)
		if intent.Class != TaskLocalFix {
			t.Errorf("ClassifyIntent(%q).Class = %d, want TaskLocalFix", input, intent.Class)
		}
		if !intent.NeedsTests {
			t.Errorf("Bug fixes should need tests: %q", input)
		}
	}
}

func TestClassifyIntent_HighRisk(t *testing.T) {
	tests := []string{
		"update the auth middleware",
		"change the database migration",
		"modify production deploy script",
	}
	for _, input := range tests {
		intent := ClassifyIntent(input)
		if intent.Risk != RiskHigh {
			t.Errorf("ClassifyIntent(%q).Risk = %d, want RiskHigh", input, intent.Risk)
		}
		if !intent.NeedsReview {
			t.Errorf("High-risk tasks should need review: %q", input)
		}
		if !intent.NeedsPlan {
			t.Errorf("High-risk tasks should need plan: %q", input)
		}
	}
}

func TestClassifyIntent_Refactor(t *testing.T) {
	intent := ClassifyIntent("refactor the tool dispatch system")
	if intent.Class != TaskRefactor {
		t.Errorf("got class %d, want TaskRefactor", intent.Class)
	}
	if !intent.NeedsTests || !intent.NeedsReview {
		t.Error("refactors should need tests and review")
	}
}

func TestClassifyIntent_Architecture(t *testing.T) {
	intent := ClassifyIntent("redesign the entire agent system")
	if intent.Class != TaskArchitecture {
		t.Errorf("got class %d, want TaskArchitecture", intent.Class)
	}
	if intent.Risk != RiskHigh {
		t.Errorf("architecture changes should be high risk")
	}
}

func TestClassifyIntent_Review(t *testing.T) {
	intent := ClassifyIntent("review the recent changes for bugs")
	if intent.Class != TaskReadOnlyReview {
		t.Errorf("got class %d, want TaskReadOnlyReview", intent.Class)
	}
	if intent.NeedsTests {
		t.Error("reviews should not need tests")
	}
}

func TestClassifyIntent_DefaultIsFeature(t *testing.T) {
	intent := ClassifyIntent("add a new button to the dashboard")
	if intent.Class != TaskFeature {
		t.Errorf("got class %d, want TaskFeature", intent.Class)
	}
	if !intent.NeedsTests {
		t.Error("features should need tests")
	}
}

func TestMatchesAny(t *testing.T) {
	if !matchesAny("hello world", "hello", "foo") {
		t.Error("should match 'hello'")
	}
	if matchesAny("hello world", "foo", "bar") {
		t.Error("should not match")
	}
}
