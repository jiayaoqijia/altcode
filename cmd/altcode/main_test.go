package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/auth"
	"github.com/jiayaoqijia/altcode/internal/engine"
	"github.com/jiayaoqijia/altcode/internal/exec"
	"github.com/jiayaoqijia/altcode/internal/store"
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

func TestDiscoverSkillsReadsGlobalAgentSkillDirs(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	codexHome := filepath.Join(home, "custom-codex")
	xdgConfig := filepath.Join(home, "xdg-config")

	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	writeTestSkill(t, filepath.Join(home, ".agents", "skills"), "global-agents")
	writeTestSkill(t, filepath.Join(codexHome, "skills"), "global-codex")
	writeTestSkill(t, filepath.Join(xdgConfig, "opencode", "skills"), "global-opencode")

	skills := discoverSkills()
	for _, name := range []string{
		"global-agents",
		"global-codex",
		"global-opencode",
	} {
		if !hasSkill(skills, name) {
			t.Fatalf("discoverSkills missing %q in %#v", name, skillNames(skills))
		}
	}
}

func TestGlobalSkillDirsIncludeVercelSkillsGlobalLocations(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, "codex-home")
	claudeHome := filepath.Join(home, "claude-home")
	vibeHome := filepath.Join(home, "vibe-home")
	xdgConfig := filepath.Join(home, "xdg")

	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("VIBE_HOME", vibeHome)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)

	dirs := globalSkillDirs(home)
	for _, want := range []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(xdgConfig, "agents", "skills"),
		filepath.Join(home, ".config", "agents", "skills"),
		filepath.Join(claudeHome, "skills"),
		filepath.Join(codexHome, "skills"),
		filepath.Join(xdgConfig, "devin", "skills"),
		filepath.Join(xdgConfig, "opencode", "skills"),
		filepath.Join(xdgConfig, "goose", "skills"),
		filepath.Join(home, ".aider-desk", "skills"),
		filepath.Join(home, ".augment", "skills"),
		filepath.Join(home, ".bob", "skills"),
		filepath.Join(home, ".codeartsdoer", "skills"),
		filepath.Join(home, ".codebuddy", "skills"),
		filepath.Join(home, ".codemaker", "skills"),
		filepath.Join(home, ".codestudio", "skills"),
		filepath.Join(home, ".commandcode", "skills"),
		filepath.Join(home, ".continue", "skills"),
		filepath.Join(home, ".snowflake", "cortex", "skills"),
		filepath.Join(home, ".config", "crush", "skills"),
		filepath.Join(home, ".cursor", "skills"),
		filepath.Join(home, ".deepagents", "agent", "skills"),
		filepath.Join(home, ".factory", "skills"),
		filepath.Join(home, ".firebender", "skills"),
		filepath.Join(home, ".forge", "skills"),
		filepath.Join(home, ".gemini", "antigravity", "skills"),
		filepath.Join(home, ".gemini", "skills"),
		filepath.Join(home, ".copilot", "skills"),
		filepath.Join(home, ".hermes", "skills"),
		filepath.Join(home, ".junie", "skills"),
		filepath.Join(home, ".iflow", "skills"),
		filepath.Join(home, ".kilocode", "skills"),
		filepath.Join(home, ".kiro", "skills"),
		filepath.Join(home, ".kode", "skills"),
		filepath.Join(home, ".mcpjam", "skills"),
		filepath.Join(vibeHome, "skills"),
		filepath.Join(home, ".mux", "skills"),
		filepath.Join(home, ".openhands", "skills"),
		filepath.Join(home, ".pi", "agent", "skills"),
		filepath.Join(home, ".qoder", "skills"),
		filepath.Join(home, ".qwen", "skills"),
		filepath.Join(home, ".rovodev", "skills"),
		filepath.Join(home, ".roo", "skills"),
		filepath.Join(home, ".tabnine", "agent", "skills"),
		filepath.Join(home, ".trae", "skills"),
		filepath.Join(home, ".trae-cn", "skills"),
		filepath.Join(home, ".codeium", "windsurf", "skills"),
		filepath.Join(home, ".zencoder", "skills"),
		filepath.Join(home, ".neovate", "skills"),
		filepath.Join(home, ".pochi", "skills"),
		filepath.Join(home, ".adal", "skills"),
		filepath.Join(home, ".moltbot", "skills"),
		filepath.Join(home, ".clawdbot", "skills"),
		filepath.Join(home, ".openclaw", "skills"),
	} {
		if !containsDir(dirs, want) {
			t.Fatalf("globalSkillDirs missing %q in %#v", want, dirs)
		}
	}
	assertNoDuplicateDirs(t, dirs)
}

