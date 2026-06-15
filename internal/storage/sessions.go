package storage

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SessionStatus values stored in the sessions table.
const (
	SessionActive   = "active"
	SessionClosed   = "closed"
	SessionOrphaned = "orphaned"
)

// LogLineRecord is a single row destined for the log_lines table.
type LogLineRecord struct {
	Text      string
	IsError   bool
	CreatedAt int64
}

// CreateSession inserts a new active session and returns its UUIDv7 identifier.
func (db *DB) CreateSession(projectPath, projectName string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}

	_, err = db.sql.Exec(
		`INSERT INTO sessions (id, project_path, project_name, started_at, status)
		 VALUES (?, ?, ?, ?, ?)`,
		id.String(), projectPath, projectName, time.Now().UnixNano(), SessionActive,
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return id.String(), nil
}

// CloseSession marks a session as closed.
func (db *DB) CloseSession(sessionID string) error {
	_, err := db.sql.Exec(
		`UPDATE sessions SET status = ? WHERE id = ?`,
		SessionClosed, sessionID,
	)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	return nil
}

// InsertLogLines writes a batch of log lines within a single transaction.
func (db *DB) InsertLogLines(sessionID string, lines []LogLineRecord) error {
	if len(lines) == 0 {
		return nil
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO log_lines (session_id, line_text, is_error, created_at)
		 VALUES (?, ?, ?, ?)`,
	)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, line := range lines {
		isError := 0
		if line.IsError {
			isError = 1
		}
		if _, err := stmt.Exec(sessionID, line.Text, isError, line.CreatedAt); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert log line: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// orphanActiveSessions transitions all sessions in "active" status to "orphaned".
func (db *DB) orphanActiveSessions() error {
	_, err := db.sql.Exec(
		`UPDATE sessions SET status = ? WHERE status = ?`,
		SessionOrphaned, SessionActive,
	)
	if err != nil {
		return fmt.Errorf("orphan active sessions: %w", err)
	}
	return nil
}
