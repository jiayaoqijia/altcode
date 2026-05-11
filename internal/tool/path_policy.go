package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pathPolicy struct {
	root string
}

func newPathPolicy(root string) pathPolicy {
	if root == "" {
		return pathPolicy{}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return pathPolicy{root: canonicalPath(filepath.Clean(abs))}
}

func firstRoot(root []string) string {
	if len(root) == 0 {
		return ""
	}
	return root[0]
}

func (p pathPolicy) resolve(raw, fallback string) (string, error) {
	if p.root == "" {
		if raw == "" {
			return fallback, nil
		}
		return raw, nil
	}

	target := raw
	if target == "" {
		target = p.root
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(p.root, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	abs = canonicalPath(abs)

	if isUnsafeTraversalRoot(abs) {
		return "", fmt.Errorf("refusing to traverse protected root %s", abs)
	}
	if !withinRoot(p.root, abs) {
		return "", fmt.Errorf("path %s is outside project root %s", abs, p.root)
	}
	return abs, nil
}

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return filepath.Clean(resolved)
}

func (p pathPolicy) display(path string) string {
	if p.root == "" {
		return path
	}
	rel, err := filepath.Rel(p.root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return path
	}
	return rel
}

func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isUnsafeTraversalRoot(path string) bool {
	clean := filepath.Clean(path)
	if clean == string(filepath.Separator) || clean == filepath.Clean(string(filepath.Separator)+"Users") {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	home = filepath.Clean(home)
	if clean == home {
		return true
	}
	for _, name := range []string{
		"Desktop",
		"Documents",
		"Downloads",
		"Library",
		"Movies",
		"Music",
		"Pictures",
		"Photos",
		filepath.Join("Library", "Mobile Documents"),
	} {
		if clean == filepath.Join(home, name) {
			return true
		}
	}
	return false
}

func shouldSkipWalkDir(root, path string, d os.DirEntry) bool {
	if !d.IsDir() {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	rel = filepath.ToSlash(rel)
	base := d.Name()
	switch base {
	case ".git", "node_modules", "vendor", ".cache",
		"Library", "Music", "Pictures", "Movies",
		"Desktop", "Documents", "Downloads", "Photos":
		return true
	}
	return rel == "Library/Mobile Documents" ||
		strings.HasPrefix(rel, "Library/Mobile Documents/")
}

func grepExcludeDirArgs() []string {
	dirs := []string{
		".git", "node_modules", "vendor", ".cache",
		"Library", "Music", "Pictures", "Movies",
		"Desktop", "Documents", "Downloads", "Photos",
	}
	args := make([]string, 0, len(dirs)*4+4)
	for _, dir := range dirs {
		args = append(args, "--glob", "!"+dir+"/**")
		args = append(args, "--glob", "!**/"+dir+"/**")
	}
	args = append(args, "--glob", "!Library/Mobile Documents/**")
	args = append(args, "--glob", "!**/Library/Mobile Documents/**")
	return args
}
