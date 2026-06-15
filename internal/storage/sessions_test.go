package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCreateAndCloseSession(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	id, err := db.CreateSession("/home/dev/recall", "recall")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty session id")
	}

	var status string
	err = db.SQL().QueryRow("SELECT status FROM sessions WHERE id = ?", id).Scan(&status)
	if err != nil {
		t.Fatalf("query session: %v", err)
	}
	if status != SessionActive {
		t.Fatalf("status = %q, want %q", status, SessionActive)
	}

	if err := db.CloseSession(id); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	err = db.SQL().QueryRow("SELECT status FROM sessions WHERE id = ?", id).Scan(&status)
	if err != nil {
		t.Fatalf("query session: %v", err)
	}
	if status != SessionClosed {
		t.Fatalf("status = %q, want %q", status, SessionClosed)
	}
}

func TestInsertLogLines(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp", "tmp")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	now := time.Now().UnixNano()
	lines := []LogLineRecord{
		{Text: "ok", IsError: false, CreatedAt: now},
		{Text: "panic: boom", IsError: true, CreatedAt: now},
	}
	if err := db.InsertLogLines(sessionID, lines); err != nil {
		t.Fatalf("InsertLogLines: %v", err)
	}

	var errorCount int
	if err := db.SQL().QueryRow(
		"SELECT COUNT(*) FROM log_lines WHERE session_id = ? AND is_error = 1",
		sessionID,
	).Scan(&errorCount); err != nil {
		t.Fatalf("count errors: %v", err)
	}
	if errorCount != 1 {
		t.Fatalf("error lines = %d, want 1", errorCount)
	}
}

func TestOrphanActiveSessions(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "recall.db")

	// 1. Open DB, create a session, and close the DB without closing the session.
	db, err := OpenAt(dbPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	id1, err := db.CreateSession("/tmp/1", "one")
	if err != nil {
		t.Fatal(err)
	}

	// Also create a session and close it properly.
	id2, err := db.CreateSession("/tmp/2", "two")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CloseSession(id2); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// 2. Reopen DB. This should trigger the startup orphan run.
	db2, err := OpenAt(dbPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db2.Close()

	// 3. Verify status of session 1 is now "orphaned".
	var status1 string
	err = db2.SQL().QueryRow("SELECT status FROM sessions WHERE id = ?", id1).Scan(&status1)
	if err != nil {
		t.Fatal(err)
	}
	if status1 != SessionOrphaned {
		t.Errorf("session 1 status = %q, want %q", status1, SessionOrphaned)
	}

	// Verify status of session 2 is still "closed".
	var status2 string
	err = db2.SQL().QueryRow("SELECT status FROM sessions WHERE id = ?", id2).Scan(&status2)
	if err != nil {
		t.Fatal(err)
	}
	if status2 != SessionClosed {
		t.Errorf("session 2 status = %q, want %q", status2, SessionClosed)
	}
}

