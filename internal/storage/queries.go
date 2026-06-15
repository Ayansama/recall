package storage

import (
	"database/sql"
	"fmt"
)

const defaultListSessionsLimit = 10

// SessionSummary is a lightweight view of a stored terminal session.
type SessionSummary struct {
	ID          string `json:"id"`
	ProjectName string `json:"project_name"`
	ProjectPath string `json:"project_path"`
	StartedAt   int64  `json:"started_at"`
	Status      string `json:"status"`
}

// ListSessions returns recent sessions ordered by start time descending.
func (db *DB) ListSessions(limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		limit = defaultListSessionsLimit
	}

	rows, err := db.sql.Query(
		`SELECT id, project_name, project_path, started_at, status
		 FROM sessions ORDER BY started_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		var s SessionSummary
		if err := rows.Scan(&s.ID, &s.ProjectName, &s.ProjectPath, &s.StartedAt, &s.Status); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	if sessions == nil {
		sessions = []SessionSummary{}
	}
	return sessions, nil
}

// GetLatestErrorWindow returns up to 61 log lines surrounding the most recent
// error row, the session ID associated with that error, and an error if any.
// When sessionID is empty the search spans all sessions.
func (db *DB) GetLatestErrorWindow(sessionID string) ([]string, string, error) {
	var errorRowID int64
	var actualSessionID string
	var err error

	if sessionID == "" {
		err = db.sql.QueryRow(
			`SELECT id, session_id FROM log_lines WHERE is_error = 1 ORDER BY id DESC LIMIT 1`,
		).Scan(&errorRowID, &actualSessionID)
	} else {
		actualSessionID = sessionID
		err = db.sql.QueryRow(
			`SELECT id FROM log_lines WHERE is_error = 1 AND session_id = ?
			 ORDER BY id DESC LIMIT 1`,
			sessionID,
		).Scan(&errorRowID)
	}
	if err == sql.ErrNoRows {
		return []string{}, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("find latest error: %w", err)
	}

	rows, err := db.sql.Query(
		`SELECT line_text FROM log_lines
		 WHERE id BETWEEN ? AND ?
		 ORDER BY id ASC`,
		errorRowID-30, errorRowID+30,
	)
	if err != nil {
		return nil, "", fmt.Errorf("fetch error window: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, "", fmt.Errorf("scan log line: %w", err)
		}
		lines = append(lines, text)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate error window: %w", err)
	}
	if lines == nil {
		lines = []string{}
	}
	return lines, actualSessionID, nil
}

// GetSessionContextLines returns the first 25 lines (head) and the last 150 lines (tail)
// of the given session, and the total lines count.
func (db *DB) GetSessionContextLines(sessionID string) (head []string, tail []string, totalCount int, err error) {
	err = db.sql.QueryRow(
		`SELECT COUNT(*) FROM log_lines WHERE session_id = ?`,
		sessionID,
	).Scan(&totalCount)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("count session lines: %w", err)
	}

	if totalCount == 0 {
		return []string{}, []string{}, 0, nil
	}

	// Fetch head (up to 25 lines)
	headRows, err := db.sql.Query(
		`SELECT line_text FROM log_lines WHERE session_id = ? ORDER BY id ASC LIMIT 25`,
		sessionID,
	)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("fetch session head: %w", err)
	}
	defer headRows.Close()

	for headRows.Next() {
		var line string
		if err := headRows.Scan(&line); err != nil {
			return nil, nil, 0, fmt.Errorf("scan session head line: %w", err)
		}
		head = append(head, line)
	}
	if err := headRows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("iterate session head: %w", err)
	}

	// If totalCount <= 175, they overlap or are contiguous. In this case, we retrieve
	// all lines in the session as head, and leave tail empty.
	if totalCount <= 175 {
		if totalCount > 25 {
			// Fetch all lines
			allRows, err := db.sql.Query(
				`SELECT line_text FROM log_lines WHERE session_id = ? ORDER BY id ASC`,
				sessionID,
			)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("fetch all session lines: %w", err)
			}
			defer allRows.Close()

			head = nil
			for allRows.Next() {
				var line string
				if err := allRows.Scan(&line); err != nil {
					return nil, nil, 0, fmt.Errorf("scan session line: %w", err)
				}
				head = append(head, line)
			}
			if err := allRows.Err(); err != nil {
				return nil, nil, 0, fmt.Errorf("iterate all session lines: %w", err)
			}
		}
		return head, nil, totalCount, nil
	}

	// If totalCount > 175, fetch tail (last 150 lines) sorted ascending
	tailRows, err := db.sql.Query(
		`SELECT line_text FROM (
			SELECT id, line_text FROM log_lines WHERE session_id = ? ORDER BY id DESC LIMIT 150
		) AS sub ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("fetch session tail: %w", err)
	}
	defer tailRows.Close()

	for tailRows.Next() {
		var line string
		if err := tailRows.Scan(&line); err != nil {
			return nil, nil, 0, fmt.Errorf("scan session tail line: %w", err)
		}
		tail = append(tail, line)
	}
	if err := tailRows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("iterate session tail: %w", err)
	}

	if head == nil {
		head = []string{}
	}
	if tail == nil {
		tail = []string{}
	}
	return head, tail, totalCount, nil
}

// PurgeAll clears all database tables and vacuums the database.
func (db *DB) PurgeAll() error {
	tx, err := db.sql.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM log_lines`); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete log_lines: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM sessions`); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	if _, err := db.sql.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	return nil
}


