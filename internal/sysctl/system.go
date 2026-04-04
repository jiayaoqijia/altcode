package sysctl

import (
	"strings"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/tool"
)

// BuildSystemPrompt assembles the system prompt sections from config,
// tools, instructions, and environment context.
func BuildSystemPrompt(
	cfg *config.Config,
	tools *tool.Registry,
	instructions []config.Instruction,
	env EnvContext,
) []provider.SystemSection {
	var sections []provider.SystemSection

	// Core persona — cacheable, static across turns
	sections = append(sections, provider.SystemSection{
		Content:      corePersona(),
		CacheControl: &provider.CacheControl{Type: "ephemeral"},
	})

	// Tool descriptions — cacheable, static across turns
	sections = append(sections, provider.SystemSection{
		Content:      toolSection(tools),
		CacheControl: &provider.CacheControl{Type: "ephemeral"},
	})

	// User/project instructions — cacheable per session
	for _, inst := range instructions {
		sections = append(sections, provider.SystemSection{
			Content:      "# " + inst.Path + "\n\n" + inst.Content,
			CacheControl: &provider.CacheControl{Type: "ephemeral"},
		})
	}

	// Environment — dynamic, changes between turns
	sections = append(sections, provider.SystemSection{
		Content: envSection(env),
	})

	return sections
}

func corePersona() string {
	return `You are altcode, an AI coding assistant. Be concise. Read before editing. Match codebase style. Use dedicated tools (read, edit, grep) over bash equivalents. Batch independent tool calls. Never fabricate outputs.`
}

func toolSection(registry *tool.Registry) string {
	var sb strings.Builder
	sb.WriteString("# Available tools\n\n")
	sb.WriteString("You have access to these tools. Use them to accomplish tasks.\n\n")

	for _, t := range registry.All() {
		sb.WriteString("## ")
		sb.WriteString(t.Name())
		sb.WriteString("\n")
		sb.WriteString(t.Description())
		sb.WriteString("\n\n")
	}

	sb.WriteString(`## Tool usage guidelines

- **read** is concurrency-safe and read-only. Use it freely.
- **glob** finds files by pattern. Use before creating new files to check if similar ones exist.
- **grep** searches file contents. Use it to find implementations, references, and patterns.
- **ls** lists directory contents. Use it to understand project structure.
- **bash** executes shell commands. NOT concurrency-safe. Use for builds, tests, git operations, and commands that don't have dedicated tools.
- **edit** performs exact string replacement. NOT concurrency-safe. Provide enough context in old_string to make the match unique.
- **write** creates or overwrites files. NOT concurrency-safe. Use for new files only — prefer edit for modifications.

When multiple read-only tools are needed, call them in parallel for efficiency.
When write tools are needed, they run sequentially to avoid conflicts.
`)

	return sb.String()
}

// SkillInfo describes a skill for the system prompt.
type SkillInfo struct {
	Name        string
	Description string
	Path        string
}

// SkillsSection builds a compact system prompt section listing available skills.
// Only includes name and file path — the model reads SKILL.md on demand.
// This keeps the system prompt small for faster first-token latency.
func SkillsSection(skills []SkillInfo) string {
	var sb strings.Builder
	sb.WriteString("# Skills\n\nTo use a skill: read its SKILL.md with the read tool, then follow its instructions.\n\n")

	for _, s := range skills {
		sb.WriteString("- ")
		sb.WriteString(s.Name)
		if s.Path != "" {
			sb.WriteString(" → ")
			sb.WriteString(s.Path)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func envSection(env EnvContext) string {
	return "# Environment\n\n" +
		"- Working directory: " + env.WorkDir + "\n" +
		"- Date: " + env.Date + "\n" +
		"- Platform: " + env.Platform + "\n"
}
