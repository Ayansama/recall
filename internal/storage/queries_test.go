package storage

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	for _, name := range []string{"alpha", "beta"} {
		if _, err := db.CreateSession("/tmp/"+name, name); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := db.ListSessions(1)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

func TestListSessionsDefaultLimit(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessions, err := db.ListSessions(0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions == nil {
		t.Fatal("expected non-nil empty slice")
	}
}

func TestGetLatestErrorWindow(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp", "tmp")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UnixNano()
	if err := db.InsertLogLines(sessionID, []LogLineRecord{
		{Text: "before", CreatedAt: now},
		{Text: "Error: boom", IsError: true, CreatedAt: now + 1},
		{Text: "after", CreatedAt: now + 2},
	}); err != nil {
		t.Fatal(err)
	}

	lines, actualSessID, err := db.GetLatestErrorWindow(sessionID)
	if err != nil {
		t.Fatalf("GetLatestErrorWindow: %v", err)
	}
	if actualSessID != sessionID {
		t.Errorf("actualSessID = %q, want %q", actualSessID, sessionID)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
}

func TestGetSessionContextLines(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp/c", "gamma")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Test empty session
	head, tail, count, err := db.GetSessionContextLines(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || len(head) != 0 || len(tail) != 0 {
		t.Errorf("expected empty context, got count=%d, head=%d, tail=%d", count, len(head), len(tail))
	}

	// 2. Test small session (<= 175 lines, e.g. 5 lines)
	var records []LogLineRecord
	now := time.Now().UnixNano()
	for i := 1; i <= 5; i++ {
		records = append(records, LogLineRecord{Text: "line " + strconv.Itoa(i), CreatedAt: now + int64(i)})
	}
	if err := db.InsertLogLines(sessionID, records); err != nil {
		t.Fatal(err)
	}

	head, tail, count, err = db.GetSessionContextLines(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
	if len(head) != 5 {
		t.Errorf("len(head) = %d, want 5", len(head))
	}
	if len(tail) != 0 {
		t.Errorf("len(tail) = %d, want 0", len(tail))
	}

	// 3. Test large session (> 175 lines, e.g. 200 lines)
	sessID2, err := db.CreateSession("/tmp/d", "delta")
	if err != nil {
		t.Fatal(err)
	}

	var largeRecords []LogLineRecord
	for i := 1; i <= 200; i++ {
		largeRecords = append(largeRecords, LogLineRecord{Text: "line " + strconv.Itoa(i), CreatedAt: now + int64(i)})
	}
	if err := db.InsertLogLines(sessID2, largeRecords); err != nil {
		t.Fatal(err)
	}

	head, tail, count, err = db.GetSessionContextLines(sessID2)
	if err != nil {
		t.Fatal(err)
	}
	if count != 200 {
		t.Errorf("count = %d, want 200", count)
	}
	if len(head) != 25 {
		t.Errorf("len(head) = %d, want 25", len(head))
	}
	if len(tail) != 150 {
		t.Errorf("len(tail) = %d, want 150", len(tail))
	}
	if head[0] != "line 1" || head[24] != "line 25" {
		t.Errorf("unexpected head context: first=%q, last=%q", head[0], head[24])
	}
	if tail[0] != "line 51" || tail[149] != "line 200" {
		t.Errorf("unexpected tail context: first=%q, last=%q", tail[0], tail[149])
	}
}

func TestListSessionsConcurrentWithWrites(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp/proj", "proj")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		now := time.Now().UnixNano()
		for i := 0; i < 200; i++ {
			_ = db.InsertLogLines(sessionID, []LogLineRecord{
				{Text: "log line", CreatedAt: now + int64(i)},
			})
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := db.ListSessions(10); err != nil {
					t.Errorf("ListSessions during writes: %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	<-done
}

func TestPurgeAll(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenAt(filepath.Join(dir, "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp", "proj")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertLogLines(sessionID, []LogLineRecord{
		{Text: "line 1", CreatedAt: time.Now().UnixNano()},
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.PurgeAll(); err != nil {
		t.Fatalf("PurgeAll failed: %v", err)
	}

	var sessionCount int
	if err := db.SQL().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 {
		t.Errorf("sessions count = %d, want 0", sessionCount)
	}

	var lineCount int
	if err := db.SQL().QueryRow("SELECT COUNT(*) FROM log_lines").Scan(&lineCount); err != nil {
		t.Fatal(err)
	}
	if lineCount != 0 {
		t.Errorf("log_lines count = %d, want 0", lineCount)
	}
}

