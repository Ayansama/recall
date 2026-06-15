package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenInitializesSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recall.db")

	db, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"sessions", "log_lines"} {
		var name string
		err := db.SQL().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("query table %q: %v", table, err)
		}
	}

	var mode string
	if err := db.SQL().QueryRow("PRAGMA journal_mode;").Scan(&mode); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestDefaultPath(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := filepath.Join(home, ".recall", "recall.db")
	if path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}
