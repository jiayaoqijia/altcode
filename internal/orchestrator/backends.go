package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/config"
)

// Backend represents an external coding CLI that altcode can delegate to.
type Backend struct {
	Name    string // "claude", "codex", "gemini", etc.
	Binary  string // path to binary
	Version string // detected version
}

// DetectBackends finds all installed coding CLIs.
func DetectBackends() []Backend {
	candidates := []struct {
		name    string
		binaries []string
		versionFlag string
	}{
		{"claude", []string{"claude"}, "--version"},
		{"codex", []string{"codex"}, "--version"},
		{"gemini", []string{"gemini"}, "--version"},
		{"aider", []string{"aider"}, "--version"},
		{"opencode", []string{"opencode"}, "--version"},
		{"cursor", []string{"cursor"}, "--version"},
	}

	var backends []Backend
	for _, c := range candidates {
		for _, bin := range c.binaries {
			path, err := exec.LookPath(bin)
			if err != nil {
				continue
			}
			ver := detectVersion(path, c.versionFlag)
			backends = append(backends, Backend{
				Name: c.name, Binary: path, Version: ver,
			})
			break
		}
	}
	return backends
}

func detectVersion(binary, flag string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, flag).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// CallBackend sends a prompt to an external coding CLI and returns its response.
func CallBackend(ctx context.Context, backend Backend, prompt string) (string, error) {
	switch backend.Name {
	case "claude":
		return callClaude(ctx, backend.Binary, prompt)
	case "codex":
		return callCodex(ctx, backend.Binary, prompt)
	default:
		return callGeneric(ctx, backend.Binary, prompt)
	}
}

func callClaude(ctx context.Context, binary, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, "-p", prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("claude: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func callCodex(ctx context.Context, binary, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, "exec",
		"--dangerously-bypass-approvals-and-sandbox", prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("codex: %w: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

func callGeneric(ctx context.Context, binary, prompt string) (string, error) {
	// Try common patterns: binary "prompt", binary -p "prompt", binary exec "prompt"
	for _, args := range [][]string{
		{prompt},
		{"-p", prompt},
		{"exec", prompt},
	} {
		cmd := exec.CommandContext(ctx, binary, args...)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("%s: could not execute", binary)
}

// BackendAssignment maps a role to an external coding CLI.
type BackendAssignment struct {
	Role    Role
	Backend Backend
}

// RunBackendsParallel executes a prompt through multiple coding CLIs in parallel.
func RunBackendsParallel(ctx context.Context, assignments []BackendAssignment, prompt string) []Finding {
	type result struct {
		finding Finding
	}

	ch := make(chan result, len(assignments))
	for _, a := range assignments {
		go func(assign BackendAssignment) {
			rolePrompt := roleSystemPrompt(assign.Role) + "\n\n" + prompt
			text, err := CallBackend(ctx, assign.Backend, rolePrompt)
			if err != nil {
				ch <- result{Finding{
					Model: assign.Backend.Name, Role: assign.Role,
					Type: "error", Content: err.Error(),
				}}
				return
			}
			ch <- result{Finding{
				Model: assign.Backend.Name, Role: assign.Role,
				Type: classifyResponse(text), Content: text,
				Confidence: 0.8,
			}}
		}(a)
	}

	var findings []Finding
	for range assignments {
		r := <-ch
		findings = append(findings, r.finding)
	}
	return findings
}

// BackendInfo returns a summary of an installed backend.
func (b Backend) String() string {
	return fmt.Sprintf("%s (%s) at %s", b.Name, b.Version, b.Binary)
}

// BackendsSummary returns a formatted list of all detected backends.
func BackendsSummary(backends []Backend) string {
	if len(backends) == 0 {
		return "No coding CLIs detected."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Detected %d coding CLI(s):\n", len(backends)))
	for _, b := range backends {
		sb.WriteString(fmt.Sprintf("  %-10s %s (%s)\n", b.Name, b.Binary, b.Version))
	}
	return sb.String()
}

// AutoTeamFromBackends creates a team config from detected backends.
// Assigns roles based on model strengths.
func AutoTeamFromBackends(backends []Backend) *config.TeamConfig {
	if len(backends) == 0 {
		return nil
	}

	models := make(map[string]config.TeamModel)
	roles := []string{"architect", "reviewer", "challenger", "implementer", "evaluator"}

	for i, b := range backends {
		if i >= len(roles) {
			break
		}
		// Map backend to a model string
		model := backendToModel(b)
		models[roles[i]] = config.TeamModel{Model: model}
	}

	return &config.TeamConfig{
		Name:   "auto",
		Models: models,
	}
}

func backendToModel(b Backend) string {
	switch b.Name {
	case "claude":
		return "anthropic/claude-sonnet-4-20250514"
	case "codex":
		return "openai/gpt-5.4"
	case "gemini":
		return "google/gemini-pro"
	case "aider":
		return "openai/gpt-4"
	default:
		return "openai/gpt-4"
	}
}

// MarshalBackends returns JSON representation of detected backends.
func MarshalBackends(backends []Backend) string {
	data, _ := json.MarshalIndent(backends, "", "  ")
	return string(data)
}
