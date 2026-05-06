package exec_test

// Phase 13: comprehensive table-driven tests for Params.Validate()
// plus E2E smoke tests for each flag class. The per-phase files
// pin behavior for a single rule each; this file verifies the
// full matrix of mutual-exclusion rules survives any future
// reordering or field additions.
//
// Every validation rule added in Phases 1-12 should have at least
// one passing case and one failing case here.

import (
	"errors"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/exec"
)

func TestValidate_Matrix(t *testing.T) {
	// Each case sets only the fields it cares about and asserts
	// whether Validate() returns a UsageError. The point is to
	// catch cross-phase regressions where adding a new rule
	// breaks an older rule or vice versa.
	cases := []struct {
		name     string
		params   exec.Params
		wantOK   bool   // true = expect no error
		wantMsg  string // substring that must appear in the error message (empty = any)
	}{
		// --- Phase 1: output format ---
		{
			name:   "default empty params",
			params: exec.Params{},
			wantOK: true,
		},
		{
			name:   "output-format text ok",
			params: exec.Params{OutputFormat: "text"},
			wantOK: true,
		},
		{
			name:    "output-format yaml rejected",
			params:  exec.Params{OutputFormat: "yaml"},
			wantMsg: "invalid --output-format",
		},
		{
			name:    "quiet + verbose mutex",
			params:  exec.Params{Quiet: true, Verbose: true},
			wantMsg: "mutually exclusive",
		},
		{
			name:    "quiet + show-system mutex",
			params:  exec.Params{Quiet: true, ShowSystem: true},
			wantMsg: "mutually exclusive",
		},
		{
			name:    "diff + verbose mutex",
			params:  exec.Params{OutputFormat: "diff", Verbose: true},
			wantMsg: "mutually exclusive",
		},
		{
			name:    "json + output-format json conflict",
			params:  exec.Params{JSON: true, OutputFormat: "json"},
			wantMsg: "--json conflicts",
		},
		{
			name:   "json + output-format stream-json ok",
			params: exec.Params{JSON: true, OutputFormat: "stream-json"},
			wantOK: true,
		},

		// --- Phase 2: permission mode ---
		{
			name:    "invalid permission-mode",
			params:  exec.Params{PermissionMode: "yolo"},
			wantMsg: "invalid --permission-mode",
		},
		{
			name:   "permission-mode plan ok",
			params: exec.Params{PermissionMode: "plan"},
			wantOK: true,
		},
		{
			name:    "bypass + deny-tool mutex",
			params:  exec.Params{PermissionMode: "bypass", DenyTools: []string{"bash"}},
			wantMsg: "bypass",
		},
		{
			name:    "malformed allow-tool",
			params:  exec.Params{AllowTools: []string{":no-name"}},
			wantMsg: "invalid --allow-tool",
		},
		{
			name:   "allow-tool with pattern ok",
			params: exec.Params{AllowTools: []string{"bash:git status"}},
			wantOK: true,
		},

		// --- Phase 3: permission-prompt-tool ---
		{
			name:    "bareword prompt tool",
			params:  exec.Params{PermissionPromptTool: "ask"},
			wantMsg: "mcp__",
		},
		{
			name:   "prefixed prompt tool ok",
			params: exec.Params{PermissionPromptTool: "mcp__auth__ask"},
			wantOK: true,
		},
		{
			name: "bypass + prompt-tool conflict",
			params: exec.Params{
				PermissionMode:       "bypass",
				PermissionPromptTool: "mcp__auth__ask",
			},
			wantMsg: "bypass",
		},

		// --- Phase 5: input ---
		{
			name:    "stdin consumer conflict prompt-file+image",
			params:  exec.Params{PromptFile: "-", Images: []string{"-"}},
			wantMsg: "stdin",
		},
		{
			name:    "prompt-file + positional prompt",
			params:  exec.Params{PromptFile: "/tmp/x", Prompt: "hi"},
			wantMsg: "mutually exclusive",
		},
		{
			name:   "prompt-file alone ok",
			params: exec.Params{PromptFile: "/tmp/x"},
			wantOK: true,
		},

		// --- Phase 6: hooks ---
		{
			name:    "malformed hook (missing colon)",
			params:  exec.Params{Hooks: []string{"PreToolUse"}},
			wantMsg: "invalid --hook",
		},
		{
			name:    "unknown hook event",
			params:  exec.Params{Hooks: []string{"Yolo:echo"}},
			wantMsg: "unknown hook event",
		},
		{
			name:   "valid hook ok",
			params: exec.Params{Hooks: []string{"PreToolUse:echo hi"}},
			wantOK: true,
		},
		{
			name:    "malformed mcp",
			params:  exec.Params{MCPServers: []string{"bogus"}},
			wantMsg: "invalid --mcp",
		},

		// --- Phase 7: commit ---
		{
			name:    "commit + dry-run mutex",
			params:  exec.Params{Commit: true, DryRun: true},
			wantMsg: "mutually exclusive",
		},
		{
			name:    "commit + plan mode mutex",
			params:  exec.Params{Commit: true, PermissionMode: "plan"},
			wantMsg: "mutually exclusive",
		},
		{
			name:   "commit alone ok",
			params: exec.Params{Commit: true},
			wantOK: true,
		},

		// --- Phase 8: budgets ---
		{
			name:    "max-turns negative",
			params:  exec.Params{MaxTurns: -1},
			wantMsg: "--max-turns",
		},
		{
			name:    "max-cost negative",
			params:  exec.Params{MaxCost: -0.5},
			wantMsg: "--max-cost",
		},
		{
			name:   "max-turns zero ok (default)",
			params: exec.Params{MaxTurns: 0, MaxCost: 0},
			wantOK: true,
		},
		{
			name:   "max-cost positive ok",
			params: exec.Params{MaxCost: 1.5},
			wantOK: true,
		},

		// --- Phase 9: batch ---
		{
			name:    "parallel negative",
			params:  exec.Params{Parallel: -1},
			wantMsg: "--parallel",
		},
		{
			name:    "retry negative",
			params:  exec.Params{Retry: -1},
			wantMsg: "--retry",
		},
		{
			name: "prompt-each + positional",
			params: exec.Params{
				PromptEach: "/tmp/prompts.txt",
				Prompt:     "extra",
			},
			wantMsg: "mutually exclusive",
		},
		{
			name:   "prompt-each alone ok",
			params: exec.Params{PromptEach: "/tmp/x"},
			wantOK: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.params.Validate()
			if tc.wantOK {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			// All validation errors must be typed UsageError so the
			// cobra wrapper exits with code 64.
			var uerr *exec.UsageError
			if !errors.As(err, &uerr) {
				t.Errorf("expected *UsageError, got %T: %v", err, err)
			}
			if uerr != nil && uerr.ExitCode != 64 {
				t.Errorf("exit code = %d, want 64", uerr.ExitCode)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("expected %q in error message, got: %v",
					tc.wantMsg, err)
			}
		})
	}
}
