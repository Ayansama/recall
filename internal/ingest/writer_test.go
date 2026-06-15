package ingest

import (
	"path/filepath"
	"testing"
	"time"

	"recall/internal/storage"
)

func TestBatchedWriterFlushesOnBatchSize(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp/proj", "proj")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	writer, ch := NewBatchedWriter(db, sessionID)

	for i := 0; i < batchSize; i++ {
		ch <- Line{Text: "line", CreatedAt: time.Now().UnixNano()}
	}

	// Allow the flush goroutine to commit the full batch.
	time.Sleep(50 * time.Millisecond)
	writer.Close()

	var count int
	if err := db.SQL().QueryRow(
		"SELECT COUNT(*) FROM log_lines WHERE session_id = ?",
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count lines: %v", err)
	}
	if count != batchSize {
		t.Fatalf("stored %d lines, want %d", count, batchSize)
	}
}

func TestBatchedWriterFlushesOnClose(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp/proj", "proj")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	writer, ch := NewBatchedWriter(db, sessionID)
	ch <- Line{Text: "tail line", CreatedAt: time.Now().UnixNano()}
	writer.Close()

	var count int
	if err := db.SQL().QueryRow(
		"SELECT COUNT(*) FROM log_lines WHERE session_id = ?",
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count lines: %v", err)
	}
	if count != 1 {
		t.Fatalf("stored %d lines, want 1", count)
	}
}
