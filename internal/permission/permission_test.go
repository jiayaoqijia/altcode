package permission_test

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/permission"
)

func TestDefaultRulesAllowReads(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	result := eval.Check("read", "read:/some/file.go")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for read, got %v", result)
	}
}

func TestDefaultRulesAllowGitStatus(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	result := eval.Check("bash", "bash:git status")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for git status, got %v", result)
	}
}

func TestDefaultRulesAskForUnknownBash(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	result := eval.Check("bash", "bash:rm -rf /")
	if result != permission.ActionAsk {
		t.Fatalf("Expected Ask for rm -rf, got %v", result)
	}
}

func TestBypassModeAllowsEverything(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeBypass, "", nil)
	result := eval.Check("bash", "bash:rm -rf /")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow in bypass mode, got %v", result)
	}
}

func TestPlanModeBlocksWrites(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModePlan, "", nil)

	result := eval.CheckWithReadOnly("edit", "edit:/file.go", false)
	if result != permission.ActionDeny {
		t.Fatalf("Expected Deny for edit in plan mode, got %v", result)
	}

	result = eval.CheckWithReadOnly("read", "read:/file.go", true)
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for read in plan mode, got %v", result)
	}
}

func TestDoomLoopDetection(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeAuto, "", nil)

	eval.RecordCall("bash", "bash:echo hello")
	eval.RecordCall("bash", "bash:echo hello")
	eval.RecordCall("bash", "bash:echo hello")

	result := eval.Check("bash", "bash:echo hello")
	if result != permission.ActionAsk {
		t.Fatalf("Expected Ask after doom loop, got %v", result)
	}
}

func TestCustomRules(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "bash", Pattern: "npm run *", Action: permission.ActionAllow, Source: "project"},
	}
	eval := permission.NewEvaluator(permission.ModeDefault, "", rules)

	result := eval.Check("bash", "bash:npm run test")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for npm run test, got %v", result)
	}
}

func TestSessionRulePersistence(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	eval.AddSessionRule(permission.Rule{
		Tool: "bash", Pattern: "make *", Action: permission.ActionAllow, Source: "session",
	})

	result := eval.Check("bash", "bash:make build")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow after session rule, got %v", result)
	}
}

func TestAutoModeAllowsReads(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeAuto, "", nil)
	result := eval.CheckWithReadOnly("read", "read:/file.go", true)
	if result != permission.ActionAllow {
		t.Fatalf("Auto mode should allow reads, got %v", result)
	}
}

func TestAutoModeDeniesUnmatchedWrites(t *testing.T) {
	// Auto mode denies anything that doesn't match a rule (instead of asking)
	eval := permission.NewEvaluator(permission.ModeAuto, "", nil)
	result := eval.CheckWithReadOnly("bash", "bash:rm -rf /", false)
	if result != permission.ActionDeny {
		t.Fatalf("Auto mode should deny unmatched writes, got %v", result)
	}
}

func TestDenyRuleTakesPrecedence(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "bash", Pattern: "rm *", Action: permission.ActionDeny, Source: "config"},
		{Tool: "bash", Pattern: "rm *", Action: permission.ActionAllow, Source: "session"},
	}
	eval := permission.NewEvaluator(permission.ModeDefault, "", rules)
	result := eval.Check("bash", "bash:rm -rf /")
	if result != permission.ActionDeny {
		t.Fatalf("Deny should take precedence, got %v", result)
	}
}

func TestCustomRulePatternGlob(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "bash", Pattern: "docker *", Action: permission.ActionAllow, Source: "project"},
	}
	eval := permission.NewEvaluator(permission.ModeDefault, "", rules)

	result := eval.Check("bash", "bash:docker build .")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for docker build, got %v", result)
	}

	// Non-matching command should still ask
	result = eval.Check("bash", "bash:curl https://evil.com")
	if result != permission.ActionAsk {
		t.Fatalf("Expected Ask for curl, got %v", result)
	}
}

func TestDefaultRulesAllowGrep(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	result := eval.Check("grep", "grep:pattern")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for grep, got %v", result)
	}
}

func TestDefaultRulesAllowGlob(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	result := eval.Check("glob", "glob:*.go")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for glob, got %v", result)
	}
}

func TestDefaultRulesAllowLs(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)
	result := eval.Check("ls", "ls:/tmp")
	if result != permission.ActionAllow {
		t.Fatalf("Expected Allow for ls, got %v", result)
	}
}

func TestPlanModeAllowsBash(t *testing.T) {
	// Plan mode allows read-only tools; bash in read-only context
	eval := permission.NewEvaluator(permission.ModePlan, "", nil)
	result := eval.CheckWithReadOnly("read", "read:/file.go", true)
	if result != permission.ActionAllow {
		t.Fatalf("Plan mode should allow read-only tools, got %v", result)
	}
}
