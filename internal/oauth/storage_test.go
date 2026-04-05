package oauth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC().Truncate(time.Second)
	orig := &AuthJSON{
		AuthMode: "Chatgpt",
		Tokens: &TokenData{
			IDToken:      "idt",
			AccessToken:  "at",
			RefreshToken: "rt",
			AccountID:    "acct-1",
		},
		LastRefresh: &now,
	}
	if err := Save(path, orig); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AuthMode != "Chatgpt" || loaded.Tokens.AccessToken != "at" {
		t.Errorf("round trip mismatch: %+v", loaded)
	}
}

func TestSave_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix perms")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := Save(path, &AuthJSON{AuthMode: "Chatgpt"}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %v, want 0600", info.Mode().Perm())
	}
}

func TestAccessToken_PreferTokensOverAPIKey(t *testing.T) {
	a := &AuthJSON{
		OpenAIKey: "sk-old",
		Tokens:    &TokenData{AccessToken: "new-access"},
	}
	if a.AccessToken() != "new-access" {
		t.Errorf("want new-access, got %q", a.AccessToken())
	}
}

func TestAccessToken_FallbackToAPIKey(t *testing.T) {
	a := &AuthJSON{OpenAIKey: "sk-only"}
	if a.AccessToken() != "sk-only" {
		t.Errorf("want sk-only, got %q", a.AccessToken())
	}
}

func TestAccessToken_NilSafe(t *testing.T) {
	var a *AuthJSON
	if a.AccessToken() != "" {
		t.Error("nil should return empty")
	}
}
