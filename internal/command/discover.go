package command

import (
	"os"
	"path/filepath"
	"strings"
)

// Discover finds all command/skill files in the given directories.
// Supports two layouts:
//   - Flat: dir/*.md (Claude Code commands format)
//   - Nested: dir/<name>/SKILL.md (Claude Code skills format)
//
// Later directories override earlier ones if commands share a name.
func Discover(dirs ...string) ([]*Command, error) {
	byName := make(map[string]*Command)

	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				// Nested skill: dir/<name>/SKILL.md
				skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
				if cmd, err := ParseFile(skillPath); err == nil {
					cmd.Name = e.Name() // use directory name
					byName[cmd.Name] = cmd
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			cmd, err := ParseFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			byName[cmd.Name] = cmd
		}
	}

	result := make([]*Command, 0, len(byName))
	for _, cmd := range byName {
		result = append(result, cmd)
	}
	return result, nil
}
