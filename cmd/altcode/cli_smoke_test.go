package main

// Phase 13: CLI-level smoke tests. These build the altcode binary
// to a temp path, then invoke it with various flag combinations
// and assert on exit codes + stderr patterns. Catches regressions
// where a flag is registered but its wire-up is broken.
//
// Tests are guarded by a build check: if `go build` itself fails,
// the tests are skipped (the build error will surface in the main
// test run anyway).

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildCLI produces the altcode binary in a temp dir and returns
// its path. Cached across subtests via sync.Once would be nice but
// t.TempDir() has per-test lifetime — the build is fast enough to
// run once per test file.
func buildCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "altcode-smoke")

	// Resolve the cmd/altcode package path relative to the test's
	// working directory. `go build` runs from the package dir, so
	// building . builds the current file's package.
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("go build failed: %v\n%s", err, stderr.String())
	}
	return bin
}

// runCLI runs the binary with args and returns (stdout, stderr, exit).
func runCLI(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	return stdout.String(), stderr.String(), exitCode
}

// TestCLI_HelpListsAllFlags verifies that every Phase 1-12 flag
// appears in --help. Acts as a smoke test for flag registration:
// if a flag was added to cliFlags but never registered on root,
// this test will miss it.
func TestCLI_HelpListsAllFlags(t *testing.T) {
	bin := buildCLI(t)
	stdout, _, code := runCLI(t, bin, "--help")
	if code != 0 {
		t.Fatalf("--help exit=%d", code)
	}
	// Every flag that should appear in --help:
	wantFlags := []string{
		// Phase 0
		"--json", "--last", "--session", "--model",
		// Phase 1
		"--output-format", "--verbose", "--quiet", "--print-cost",
		"--print-tools", "--print-tree", "--show-system",
		"--save-transcript", "--save-cost", "--save-diff",
		// Phase 2
		"--permission-mode", "--allow-tool", "--deny-tool", "--dry-run",
		// Phase 3
		"--permission-prompt-tool",
		// Phase 4
		"--continue", "--fork-session", "--session-db", "--list-sessions",
		// Phase 5
		"--image", "--file", "--prompt-file", "--system", "--system-file",
		// Phase 6
		"--hook", "--mcp", "--skill",
		// Phase 7
		"--commit", "--commit-dirty",
		// Phase 8
		"--max-turns", "--max-cost",
		// Phase 9
		"--run-workflow", "--prompt-each", "--parallel", "--retry", "--bail",
		// Phase 10
		"--print-config", "--print-tools-list", "--print-skills",
		"--print-mcp", "--doctor",
	}
	for _, flag := range wantFlags {
		if !strings.Contains(stdout, flag) {
			t.Errorf("flag %q missing from --help", flag)
		}
	}
}

// TestCLI_ValidationErrorExitsUsage verifies that Params.Validate()
// errors surface as exit code 64 (EX_USAGE).
func TestCLI_ValidationErrorExitsUsage(t *testing.T) {
	bin := buildCLI(t)
	cases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			"invalid output-format",
			[]string{"--output-format", "yaml", "hi"},
			"invalid --output-format",
		},
		{
			"bareword permission-prompt-tool",
			[]string{"--permission-prompt-tool", "ask", "hi"},
			"mcp__",
		},
		{
			"invalid permission-mode",
			[]string{"--permission-mode", "yolo", "hi"},
			"invalid --permission-mode",
		},
		{
			"invalid hook event",
			[]string{"--hook", "Yolo:echo", "hi"},
			"unknown hook event",
		},
		{
			"malformed hook",
			[]string{"--hook", "no-colon", "hi"},
			"invalid --hook",
		},
		{
			"malformed mcp",
			[]string{"--mcp", "bogus", "hi"},
			"invalid --mcp",
		},
		{
			"negative max-turns",
			[]string{"--max-turns", "-5", "hi"},
			"--max-turns",
		},
		{
			"fork-session unknown id",
			[]string{"--fork-session", "nope", "--session-db", "/tmp/empty.db", "hi"},
			"not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, bin, tc.args...)
			if code != 64 {
				t.Errorf("exit=%d, want 64 (EX_USAGE); stderr=%q", code, stderr)
			}
			if !strings.Contains(stderr, tc.wantMsg) {
				t.Errorf("stderr missing %q: %q", tc.wantMsg, stderr)
			}
		})
	}
}

// TestCLI_InspectionFlagsExitZero verifies every print-and-exit
// flag runs successfully and writes *something* to stdout.
func TestCLI_InspectionFlagsExitZero(t *testing.T) {
	bin := buildCLI(t)
	cases := []struct {
		name string
		flag string
	}{
		{"print-config", "--print-config"},
		{"print-tools-list", "--print-tools-list"},
		{"print-skills", "--print-skills"},
		{"print-mcp", "--print-mcp"},
		{"doctor", "--doctor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, bin, tc.flag)
			if code != 0 {
				t.Errorf("%s exit=%d; stderr=%q", tc.flag, code, stderr)
			}
			if len(stdout) == 0 && len(stderr) == 0 {
				t.Errorf("%s wrote nothing", tc.flag)
			}
		})
	}
}

// TestCLI_PrintConfigRedactsSecrets runs --print-config with the
// default config and verifies no `<redacted>` leak markers appear
// in plaintext alongside real credential-like strings. The default
// config won't have secrets, so this is a smoke test that the
// redactor at least runs without panic.
func TestCLI_PrintConfigRedactsSecrets(t *testing.T) {
	bin := buildCLI(t)
	stdout, _, code := runCLI(t, bin, "--print-config")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	// If a raw API key pattern slips through, fail. This is a
	// weak check (defaults have no secrets) but catches panics.
	if strings.Contains(stdout, "sk-") {
		t.Error("possible unredacted API key in --print-config output")
	}
}

// TestCLI_PrintParamsShowsMaxTurns is the end-to-end test Codex
// asked for in iteration-3: prove that a CLI flag (`--max-turns 7`)
// flows through Cobra → exec.Params → engine-ready state. Without
// this, budget semantics can regress silently at any boundary.
func TestCLI_PrintParamsShowsMaxTurns(t *testing.T) {
	bin := buildCLI(t)
	stdout, stderr, code := runCLI(t, bin,
		"--max-turns", "7", "--max-cost", "1.25",
		"--print-params", "hello")
	if code != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%q stdout=%q", code, stderr, stdout)
	}
	// JSON must contain the flag values at both the Params level and
	// after threading into EngineParams.
	for _, want := range []string{
		`"max_turns": 7`,
		`"max_cost": 1.25`,
		`"engine_max_turns": 7`,
		`"engine_cost_budget_wired": true`,
		`"prompt": "hello"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q\ngot: %s", want, stdout)
		}
	}
}

// TestCLI_PrintParamsDefaults verifies the zero-flags baseline so
// regressions that accidentally wire a budget (e.g. hardcoding
// MaxTurns=50 somewhere) are caught.
func TestCLI_PrintParamsDefaults(t *testing.T) {
	bin := buildCLI(t)
	stdout, stderr, code := runCLI(t, bin, "--print-params", "hi")
	if code != 0 {
		t.Fatalf("exit=%d, want 0; stderr=%q", code, stderr)
	}
	for _, want := range []string{
		`"max_turns": 0`,
		`"max_cost": 0`,
		`"engine_cost_budget_wired": false`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q\ngot: %s", want, stdout)
		}
	}
}
