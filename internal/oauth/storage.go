package oauth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AuthJSON mirrors Codex's ~/.codex/auth.json schema so altcode can
// interoperate with both its own and Codex's credentials files.
type AuthJSON struct {
	AuthMode    string     `json:"auth_mode,omitempty"` // "Chatgpt" | "ApiKey"
	OpenAIKey   string     `json:"OPENAI_API_KEY,omitempty"`
	Tokens      *TokenData `json:"tokens,omitempty"`
	LastRefresh *time.Time `json:"last_refresh,omitempty"`
}

// TokenData holds the access/refresh/id token set.
type TokenData struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id,omitempty"`
}

// DefaultAuthFile returns ~/.altcode/auth.json (preferred) or falls back
// to ~/.codex/auth.json for interop.
func DefaultAuthFile() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".altcode", "auth.json")
	}
	return ".altcode/auth.json"
}

// Save writes the auth file with 0600 permissions (Unix).
func Save(path string, auth *AuthJSON) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// Load reads an auth.json file.
func Load(path string) (*AuthJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a AuthJSON
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &a, nil
}

// AccessToken returns the current access token from either token set
// or API key storage, whichever is present.
func (a *AuthJSON) AccessToken() string {
	if a == nil {
		return ""
	}
	if a.Tokens != nil && a.Tokens.AccessToken != "" {
		return a.Tokens.AccessToken
	}
	return a.OpenAIKey
}
