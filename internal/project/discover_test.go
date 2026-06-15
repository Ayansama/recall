package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFindsGitRoot(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("[core]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	path, name := Discover(nested)
	if path != root {
		t.Errorf("projectPath = %q, want %q", path, root)
	}
	if name != filepath.Base(root) {
		t.Errorf("projectName = %q, want %q", name, filepath.Base(root))
	}
}

func TestDiscoverFallbackWithoutGit(t *testing.T) {
	dir := t.TempDir()
	path, name := Discover(dir)
	if path != dir {
		t.Errorf("projectPath = %q, want %q", path, dir)
	}
	if name != filepath.Base(dir) {
		t.Errorf("projectName = %q, want %q", name, filepath.Base(dir))
	}
}
