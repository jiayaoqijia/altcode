package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildProfile(t *testing.T) {
	dir := t.TempDir()

	// Create a mini Go repo.
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/test\n\ngo 1.21\n")
	writeTestFile(t, filepath.Join(dir, "main.go"),
		"package main\n\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(dir, "main_test.go"),
		"package main\n\nimport \"testing\"\n\n"+
			"func TestMain(t *testing.T) {}\n")
	os.MkdirAll(
		filepath.Join(dir, ".github", "workflows"), 0o755,
	)
	writeTestFile(t,
		filepath.Join(dir, ".github", "workflows", "ci.yml"),
		"name: CI\n")

	ri := NewRepoIntel(nil)
	p, err := ri.BuildProfile(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildProfile: %v", err)
	}

	if p.TotalFiles < 2 {
		t.Errorf("TotalFiles = %d, want >= 2", p.TotalFiles)
	}
	if p.TotalLOC < 3 {
		t.Errorf("TotalLOC = %d, want >= 3", p.TotalLOC)
	}
	if len(p.Languages) == 0 {
		t.Error("Languages is empty, want at least Go")
	}
	if p.Languages[0] != "Go" {
		t.Errorf("Languages[0] = %q, want Go", p.Languages[0])
	}
	if !p.HasTests {
		t.Error("HasTests = false, want true")
	}
	if !p.HasCI {
		t.Error("HasCI = false, want true")
	}
}

func TestDetectLanguages(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.go"), "package a\n")
	writeTestFile(t, filepath.Join(dir, "b.go"), "package b\n")
	writeTestFile(t, filepath.Join(dir, "c.py"), "print(1)\n")
	writeTestFile(t, filepath.Join(dir, "d.js"), "var x=1;\n")

	ri := NewRepoIntel(nil)
	langs, err := ri.DetectLanguages(
		context.Background(), dir,
	)
	if err != nil {
		t.Fatalf("DetectLanguages: %v", err)
	}
	if len(langs) < 3 {
		t.Fatalf("got %d languages, want >= 3", len(langs))
	}
	// Go should be first (2 files vs 1 each).
	if langs[0] != "Go" {
		t.Errorf("first language = %q, want Go", langs[0])
	}
}

func TestDetectTestFramework_Go(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/test\n")

	ri := NewRepoIntel(nil)
	got := ri.DetectTestFramework(context.Background(), dir)
	if got != "go test ./..." {
		t.Errorf(
			"DetectTestFramework = %q, want %q",
			got, "go test ./...",
		)
	}
}

func TestDetectTestFramework_Node(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "package.json"),
		`{"devDependencies":{"jest":"^29.0.0"},`+
			`"scripts":{"test":"jest"}}`)

	ri := NewRepoIntel(nil)
	got := ri.DetectTestFramework(context.Background(), dir)
	if got != "npm test" {
		t.Errorf(
			"DetectTestFramework = %q, want %q",
			got, "npm test",
		)
	}
}

func TestDetectLintCommand(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "go.mod"),
			"module example.com/test\n")

		ri := NewRepoIntel(nil)
		got := ri.DetectLintCommand(
			context.Background(), dir,
		)
		if got != "go vet ./..." {
			t.Errorf(
				"DetectLintCommand = %q, want %q",
				got, "go vet ./...",
			)
		}
	})

	t.Run("node_eslint", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "package.json"),
			`{"devDependencies":{"eslint":"^8.0.0"}}`)

		ri := NewRepoIntel(nil)
		got := ri.DetectLintCommand(
			context.Background(), dir,
		)
		if got != "npx eslint ." {
			t.Errorf(
				"DetectLintCommand = %q, want %q",
				got, "npx eslint .",
			)
		}
	})
}

func TestIsMonorepo(t *testing.T) {
	t.Run("multiple_go_mod", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "go.mod"),
			"module root\n")
		sub := filepath.Join(dir, "pkg", "sub")
		os.MkdirAll(sub, 0o755)
		writeTestFile(t, filepath.Join(sub, "go.mod"),
			"module root/pkg/sub\n")

		ri := NewRepoIntel(nil)
		if !ri.IsMonorepo(context.Background(), dir) {
			t.Error("IsMonorepo = false, want true")
		}
	})

	t.Run("single_go_mod", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "go.mod"),
			"module root\n")

		ri := NewRepoIntel(nil)
		if ri.IsMonorepo(context.Background(), dir) {
			t.Error("IsMonorepo = true, want false")
		}
	})

	t.Run("npm_workspaces", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "package.json"),
			`{"workspaces":["packages/*"]}`)

		ri := NewRepoIntel(nil)
		if !ri.IsMonorepo(context.Background(), dir) {
			t.Error("IsMonorepo = false, want true (workspaces)")
		}
	})

	t.Run("bazel", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "WORKSPACE"), "")

		ri := NewRepoIntel(nil)
		if !ri.IsMonorepo(context.Background(), dir) {
			t.Error("IsMonorepo = false, want true (bazel)")
		}
	})
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(
		path, []byte(content), 0o644,
	); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
