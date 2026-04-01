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
