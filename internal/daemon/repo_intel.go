package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RepoIntel analyzes repositories for the orchestrator.
type RepoIntel struct {
	run    cmdRunner
	logger *slog.Logger
}

// NewRepoIntel creates a RepoIntel with the default command runner.
func NewRepoIntel(logger *slog.Logger) *RepoIntel {
	if logger == nil {
		logger = slog.New(
			slog.NewJSONHandler(os.Stderr, nil),
		)
	}
	return &RepoIntel{
		run:    defaultCmdRunner,
		logger: logger,
	}
}

// skipDirs lists directories excluded from analysis.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
}

// BuildProfile analyzes a repository and returns a RepoProfile.
func (ri *RepoIntel) BuildProfile(
	ctx context.Context, repoDir string,
) (*RepoProfile, error) {
	p := &RepoProfile{}

	langs, err := ri.DetectLanguages(ctx, repoDir)
	if err != nil {
		ri.logger.Warn("detect languages failed", "err", err)
	}
	p.Languages = langs

	files, loc := ri.countFilesAndLOC(ctx, repoDir)
	p.TotalFiles = files
	p.TotalLOC = loc

	p.HasTests = ri.hasTests(ctx, repoDir)
	p.HasCI = ri.hasCI(repoDir)

	return p, nil
}

// DetectLanguages returns primary languages sorted by file count.
func (ri *RepoIntel) DetectLanguages(
	ctx context.Context, repoDir string,
) ([]string, error) {
	counts := map[string]int{}
	err := filepath.WalkDir(
		repoDir,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			lang := extToLang(ext)
			if lang != "" {
				counts[lang]++
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	type langCount struct {
		lang  string
		count int
	}
	var sorted []langCount
	for l, c := range counts {
		sorted = append(sorted, langCount{l, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	var result []string
	for _, lc := range sorted {
		result = append(result, lc.lang)
	}
	return result, nil
}

// DetectTestFramework returns the detected test command or "".
func (ri *RepoIntel) DetectTestFramework(
	ctx context.Context, repoDir string,
) string {
	// Go project
	if fileExists(filepath.Join(repoDir, "go.mod")) {
		return "go test ./..."
	}
	// Node project
	pkg := filepath.Join(repoDir, "package.json")
	if fileExists(pkg) {
		data, err := os.ReadFile(pkg)
		if err == nil {
			content := string(data)
			if strings.Contains(content, "jest") ||
				strings.Contains(content, "vitest") {
				return "npm test"
			}
			if strings.Contains(content, "mocha") {
				return "npm test"
			}
			// Generic npm test
			if strings.Contains(content, `"test"`) {
				return "npm test"
			}
		}
	}
	// Python project
	if fileExists(filepath.Join(repoDir, "pytest.ini")) ||
		fileExists(filepath.Join(repoDir, "setup.py")) ||
		fileExists(filepath.Join(repoDir, "pyproject.toml")) {
		return "pytest"
	}
	// Rust
	if fileExists(filepath.Join(repoDir, "Cargo.toml")) {
		return "cargo test"
	}
	return ""
}

// DetectLintCommand returns the detected lint command or "".
func (ri *RepoIntel) DetectLintCommand(
	ctx context.Context, repoDir string,
) string {
	// Go
	if fileExists(filepath.Join(repoDir, "go.mod")) {
		return "go vet ./..."
	}
	// Node
	pkg := filepath.Join(repoDir, "package.json")
	if fileExists(pkg) {
		data, err := os.ReadFile(pkg)
		if err == nil {
			content := string(data)
			if strings.Contains(content, "eslint") {
				return "npx eslint ."
			}
			if strings.Contains(content, `"lint"`) {
				return "npm run lint"
			}
		}
	}
	// Python
	if fileExists(filepath.Join(repoDir, "pyproject.toml")) {
		data, _ := os.ReadFile(
			filepath.Join(repoDir, "pyproject.toml"),
		)
		if strings.Contains(string(data), "ruff") {
			return "ruff check ."
		}
		return "flake8"
	}
	return ""
}

// IsMonorepo checks for monorepo indicators: multiple go.mod
// files, workspaces in package.json, or Bazel BUILD files.
func (ri *RepoIntel) IsMonorepo(
	ctx context.Context, repoDir string,
) bool {
	// Check for multiple go.mod files.
	goModCount := 0
	_ = filepath.WalkDir(
		repoDir,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if d.Name() == "go.mod" {
				goModCount++
				if goModCount >= 2 {
					return filepath.SkipAll
				}
			}
			return nil
		},
	)
	if goModCount >= 2 {
		return true
	}

	// Check package.json for workspaces.
	pkg := filepath.Join(repoDir, "package.json")
	if fileExists(pkg) {
		data, err := os.ReadFile(pkg)
		if err == nil &&
			strings.Contains(string(data), "workspaces") {
			return true
		}
	}

	// Check for Bazel BUILD at root.
	if fileExists(filepath.Join(repoDir, "BUILD")) ||
		fileExists(filepath.Join(repoDir, "BUILD.bazel")) ||
		fileExists(filepath.Join(repoDir, "WORKSPACE")) {
		return true
	}
	return false
}

// countFilesAndLOC walks the repo and counts source files and
// lines of code, excluding skip directories.
func (ri *RepoIntel) countFilesAndLOC(
	ctx context.Context, repoDir string,
) (files int, loc int) {
	_ = filepath.WalkDir(
		repoDir,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if extToLang(ext) == "" {
				return nil
			}
			files++
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			loc += countLines(string(data))
			return nil
		},
	)
	return files, loc
}

// hasTests checks for test file or directory presence.
func (ri *RepoIntel) hasTests(
	ctx context.Context, repoDir string,
) bool {
	testIndicators := []string{
		"__tests__", "test", "tests", "spec",
	}
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		for _, ind := range testIndicators {
			if name == ind && e.IsDir() {
				return true
			}
		}
	}
	// Check for *_test.go at root level.
	matches, _ := filepath.Glob(
		filepath.Join(repoDir, "*_test.go"),
	)
	return len(matches) > 0
}

// hasCI checks for CI configuration files.
func (ri *RepoIntel) hasCI(repoDir string) bool {
	ciPaths := []string{
		filepath.Join(".github", "workflows"),
		".circleci",
		".gitlab-ci.yml",
		"Jenkinsfile",
		".travis.yml",
	}
	for _, p := range ciPaths {
		full := filepath.Join(repoDir, p)
		if _, err := os.Stat(full); err == nil {
			return true
		}
	}
	return false
}

// extToLang maps file extensions to language names.
func extToLang(ext string) string {
	m := map[string]string{
		".go":    "Go",
		".py":    "Python",
		".js":    "JavaScript",
		".ts":    "TypeScript",
		".tsx":   "TypeScript",
		".jsx":   "JavaScript",
		".rb":    "Ruby",
		".rs":    "Rust",
		".java":  "Java",
		".c":     "C",
		".cpp":   "C++",
		".h":     "C",
		".cs":    "C#",
		".swift": "Swift",
		".kt":    "Kotlin",
		".sh":    "Shell",
		".lua":   "Lua",
		".php":   "PHP",
	}
	return m[strings.ToLower(ext)]
}

// countLines counts non-empty lines in a string.
func countLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// fileExists returns true if path exists and is a file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