func TestGlobalSkillDirsPreferCurrentOpenClawDir(t *testing.T) {
	home := t.TempDir()

	dirs := globalSkillDirs(home)
	moltbot := filepath.Join(home, ".moltbot", "skills")
	clawdbot := filepath.Join(home, ".clawdbot", "skills")
	openclaw := filepath.Join(home, ".openclaw", "skills")
	moltbotIdx := indexDir(dirs, moltbot)
	clawdbotIdx := indexDir(dirs, clawdbot)
	openclawIdx := indexDir(dirs, openclaw)
	if moltbotIdx < 0 || clawdbotIdx < 0 || openclawIdx < 0 {
		t.Fatalf("OpenClaw legacy dirs missing from %#v", dirs)
	}
	if !(moltbotIdx < clawdbotIdx && clawdbotIdx < openclawIdx) {
		t.Fatalf("OpenClaw legacy dirs should be old-to-new so current path overrides: %#v", dirs)
	}
}

func TestDiscoverCommandDirsKeepProjectSkillsLast(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()

	dirs := discoverCommandDirs(project, home)
	globalAgents := filepath.Join(home, ".agents", "skills")
	projectAgents := filepath.Join(project, ".agents", "skills")
	if indexDir(dirs, globalAgents) < 0 {
		t.Fatalf("discoverCommandDirs missing global .agents skills dir")
	}
	if indexDir(dirs, projectAgents) < 0 {
		t.Fatalf("discoverCommandDirs missing project .agents skills dir")
	}
	if indexDir(dirs, projectAgents) < indexDir(dirs, globalAgents) {
		t.Fatalf("project .agents skills should be discovered after global dirs so project skills override")
	}
	assertNoDuplicateDirs(t, dirs)
}

func TestDiscoverAgentDirsDoNotTreatGlobalSkillsAsAgents(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	codexHome := filepath.Join(home, "codex-home")

	t.Setenv("CODEX_HOME", codexHome)

	dirs := discoverAgentDirs(project, home)
	for _, globalSkillDir := range []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(codexHome, "skills"),
	} {
		if containsDir(dirs, globalSkillDir) {
			t.Fatalf("discoverAgentDirs should not treat global skill dir %q as an agent dir: %#v", globalSkillDir, dirs)
		}
	}
	assertNoDuplicateDirs(t, dirs)
}

func writeTestSkill(t *testing.T, base, name string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	data := []byte("---\ndescription: test skill\n---\n\nDo the thing.\n")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), data, 0o644); err != nil {
		t.Fatalf("WriteFile skill: %v", err)
	}
}

func hasSkill(skills []engine.Skill, name string) bool {
	for _, s := range skills {
		if s.Name == name {
			return true
		}
	}
	return false
}

func skillNames(skills []engine.Skill) []string {
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	return names
}

func containsDir(dirs []string, want string) bool {
	return indexDir(dirs, want) >= 0
}

func indexDir(dirs []string, want string) int {
	for i, dir := range dirs {
		if dir == want {
			return i
		}
	}
	return -1
}

func assertNoDuplicateDirs(t *testing.T, dirs []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] {
			t.Fatalf("duplicate discovery dir %q in %#v", dir, dirs)
		}
		seen[dir] = true
	}
}

// --- Phase 4 tests --------------------------------------------------

