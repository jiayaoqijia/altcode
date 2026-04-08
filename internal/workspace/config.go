package workspace

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WorkspaceConfig holds workspace-level settings loaded from
// .altcode/config.yaml. All fields have sensible defaults.
type WorkspaceConfig struct {
	MaxCIRetries int    `yaml:"max_ci_retries"`
	AutoMerge    bool   `yaml:"auto_merge"`
	MergeMethod  string `yaml:"merge_method"`
	PollInterval int    `yaml:"poll_interval_seconds"`
}

// DefaultConfig returns production defaults.
func DefaultConfig() *WorkspaceConfig {
	return &WorkspaceConfig{
		MaxCIRetries: 3,
		AutoMerge:    false,
		MergeMethod:  "squash",
		PollInterval: 10,
	}
}

// LoadConfig reads .altcode/config.yaml under gitRoot.
// Returns defaults if the file is missing or unreadable.
func LoadConfig(gitRoot string) *WorkspaceConfig {
	cfg := DefaultConfig()
	p := filepath.Join(gitRoot, ".altcode", "config.yaml")
	data, err := os.ReadFile(p)
	if err != nil {
		return cfg
	}
	_ = yaml.Unmarshal(data, cfg)
	return cfg
}
