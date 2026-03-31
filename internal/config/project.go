package config

import (
	"os"
	"path/filepath"
)

// DetectProjectRoot walks up from startDir looking for a .git directory.
// If none is found before reaching the filesystem root, startDir is returned.
func DetectProjectRoot(startDir string) string {
	dir := startDir
	for {
		if isGitRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding .git.
			return startDir
		}
		dir = parent
	}
}

func isGitRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}
