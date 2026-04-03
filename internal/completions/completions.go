// Package completions provides fuzzy file/folder autocomplete for the
// TUI @ mention system.
package completions

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Match represents a single completion result.
type Match struct {
	Path  string
	IsDir bool
	Score float64
}

// skipDirs contains directory names to always skip during traversal.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".hg":          true,
	".svn":         true,
}

// walkLimit caps the number of entries examined to prevent TUI freezes
// on large repositories. Once we've seen this many entries, we stop walking.
const walkLimit = 10000

// Complete walks root and returns up to maxResults fuzzy matches for query.
// Results are sorted by score (higher = better match). Walking stops early
// once walkLimit entries have been examined to stay responsive in large repos.
func Complete(root, query string, maxResults int) []Match {
	if maxResults <= 0 {
		maxResults = 20
	}

	query = strings.ToLower(query)
	var matches []Match
	seen := 0

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		name := d.Name()

		// Skip hidden dirs and known noisy dirs at entry.
		if d.IsDir() && (skipDirs[name] || isHidden(name)) {
			return filepath.SkipDir
		}

		// Skip the root itself.
		if path == root {
			return nil
		}

		// Skip hidden files.
		if isHidden(name) {
			return nil
		}

		seen++
		if seen > walkLimit {
			return filepath.SkipAll
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		if query == "" {
			matches = append(matches, Match{
				Path:  rel,
				IsDir: d.IsDir(),
				Score: 0,
			})
			if len(matches) >= maxResults {
				return filepath.SkipAll
			}
			return nil
		}

		score := score(rel, query)
		if score > 0 {
			matches = append(matches, Match{
				Path:  rel,
				IsDir: d.IsDir(),
				Score: score,
			})
		}
		return nil
	})

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Path < matches[j].Path
	})

	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}
	return matches
}

// score returns a relevance score for path against query.
// Returns 0 if there is no match.
//
//	3.0 = exact base name match
//	2.0 = base name prefix match
//	1.0 = substring match in full path
//	0.5 = fuzzy subsequence match
func score(path, query string) float64 {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(path))

	if base == query {
		return 3.0
	}
	if strings.HasPrefix(base, query) {
		return 2.0
	}
	if strings.Contains(lower, query) {
		return 1.0
	}
	if fuzzyMatch(lower, query) {
		return 0.5
	}
	return 0
}

// fuzzyMatch returns true if all characters of query appear in s
// in order (case-insensitive subsequence match).
func fuzzyMatch(s, query string) bool {
	qi := 0
	for i := 0; i < len(s) && qi < len(query); i++ {
		if s[i] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

// isHidden returns true for dot-prefixed names (Unix hidden files).
func isHidden(name string) bool {
	return len(name) > 1 && name[0] == '.'
}
