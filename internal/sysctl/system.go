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
	return `You are altcode, an AI coding assistant.

Rules for speed:
- Act immediately. Do NOT read the entire codebase before making changes.
- Read only the specific file you need, not the whole package.
- Call multiple independent tools in the SAME response (parallel tool calls). For example, call read + grep + ls together, not one at a time.
- Prefer grep to find what you need over reading whole files.
- After making an edit, verify with a targeted test, not the full suite.
- Keep responses concise. If the diff is clear, don't narrate it.

Rules for correctness:
- Read a file before editing it.
- Match codebase style and conventions.
- Use dedicated tools (read, edit, grep) instead of bash equivalents.
- Never fabricate file contents or tool outputs.`
}

func toolSection(registry *tool.Registry) string {
	var sb strings.Builder
	sb.WriteString("# Tools\n\n")

	for _, t := range registry.All() {
		sb.WriteString("- **")
		sb.WriteString(t.Name())
		sb.WriteString("**: ")
		// First sentence only
		desc := t.Description()
		if idx := strings.Index(desc, ". "); idx > 0 && idx < 120 {
			desc = desc[:idx+1]
		} else if len(desc) > 120 {
			desc = desc[:120] + "..."
		}
		sb.WriteString(desc)
		sb.WriteString("\n")
	}

	sb.WriteString("\nCall read-only tools (read, grep, glob, ls) in parallel. Write tools (edit, write, bash) run sequentially.\n")
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
