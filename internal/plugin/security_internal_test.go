package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSafeJoin_SymlinkEscapeRejected guards the iter-M finding:
// safeJoin's lexical containment check passed on symlinks pointing
// outside the plugin tree. A plugin shipping
// `commands/leak.md -> /etc/passwd` could previously load host
// content into prompts. Now the symlink resolution is part of the
// containment check.
func TestSafeJoin_SymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires POSIX")
	}
	base := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("sensitive"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Place a symlink inside `base` pointing at the outside file.
	link := filepath.Join(base, "evil.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := safeJoin(base, "evil.md")
	if err == nil {
		t.Fatal("expected safeJoin to reject symlink escape")
	}
	if !strings.Contains(err.Error(), "symlink") &&
		!strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v, want mention of symlink/escape", err)
	}
}

// TestSafeJoin_LegitimatePathAccepted confirms the hardening didn't
// break regular plugin layouts.
func TestSafeJoin_LegitimatePathAccepted(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "commands")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "ok.md")
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := safeJoin(base, "commands/ok.md")
	if err != nil {
		t.Fatalf("legitimate path rejected: %v", err)
	}
	if got != file {
		t.Errorf("got %q, want %q", got, file)
	}
}

// TestSafeJoin_NonexistentLexicallySafe verifies paths that don't
// exist yet still validate purely lexically (so `commands/new.md`
// can be written before the filesystem has the entry).
func TestSafeJoin_NonexistentLexicallySafe(t *testing.T) {
	base := t.TempDir()
	got, err := safeJoin(base, "commands/not-yet.md")
	if err != nil {
		t.Errorf("nonexistent path rejected: %v", err)
	}
	if !strings.HasPrefix(got, base) {
		t.Errorf("got %q, expected prefix %q", got, base)
	}
}

// TestDiscoverFromMarketplace_AbsolutePathRejected guards the
// marketplace.go iter-M finding: an untrusted marketplace.json with
// an absolute `source` field could point at arbitrary local paths.
func TestDiscoverFromMarketplace_AbsolutePathRejected(t *testing.T) {
	mp := `{"name":"m","plugins":[{"name":"evil","source":"/etc"}]}`
	dir := t.TempDir()
	// Mimic .claude-plugin layout so repoRoot resolves one level up.
	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(pluginDir, "marketplace.json")
	if err := os.WriteFile(path, []byte(mp), 0o644); err != nil {
		t.Fatal(err)
	}
	plugins, err := DiscoverFromMarketplace(path)
	if err != nil {
		t.Fatalf("DiscoverFromMarketplace: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins (absolute path rejected), got %d", len(plugins))
	}
}

// TestDiscoverFromMarketplace_TraversalRejected ensures relative
// `..` chains can't escape the repo root either.
func TestDiscoverFromMarketplace_TraversalRejected(t *testing.T) {
	mp := `{"name":"m","plugins":[{"name":"evil","source":"../../../../etc"}]}`
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(pluginDir, "marketplace.json")
	if err := os.WriteFile(path, []byte(mp), 0o644); err != nil {
		t.Fatal(err)
	}
	plugins, err := DiscoverFromMarketplace(path)
	if err != nil {
		t.Fatalf("DiscoverFromMarketplace: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins (traversal rejected), got %d", len(plugins))
	}
}

// TestLoadHooks_ArrayFormHookFiles guards the Codex round-N finding:
// Manifest.UnmarshalJSON accepts `"hooks":["./a.json","./b.json"]`
// as an array and populates m.HookFiles, but loadHooks previously
// only consulted m.Hooks (string form) and silently dropped every
// array entry. This regression would be invisible at plugin install
// and only manifest as "my hooks aren't firing".
func TestLoadHooks_ArrayFormHookFiles(t *testing.T) {
	pluginDir := t.TempDir()

	// Two separate hook files to prove both entries are merged.
	hookA := filepath.Join(pluginDir, "a.json")
	hookB := filepath.Join(pluginDir, "b.json")
	if err := os.WriteFile(hookA,
		[]byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo A"}]}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookB,
		[]byte(`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo B"}]}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}

	// Drive through the public Load() entry point rather than calling
	// the private loadHooks directly — that exercises the full
	// manifest decode + load pipeline a real plugin install would hit.
	// Manifests live under .altcode-plugin/plugin.json or
	// .claude-plugin/plugin.json (Load tries both).
	manifestDir := filepath.Join(pluginDir, ".altcode-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginJSON := `{"name":"t","hooks":["a.json","b.json"]}`
	if err := os.WriteFile(filepath.Join(manifestDir, "plugin.json"),
		[]byte(pluginJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(pluginDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(p.Hooks["PreToolUse"]) == 0 {
		t.Errorf("PreToolUse hook from a.json not loaded (array-form regression)")
	}
	if len(p.Hooks["PostToolUse"]) == 0 {
		t.Errorf("PostToolUse hook from b.json not loaded (array-form regression)")
	}
	t.Logf("PreToolUse=%d PostToolUse=%d",
		len(p.Hooks["PreToolUse"]), len(p.Hooks["PostToolUse"]))
}
