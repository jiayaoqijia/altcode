// Package memory provides persistent cross-session memory storage.
// Memories are markdown files stored in a configurable directory
// with MEMORY.md as the index. Compatible with Claude Code's
// auto-memory format.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Memory represents a single memory entry.
type Memory struct {
	ID        string    // filename without .md
	Title     string    // from YAML frontmatter "name"
	Content   string    // full file content
	Summary   string    // one-line description from frontmatter
	Tags      []string  // from frontmatter "tags"
	CreatedAt time.Time // file modification time
	Path      string    // absolute file path
}

// Store manages persistent memories on disk. The mutex serializes
// writes from concurrent callers (multiple sessions or background
// goroutines saving memories at once) so MEMORY.md and individual
// entries don't get corrupted by interleaved writes.
type Store struct {
	dir string // directory containing memory files
	mu  sync.Mutex
}

// NewStore creates a Store at the given directory.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// DefaultDir returns the default memory directory for the project.
func DefaultDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".altcode", "memory")
}

// ClaudeCodeDir returns Claude Code's memory directory.
func ClaudeCodeDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".claude", "memory")
}

// validateMemoryID rejects IDs that contain path separators or other
// characters that could escape the memory directory via filepath.Join.
// Previously an id like "../../etc/passwd" would let a caller read or
// write arbitrary files outside the memory store.
func validateMemoryID(id string) error {
	if id == "" {
		return fmt.Errorf("memory id is empty")
	}
	if strings.ContainsAny(id, `/\`+string(filepath.Separator)) {
		return fmt.Errorf("memory id %q contains path separator", id)
	}
	if id == "." || id == ".." || strings.HasPrefix(id, ".") {
		return fmt.Errorf("memory id %q is not allowed", id)
	}
	for _, r := range id {
		if r == ':' || r == 0 || r == '\n' {
			return fmt.Errorf("memory id %q contains invalid character", id)
		}
	}
	return nil
}

// Save writes a memory to disk and updates the index. The write is
// guarded by the store mutex and uses an atomic temp-file + rename
// so partial writes from a crash never leave a half-written file.
func (s *Store) Save(id, title, content string) error {
	if err := validateMemoryID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	path := filepath.Join(s.dir, id+".md")
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\ncreated: %s\n---\n\n%s\n",
		title, firstLine(content), time.Now().Format(time.RFC3339), content)

	if err := writeFileAtomic(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}

	return s.updateIndexLocked()
}

// Load reads a single memory by ID.
func (s *Store) Load(id string) (*Memory, error) {
	if err := validateMemoryID(id); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, id+".md")
	return parseMemoryFile(path)
}

// List returns all memories sorted by modification time (newest first).
func (s *Store) List() ([]*Memory, error) {
	if _, err := os.Stat(s.dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	var memories []*Memory
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() == "MEMORY.md" {
			continue
		}
		m, err := parseMemoryFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		memories = append(memories, m)
	}

	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt.After(memories[j].CreatedAt)
	})
	return memories, nil
}

// Delete removes a memory and updates the index.
func (s *Store) Delete(id string) error {
	if err := validateMemoryID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, id+".md")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	return s.updateIndexLocked()
}

// Search returns memories containing the query string.
func (s *Store) Search(query string) ([]*Memory, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	var results []*Memory
	for _, m := range all {
		if strings.Contains(strings.ToLower(m.Content), query) ||
			strings.Contains(strings.ToLower(m.Title), query) {
			results = append(results, m)
		}
	}
	return results, nil
}

// ForContext returns a formatted string of all memories for injection
// into the system prompt. Truncated to maxBytes.
func (s *Store) ForContext(maxBytes int) string {
	memories, err := s.List()
	if err != nil || len(memories) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Memories\n\n")

	totalBytes := sb.Len()
	for _, m := range memories {
		entry := fmt.Sprintf("## %s\n%s\n\n", m.Title, m.Content)
		if totalBytes+len(entry) > maxBytes {
			sb.WriteString("...(truncated)\n")
			break
		}
		sb.WriteString(entry)
		totalBytes += len(entry)
	}
	return sb.String()
}

// LoadIndex reads the MEMORY.md index file.
func (s *Store) LoadIndex() (string, error) {
	path := filepath.Join(s.dir, "MEMORY.md")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// updateIndexLocked rewrites MEMORY.md. The caller must hold s.mu.
// Renamed from updateIndex so the locking discipline is visible at
// every call site (Save and Delete both already hold the lock).
func (s *Store) updateIndexLocked() error {
	memories, err := s.List()
	if err != nil {
		return err
	}

	var sb strings.Builder
	for _, m := range memories {
		summary := m.Summary
		if summary == "" {
			summary = firstLine(m.Content)
		}
		if len(summary) > 120 {
			summary = summary[:120] + "..."
		}
		sb.WriteString(fmt.Sprintf("- [%s](%s.md) — %s\n", m.Title, m.ID, summary))
	}

	// Truncate at 200 lines (matching Claude Code's limit)
	lines := strings.Split(sb.String(), "\n")
	if len(lines) > 200 {
		lines = lines[:200]
	}

	path := filepath.Join(s.dir, "MEMORY.md")
	return writeFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// writeFileAtomic writes data to path via a temp file in the same
// directory and an os.Rename. This survives a crash mid-write — either
// the old file remains intact or the new file is fully committed.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func parseMemoryFile(path string) (*Memory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSuffix(filepath.Base(path), ".md")
	content := string(data)

	m := &Memory{
		ID:        name,
		Title:     name,
		Content:   content,
		CreatedAt: info.ModTime(),
		Path:      path,
	}

	// Parse frontmatter if present
	if strings.HasPrefix(content, "---") {
		rest := content[3:]
		idx := strings.Index(rest, "\n---")
		if idx >= 0 {
			fm := rest[:idx]
			m.Content = strings.TrimSpace(rest[idx+4:])
			parseFM(fm, m)
		}
	}

	return m, nil
}

func parseFM(fm string, m *Memory) {
	for _, line := range strings.Split(fm, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch key {
		case "name":
			m.Title = val
		case "description":
			m.Summary = val
		case "tags":
			for _, t := range strings.Split(val, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					m.Tags = append(m.Tags, t)
				}
			}
		}
	}
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}
