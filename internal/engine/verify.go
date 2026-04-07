package engine

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// VerifyLevel represents a step in the verification ladder.
type VerifyLevel int

const (
	VerifyBuild   VerifyLevel = iota // go build (cheapest)
	VerifyVet                        // go vet (catches common issues)
	VerifyTest                       // targeted test
	VerifyPackage                    // package-level test
	VerifyLint                       // optional lint
)

// VerifyResult holds the outcome of a verification step.
type VerifyResult struct {
	Level   VerifyLevel
	Passed  bool
	Output  string
	Elapsed time.Duration
}

// RunVerificationLadder executes verification steps in order from cheapest
// to most expensive, stopping at the first failure.
// Implements Section 12 of the world-class design doc.
func RunVerificationLadder(ctx context.Context, dir string, levels []VerifyLevel) []VerifyResult {
	var results []VerifyResult
	for _, level := range levels {
		r := runVerifyStep(ctx, dir, level)
		results = append(results, r)
		if !r.Passed {
			break // stop at first failure (ladder principle)
		}
	}
	return results
}

// DefaultVerificationLadder returns the standard verification steps for Go.
func DefaultVerificationLadder() []VerifyLevel {
	return []VerifyLevel{VerifyBuild, VerifyVet, VerifyTest}
}

func runVerifyStep(ctx context.Context, dir string, level VerifyLevel) VerifyResult {
	start := time.Now()
	var cmd *exec.Cmd

	switch level {
	case VerifyBuild:
		cmd = exec.CommandContext(ctx, "go", "build", "./...")
	case VerifyVet:
		cmd = exec.CommandContext(ctx, "go", "vet", "./...")
	case VerifyTest:
		// Run tests for the package containing the directory
		pkg := "./" + filepath.Base(dir) + "/..."
		if dir == "." || dir == "" {
			pkg = "./..."
		}
		cmd = exec.CommandContext(ctx, "go", "test", pkg, "-count=1", "-timeout=60s")
	case VerifyPackage:
		cmd = exec.CommandContext(ctx, "go", "test", "./...", "-count=1", "-timeout=120s")
	case VerifyLint:
		cmd = exec.CommandContext(ctx, "go", "vet", "./...")
	default:
		return VerifyResult{Level: level, Passed: true, Elapsed: time.Since(start)}
	}

	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GOFLAGS=-mod=mod")

	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	passed := err == nil
	output := strings.TrimSpace(string(out))
	if len(output) > 500 {
		output = output[:500] + "..."
	}

	return VerifyResult{
		Level:   level,
		Passed:  passed,
		Output:  output,
		Elapsed: elapsed,
	}
}

// FormatVerificationResults returns a human-readable summary.
func FormatVerificationResults(results []VerifyResult) string {
	if len(results) == 0 {
		return "no verification run"
	}
	var sb strings.Builder
	for _, r := range results {
		icon := "✓"
		if !r.Passed {
			icon = "✗"
		}
		name := verifyLevelName(r.Level)
		sb.WriteString(fmt.Sprintf("  %s %s (%dms)\n", icon, name, r.Elapsed.Milliseconds()))
		if !r.Passed && r.Output != "" {
			sb.WriteString("    " + strings.ReplaceAll(r.Output, "\n", "\n    ") + "\n")
		}
	}
	return sb.String()
}

func verifyLevelName(l VerifyLevel) string {
	switch l {
	case VerifyBuild:
		return "build"
	case VerifyVet:
		return "vet"
	case VerifyTest:
		return "test (targeted)"
	case VerifyPackage:
		return "test (package)"
	case VerifyLint:
		return "lint"
	default:
		return "unknown"
	}
}
