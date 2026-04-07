package orchestra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InjectContext writes provider-native context files into the agent's workdir.
func InjectContext(workDir, provider, role, task string, priorOutputs map[string]string) error {
	content := buildContextContent(role, task, priorOutputs)
	switch provider {
	case "claude":
		return appendOrCreate(filepath.Join(workDir, "CLAUDE.md"), content)
	case "codex", "opencode":
		return appendOrCreate(filepath.Join(workDir, "AGENTS.md"), content)
	default:
		return appendOrCreate(filepath.Join(workDir, "AGENTS.md"), content)
	}
}

func buildContextContent(role, task string, priorOutputs map[string]string) string {
	var b strings.Builder
	b.WriteString("\n# Altcode Workflow Context\n\n")
	fmt.Fprintf(&b, "**Role:** %s\n\n", role)
	fmt.Fprintf(&b, "**Task:** %s\n\n", task)

	if len(priorOutputs) > 0 {
		b.WriteString("## Prior Phase Outputs\n\n")
		for phase, output := range priorOutputs {
			fmt.Fprintf(&b, "### %s\n\n", phase)
			if len(output) > 32768 {
				output = output[:32768] + "\n\n[truncated]"
			}
			b.WriteString(output)
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func appendOrCreate(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
