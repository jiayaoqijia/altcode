package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/altcode-ai/altcode/internal/auth"
)

func TestLoadConfigReadsUserConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	path := auth.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	data := []byte(`{
  "provider": {
    "openai": {
      "apiKey": "test-openai-key"
    }
  }
}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := loadConfig("", "", "")
	if got := cfg.Provider["openai"].APIKey; got != "test-openai-key" {
		t.Fatalf("expected user config key to load, got %q", got)
	}
}
