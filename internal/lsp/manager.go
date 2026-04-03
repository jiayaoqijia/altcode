package lsp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// ServerConfig describes how to launch a language server.
type ServerConfig struct {
	Language string
	Command  string
	Args     []string
}

// DefaultConfigs returns the built-in server configurations.
func DefaultConfigs() []ServerConfig {
	return []ServerConfig{
		{Language: "go", Command: "gopls", Args: nil},
		{
			Language: "typescript",
			Command:  "typescript-language-server",
			Args:     []string{"--stdio"},
		},
		{
			Language: "python",
			Command:  "pyright-langserver",
			Args:     []string{"--stdio"},
		},
	}
}

// Manager manages multiple LSP clients keyed by language.
type Manager struct {
	configs  map[string]ServerConfig
	servers  map[string]*Client
	mu       sync.Mutex
	rootPath string
}

// NewManager creates a Manager pre-loaded with default configs.
func NewManager(rootPath string) *Manager {
	m := &Manager{
		configs:  make(map[string]ServerConfig),
		servers:  make(map[string]*Client),
		rootPath: rootPath,
	}
	for _, cfg := range DefaultConfigs() {
		m.configs[cfg.Language] = cfg
	}
	return m
}

// GetOrStart returns an existing client for the language, or starts one.
func (m *Manager) GetOrStart(ctx context.Context, language string) (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c, ok := m.servers[language]; ok {
		return c, nil
	}

	cfg, ok := m.configs[language]
	if !ok {
		return nil, fmt.Errorf("no server config for %q", language)
	}

	c, err := Connect(ctx, cfg.Command, cfg.Args)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", language, err)
	}

	if err := c.Initialize(ctx, m.rootPath); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initialize %s: %w", language, err)
	}

	m.servers[language] = c
	return c, nil
}

// DiagnosticsForFile returns diagnostics from the appropriate server.
func (m *Manager) DiagnosticsForFile(uri string) []Diagnostic {
	lang := LanguageForURI(uri)
	m.mu.Lock()
	c, ok := m.servers[lang]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return c.GetDiagnostics(uri)
}

// Close shuts down all managed servers.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for lang, c := range m.servers {
		_ = c.Close()
		delete(m.servers, lang)
	}
}

// LanguageForURI returns a language identifier from a file URI.
func LanguageForURI(uri string) string {
	path := strings.TrimPrefix(uri, "file://")
	ext := strings.ToLower(filepath.Ext(path))
	return languageForExt(ext)
}

// languageForExt maps file extensions to language server keys.
func languageForExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript"
	case ".py":
		return "python"
	default:
		return ""
	}
}
