package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MarketplaceEntry describes a plugin in a marketplace index.
type MarketplaceEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version,omitempty"`
	Source      string `json:"source"`
	Category    string `json:"category,omitempty"`
}

// Marketplace is a local plugin marketplace index.
type Marketplace struct {
	Name        string             `json:"name"`
	Version     string             `json:"version,omitempty"`
	Description string             `json:"description,omitempty"`
	Plugins     []MarketplaceEntry `json:"plugins"`
}

// LoadMarketplace reads a marketplace.json file.
func LoadMarketplace(path string) (*Marketplace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Marketplace
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// DiscoverFromMarketplace loads plugins listed in a marketplace file.
// marketplace.json is untrusted: `entry.Source` comes from a file
// a repo can ship, so candidate paths are constrained to the repo
// tree. Absolute paths and relatives that escape via `..` or a
// symlink are rejected — otherwise a malicious marketplace entry
// could point at `/etc/passwd` or `~/.ssh/config`.
func DiscoverFromMarketplace(marketplacePath string) ([]*Plugin, error) {
	mp, err := LoadMarketplace(marketplacePath)
	if err != nil {
		return nil, err
	}

	// Resolve the containment root once. Everything plugins load
	// must stay inside (or equal to) repoRoot after symlink resolution.
	baseDir := filepath.Dir(marketplacePath)
	repoRoot := filepath.Dir(baseDir) // marketplace is in .claude-plugin/
	repoRootResolved, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		repoRootResolved = repoRoot
	}

	var plugins []*Plugin
	for _, entry := range mp.Plugins {
		candidate := entry.Source
		if filepath.IsAbs(candidate) {
			// Absolute paths in untrusted marketplace.json are refused
			// outright — there's no legitimate use case.
			continue
		}
		// Try relative to marketplace file first, then to repo root.
		probe := filepath.Join(baseDir, candidate)
		if _, err := os.Stat(probe); os.IsNotExist(err) {
			probe = filepath.Join(repoRoot, candidate)
		}
		if !withinRoot(probe, repoRootResolved) {
			continue
		}
		p, err := Load(probe)
		if err != nil {
			continue
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

// withinRoot reports whether path (after lexical cleaning AND symlink
// resolution) is equal to or a descendant of root. Both inputs must
// refer to existing filesystem paths; non-existent entries are
// rejected (safer default than "assume it's inside").
func withinRoot(path, root string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	// filepath.Rel returns a string containing ".." when the target is
	// outside the root — use that as the containment check.
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return false
	}
	if rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return false
	}
	return true
}
