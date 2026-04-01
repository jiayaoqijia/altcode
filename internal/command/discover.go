package command

import (
	"os"
	"path/filepath"
	"strings"
)

// Discover finds all .md command files in the given directories.
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
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
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
