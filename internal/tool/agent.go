package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type agentTool struct{}

// NewAgentTool creates a tool that spawns subagent processes to handle
// complex tasks autonomously. Matches Claude Code's Agent tool behavior.
func NewAgentTool() Tool { return &agentTool{} }

func (t *agentTool) Name() string { return "Agent" }
func (t *agentTool) Description() string {
	return "Launch a subagent to handle a complex task autonomously. " +
		"The agent runs as a separate process with its own context. " +
		"Use for: parallel research, code review, testing, exploration. " +
		"The agent has access to Read, Grep, Glob, Bash tools but not Edit/Write."
}
func (t *agentTool) IsConcurrencySafe() bool { return true }
func (t *agentTool) IsReadOnly() bool        { return true }
func (t *agentTool) PermissionPattern(input json.RawMessage) string {
	var p struct{ Prompt string `json:"prompt"` }
	json.Unmarshal(input, &p)
	prompt := p.Prompt
	if len(prompt) > 50 {
		prompt = prompt[:47] + "..."
	}
	return "Agent(" + prompt + ")"
}

func (t *agentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "The task for the subagent to perform"
			},
			"description": {
				"type": "string",
				"description": "Short (3-5 word) description of the task"
			},
			"backend": {
				"type": "string",
				"description": "Which agent backend to use: claude, codex, altcode. Default: auto-detect best available."
			},
			"model": {
				"type": "string",
				"description": "Model override for the subagent, e.g. minimax/MiniMax-M2.7, kimi/kimi-k2, zhipu/glm-4.7"
			}
		},
		"required": ["prompt"]
	}`)
}

func (t *agentTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Prompt      string `json:"prompt"`
		Description string `json:"description"`
		Backend     string `json:"backend"`
		Model       string `json:"model"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	if strings.TrimSpace(params.Prompt) == "" {
		return &Result{Error: fmt.Errorf("prompt is required")}, nil
	}

	// Find backend: explicit > auto-detect
	var binary, backendName string
	if params.Backend != "" {
		binary, backendName = findSpecificBinary(params.Backend)
	}
	if binary == "" {
		binary, backendName = findAgentBinary()
	}
	if binary == "" {
		return &Result{
			Output: "No agent backend found. Install claude, codex, or altcode CLI.",
			Error:  fmt.Errorf("no agent backend on PATH"),
		}, nil
	}

	// Build command with optional model override
	argv := buildAgentArgv(binary, backendName, params.Prompt)
	if params.Model != "" {
		switch backendName {
		case "altcode":
			// altcode --model minimax/MiniMax-M2.7 --json "prompt"
			argv = []string{binary, "--model", params.Model, "--json", params.Prompt}
		case "claude":
			// claude --model claude-opus-4-6 -p "prompt"
			argv = []string{binary,
				"--permission-mode", "bypassPermissions",
				"--model", params.Model,
				"--max-turns", "10",
				"-p", params.Prompt}
		}
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, argv[0], argv[1:]...)
	// Subagents (claude, codex, altcode) commonly fork their own
	// children — shell wrappers, MCP servers, LSPs. Without a process
	// group, exec.CommandContext only kills the direct child on
	// timeout, leaking the grandchildren. Reuse the same helper as
	// internal/tool/bash.go.
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start agent: %w", err)
	}

	// Stream stdout
	var output strings.Builder
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		output.WriteString(scanner.Text() + "\n")
	}

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	desc := params.Description
	if desc == "" {
		desc = params.Prompt
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
	}

	title := fmt.Sprintf("Agent(%s) %s", backendName, desc)

	if exitCode != 0 {
		return &Result{
			Output: truncateOutput(output.String()),
			Title:  title,
			Error:  fmt.Errorf("agent exited with code %d", exitCode),
		}, nil
	}

	return &Result{
		Output: truncateOutput(output.String()),
		Title:  title,
	}, nil
}

// findSpecificBinary looks for a specific backend by name.
func findSpecificBinary(name string) (binary, backendName string) {
	binName := name
	// Map logical names to binary names
	switch name {
	case "cc", "claude-code":
		binName = "claude"
		name = "claude"
	}
	if p, err := exec.LookPath(binName); err == nil {
		return p, name
	}
	return "", ""
}

// findAgentBinary probes PATH for agent CLIs in preference order.
func findAgentBinary() (binary, name string) {
	for _, probe := range []struct{ binary, name string }{
		{"claude", "claude"},
		{"codex", "codex"},
		{"altcode", "altcode"},
		{"opencode", "opencode"},
	} {
		if p, err := exec.LookPath(probe.binary); err == nil {
			return p, probe.name
		}
	}
	return "", ""
}

// buildAgentArgv constructs the argv for a subagent invocation.
func buildAgentArgv(binary, name, prompt string) []string {
	switch name {
	case "claude":
		return []string{binary,
			"--permission-mode", "bypassPermissions",
			"--max-turns", "10",
			"-p", prompt,
		}
	case "codex":
		return []string{binary, "exec",
			"--dangerously-bypass-approvals-and-sandbox",
			prompt,
		}
	case "altcode":
		// altcode as subagent: headless mode with JSON output
		return []string{binary, "--json", prompt}
	default:
		return []string{binary, "exec", prompt}
	}
}
