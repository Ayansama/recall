package mcp

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"recall/internal/storage"
)

func testDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.OpenAt(filepath.Join(t.TempDir(), "recall.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return db
}

func TestParseListSessionsLimit(t *testing.T) {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"limit": 25}

	if got := ParseListSessionsLimit(req); got != 25 {
		t.Fatalf("limit = %d, want 25", got)
	}
}

func TestParseListSessionsLimitDefault(t *testing.T) {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	if got := ParseListSessionsLimit(req); got != defaultSessionLimit {
		t.Fatalf("limit = %d, want default %d", got, defaultSessionLimit)
	}
}

func TestParseGetSessionContextParams(t *testing.T) {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"session_id":   "abc-123",
		"token_budget": 8000,
	}

	sessionID, budget := ParseGetSessionContextParams(req)
	if sessionID != "abc-123" {
		t.Fatalf("session_id = %q, want abc-123", sessionID)
	}
	if budget != 8000 {
		t.Fatalf("token_budget = %d, want 8000", budget)
	}
}

func TestParseGetLatestErrorSessionID(t *testing.T) {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = map[string]any{"session_id": "sess-1"}

	if got := ParseGetLatestErrorSessionID(req); got != "sess-1" {
		t.Fatalf("session_id = %q, want sess-1", got)
	}
}

func TestHandleListSessionsReturnsJSON(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	if _, err := db.CreateSession("/tmp/a", "alpha"); err != nil {
		t.Fatal(err)
	}

	result, err := handleListSessions(db, listSessionsArgs{Limit: 5})
	if err != nil {
		t.Fatalf("handleListSessions: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := result.Content[0].(mcpsdk.TextContent).Text
	var sessions []map[string]any
	if err := json.Unmarshal([]byte(text), &sessions); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

func TestHandleGetSessionContextRequiresSessionID(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	result, err := handleGetSessionContext(db, getSessionContextArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected error when session_id is missing")
	}
}

func TestHandleGetLatestErrorReturnsWindow(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp/b", "beta")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UnixNano()
	records := []storage.LogLineRecord{
		{Text: "starting build", CreatedAt: now},
		{Text: "compiling", CreatedAt: now + 1},
		{Text: "panic: runtime error", IsError: true, CreatedAt: now + 2},
		{Text: "goroutine 1", CreatedAt: now + 3},
	}
	if err := db.InsertLogLines(sessionID, records); err != nil {
		t.Fatal(err)
	}

	result, err := handleGetLatestError(db, getLatestErrorArgs{SessionID: sessionID})
	if err != nil {
		t.Fatalf("handleGetLatestError: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %v", result.Content)
	}

	text := result.Content[0].(mcpsdk.TextContent).Text

	// Check the wrapper block
	expectedHeader := "--- START RECALL LOG BLOCK [SESSION: " + sessionID + "] ---"
	expectedFooter := "--- END RECALL LOG BLOCK ---"
	if !strings.HasPrefix(text, expectedHeader) {
		t.Errorf("expected header %q, got %q", expectedHeader, text)
	}
	if !strings.HasSuffix(strings.TrimSpace(text), expectedFooter) {
		t.Errorf("expected footer %q, got %q", expectedFooter, text)
	}

	// Extract content between headers
	startIdx := len(expectedHeader) + 1
	endIdx := strings.LastIndex(text, expectedFooter) - 1
	if startIdx < 0 || endIdx < 0 || startIdx > endIdx {
		t.Fatalf("invalid wrapped layout")
	}
	extracted := text[startIdx:endIdx]

	var lines []string
	if err := json.Unmarshal([]byte(extracted), &lines); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	if lines[2] != "panic: runtime error" {
		t.Fatalf("error line = %q", lines[2])
	}
}

func TestHandleGetSessionContext(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	sessionID, err := db.CreateSession("/tmp/c", "gamma")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Small session, within budget (no truncation)
	var records []storage.LogLineRecord
	now := time.Now().UnixNano()
	for i := 1; i <= 5; i++ {
		records = append(records, storage.LogLineRecord{Text: "line " + strconv.Itoa(i), CreatedAt: now + int64(i)})
	}
	if err := db.InsertLogLines(sessionID, records); err != nil {
		t.Fatal(err)
	}

	result, err := handleGetSessionContext(db, getSessionContextArgs{SessionID: sessionID, TokenBudget: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	text := result.Content[0].(mcpsdk.TextContent).Text
	expected := "--- START RECALL LOG BLOCK [SESSION: " + sessionID + "] ---\n" +
		"line 1\nline 2\nline 3\nline 4\nline 5\n" +
		"--- END RECALL LOG BLOCK ---"
	if text != expected {
		t.Errorf("got:\n%s\nwant:\n%s", text, expected)
	}

	// 2. Large session, within budget (with truncation)
	sessID2, err := db.CreateSession("/tmp/d", "delta")
	if err != nil {
		t.Fatal(err)
	}

	var largeRecords []storage.LogLineRecord
	for i := 1; i <= 200; i++ {
		largeRecords = append(largeRecords, storage.LogLineRecord{Text: "line " + strconv.Itoa(i), CreatedAt: now + int64(i)})
	}
	if err := db.InsertLogLines(sessID2, largeRecords); err != nil {
		t.Fatal(err)
	}

	result, err = handleGetSessionContext(db, getSessionContextArgs{SessionID: sessID2, TokenBudget: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	text = result.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, "line 1") || !strings.Contains(text, "line 25") {
		t.Errorf("missing head context")
	}
	if !strings.Contains(text, "line 51") || !strings.Contains(text, "line 200") {
		t.Errorf("missing tail context")
	}
	delimiter := "\n\n[... TEXT TRUNCATED BY RECALL ENGINE ...]\n\n"
	if !strings.Contains(text, delimiter) {
		t.Errorf("missing delimiter")
	}

	// 3. Small budget, forces shrinking
	result, err = handleGetSessionContext(db, getSessionContextArgs{SessionID: sessID2, TokenBudget: 20})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Content)
	}

	text = result.Content[0].(mcpsdk.TextContent).Text
	header := "--- START RECALL LOG BLOCK [SESSION: " + sessID2 + "] ---\n"
	footer := "\n--- END RECALL LOG BLOCK ---"
	if !strings.HasPrefix(text, header) || !strings.HasSuffix(text, footer) {
		t.Fatalf("invalid wrapper")
	}
	inner := text[len(header) : len(text)-len(footer)]
	if len(inner) > 80 {
		t.Errorf("inner content length = %d, exceeds budget 80", len(inner))
	}
	if !strings.Contains(inner, "[... TEXT TRUNCATED BY RECALL ENGINE ...]") {
		t.Errorf("missing truncation message inside inner")
	}
}
