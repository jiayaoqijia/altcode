package memory_test

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/memory"
)

// TestMemory_ConcurrentSavesDontDropIndex guards Codex round-P's
// cross-process race finding. Two parallel Save calls through
// separate Store instances on the same directory used to be able
// to last-writer-win MEMORY.md; the new file lock serialises the
// index rewrites so both entries are persisted.
//
// This test uses two separate Store instances to simulate two
// altcode sessions — a single in-process mutex would hide the bug.
func TestMemory_ConcurrentSavesDontDropIndex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("memory lock test relies on POSIX-style OpenFile semantics")
	}
	dir := t.TempDir()

	// Two independent Stores backed by the same filesystem dir.
	s1 := memory.NewStore(dir)
	s2 := memory.NewStore(dir)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = s1.Save("alpha", "Alpha memo", "alpha content")
	}()
	go func() {
		defer wg.Done()
		_ = s2.Save("beta", "Beta memo", "beta content")
	}()
	wg.Wait()

	// Both memory files must exist on disk.
	listed, err := s1.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) < 2 {
		t.Errorf("expected both memories persisted, got %d: %+v",
			len(listed), listed)
	}
	// Verify the MEMORY.md index contains both (the was the bug —
	// one entry could vanish from the index even though the .md
	// file was on disk).
	s3 := memory.NewStore(dir)
	got, err := s3.LoadIndex()
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	_ = filepath.Base("") // keep import
	for _, id := range []string{"alpha", "beta"} {
		if !containsID(got, id) {
			t.Errorf("MEMORY.md index missing %q; full index:\n%s", id, got)
		}
	}
}

func containsID(index, id string) bool {
	return index != "" && (indexHas(index, id))
}

// indexHas does a substring match in the index content.
func indexHas(s, id string) bool {
	return len(s) >= len(id) && contains(s, id)
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