// TestShortID verifies the session ID truncation helper used by
// fork-session diagnostic messages.
func TestShortID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"abcdef1234567890", "abcdef12"},
		{"abc", "abc"}, // shorter than 8 → unchanged
		{"", ""},
		{"exactly8", "exactly8"},
	}
	for _, tc := range cases {
		if got := shortID(tc.in); got != tc.want {
			t.Errorf("shortID(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestForkSession_HappyPath creates a source session with a couple
// of messages, then forks it and verifies the new session has the
// same messages but a distinct ID.
func TestForkSession_HappyPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	src, err := db.CreateSession("proj", "source", "test-model")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.AddMessage(src.ID, "user", []byte(`"hello"`), "test-model", 0, 0)
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}
	_, err = db.AddMessage(src.ID, "assistant", []byte(`"hi there"`), "test-model", 0, 0)
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}

	newID, err := forkSession(db, src.ID, "test-model")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if newID == src.ID {
		t.Fatal("fork produced same ID")
	}

	newMsgs, err := db.ListMessages(newID)
	if err != nil {
		t.Fatalf("list forked msgs: %v", err)
	}
	if len(newMsgs) != 2 {
		t.Errorf("expected 2 messages in fork, got %d", len(newMsgs))
	}

	// Source is untouched
	srcMsgs, _ := db.ListMessages(src.ID)
	if len(srcMsgs) != 2 {
		t.Errorf("source mutated: expected 2 messages, got %d", len(srcMsgs))
	}
}

// TestForkSession_UnknownSource returns a typed UsageError with
// exit code 64, not a random wrapped error.
func TestForkSession_UnknownSource(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	_, err = forkSession(db, "does-not-exist", "test-model")
	if err == nil {
		t.Fatal("expected error")
	}
	var uerr *exec.UsageError
	if !errors.As(err, &uerr) {
		t.Errorf("expected *exec.UsageError, got %T: %v", err, err)
	}
	if uerr.ExitCode != 64 {
		t.Errorf("expected exit 64, got %d", uerr.ExitCode)
	}
	if !strings.Contains(uerr.Msg, "not found") {
		t.Errorf("expected 'not found' in error, got %q", uerr.Msg)
	}
}

// TestForkSession_NilDB returns a UsageError instead of nil-deref.
func TestForkSession_NilDB(t *testing.T) {
	_, err := forkSession(nil, "id", "model")
	if err == nil {
		t.Fatal("expected error on nil db")
	}
	var uerr *exec.UsageError
	if !errors.As(err, &uerr) {
		t.Errorf("expected UsageError, got %T", err)
	}
}

// TestListSessionsFromDB exercises the alternate-path list entry
// point used by `--list-sessions --session-db`.
func TestListSessionsFromDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = db.CreateSession("proj", "one", "test-model")
	db.Close()

	// listSessionsFromDB prints to stdout; we just verify no error.
	if err := listSessionsFromDB(dbPath); err != nil {
		t.Errorf("listSessionsFromDB: %v", err)
	}
}

// TestForkSession_TransactionAtomicity verifies that the fork
// operation writes no half-forked state if it fails partway through.
// Hard to inject a mid-transaction failure without mocking SQL, so
// the test instead verifies that ForkSession emits a message count
// that matches what the store reports — if messages were being
// double-inserted or the tx was committing partial state, the count
// would drift.
func TestForkSession_TransactionAtomicity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	src, _ := db.CreateSession("proj", "source", "test-model")
	for i := 0; i < 50; i++ {
		_, _ = db.AddMessage(src.ID, "user", []byte(`"m"`), "test-model", 0, 0)
	}

	newID, err := forkSession(db, src.ID, "test-model")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	msgs, _ := db.ListMessages(newID)
	if len(msgs) != 50 {
		t.Errorf("expected 50 messages in fork, got %d", len(msgs))
	}
	// Source still has exactly 50 — no duplicates, no missing
	srcMsgs, _ := db.ListMessages(src.ID)
	if len(srcMsgs) != 50 {
		t.Errorf("source count drifted: got %d, want 50", len(srcMsgs))
	}
}
