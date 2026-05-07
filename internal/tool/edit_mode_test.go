package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/tool"
)

// TestEdit_PreservesFileMode verifies editing a 0600 credential file
// keeps it at 0600 — the old hardcoded 0o644 on WriteFile silently
// downgraded sensitive files. altcode-TUI round-J adversarial finding.
func TestEdit_PreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("old value"), 0o600); err != nil {
		t.Fatal(err)
	}

	et := tool.NewEditTool()
	input, _ := json.Marshal(map[string]any{
		"file_path":   path,
		"old_string":  "old value",
		"new_string":  "new value",
		"replace_all": false,
	})
	result, err := et.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode after edit = %o, want 0600 (clobber regression)", got)
	}
}
