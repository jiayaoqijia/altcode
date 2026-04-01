package sysctl

import (
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

	sections = append(sections, provider.SystemSection{
		Content:      corePersona(),
		CacheControl: &provider.CacheControl{Type: "ephemeral"},
	})

	sections = append(sections, provider.SystemSection{
		Content:      toolDescriptions(tools),
		CacheControl: &provider.CacheControl{Type: "ephemeral"},
	})

	for _, inst := range instructions {
		sections = append(sections, provider.SystemSection{
			Content:      "# " + inst.Path + "\n\n" + inst.Content,
			CacheControl: &provider.CacheControl{Type: "ephemeral"},
		})
	}

	sections = append(sections, provider.SystemSection{
		Content: envSection(env),
	})

	return sections
}

func corePersona() string {
	return `You are an expert coding assistant. You help users with software engineering tasks including writing code, debugging, refactoring, and explaining code.

Key behaviors:
- Be concise and direct
- Write clean, idiomatic code
- Explain your reasoning when making non-obvious choices
- Use tools to read files before modifying them
- Prefer editing existing files over creating new ones`
}

func toolDescriptions(registry *tool.Registry) string {
	var desc string
	for _, t := range registry.All() {
		desc += "## " + t.Name() + "\n" + t.Description() + "\n\n"
	}
	return desc
}

func envSection(env EnvContext) string {
	return "# Environment\n" +
		"Working directory: " + env.WorkDir + "\n" +
		"Date: " + env.Date + "\n" +
		"Platform: " + env.Platform + "\n"
}
