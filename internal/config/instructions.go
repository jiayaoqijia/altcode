package config

import (
	"os"
	"path/filepath"
	"sort"
)

// Instruction represents a single instruction document loaded from disk.
type Instruction struct {
	Path    string
	Content string
}

// LoadInstructions loads instruction files in cascade order:
//  1. ~/.config/altcode/instructions.md  (user-global)
//  2. <root>/CLAUDE.md
//  3. <root>/AGENTS.md
//  4. <root>/ALTCODE.md
//  5. <root>/.altcode/rules/*.md  (sorted alphabetically)
//
// Missing files are silently skipped. Errors reading existing files are
// returned immediately.
func LoadInstructions(root string) ([]Instruction, error) {
	paths, err := buildInstructionPaths(root)
	if err != nil {
		return nil, err
	}

	var result []Instruction
	for _, p := range paths {
		inst, err := readInstruction(p)
		if err != nil {
			return nil, err
		}
		if inst != nil {
			result = append(result, *inst)
		}
	}
	return result, nil
}

func buildInstructionPaths(root string) ([]string, error) {
	paths := []string{
		globalInstructionsPath(),
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(root, "ALTCODE.md"),
	}

	rulesDir := filepath.Join(root, ".altcode", "rules")
	ruleFiles, err := globMarkdown(rulesDir)
	if err != nil {
		return nil, err
	}
	paths = append(paths, ruleFiles...)
	return paths, nil
}

func globalInstructionsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "altcode", "instructions.md")
}

// globMarkdown returns sorted .md files inside dir, or nil if dir does not exist.
func globMarkdown(dir string) ([]string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// readInstruction reads a file, returning nil if it does not exist.
func readInstruction(path string) (*Instruction, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Instruction{Path: path, Content: string(data)}, nil
}
