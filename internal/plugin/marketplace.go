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
func DiscoverFromMarketplace(marketplacePath string) ([]*Plugin, error) {
	mp, err := LoadMarketplace(marketplacePath)
	if err != nil {
		return nil, err
	}

	// Try relative to marketplace file first, then relative to repo root
	baseDir := filepath.Dir(marketplacePath)
	repoRoot := filepath.Dir(baseDir) // marketplace is in .claude-plugin/
	var plugins []*Plugin
	for _, entry := range mp.Plugins {
		source := entry.Source
		if !filepath.IsAbs(source) {
			// Try relative to marketplace file first
			candidate := filepath.Join(baseDir, source)
			if _, err := os.Stat(candidate); os.IsNotExist(err) {
				// Fall back to relative to repo root
				candidate = filepath.Join(repoRoot, source)
			}
			source = candidate
		}
		p, err := Load(source)
		if err != nil {
			continue
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}
