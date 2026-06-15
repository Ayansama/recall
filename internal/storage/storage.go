package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const (
	dbDirName  = ".recall"
	dbFileName = "recall.db"
)

// DB wraps the local SQLite database used by Recall.
type DB struct {
	sql *sql.DB
	path string
}

// DefaultPath returns the canonical database path (~/.recall/recall.db).
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, dbDirName, dbFileName), nil
}

// Open opens (or creates) the Recall database at the default path and
// initializes the schema with WAL optimizations enabled.
func Open() (*DB, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return OpenAt(path)
}

// OpenAt opens (or creates) the Recall database at the given path.
func OpenAt(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := configure(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}

	if err := initSchema(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}

	db := &DB{sql: sqlDB, path: path}
	if err := db.orphanActiveSessions(); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return db, nil
}

func configure(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("apply %q: %w", p, err)
		}
	}
	return nil
}

func initSchema(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("initialize schema: %w", err)
	}
	return nil
}

// Path returns the filesystem path of the open database.
func (db *DB) Path() string {
	return db.path
}

// SQL returns the underlying *sql.DB for query operations.
func (db *DB) SQL() *sql.DB {
	return db.sql
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.sql == nil {
		return nil
	}
	return db.sql.Close()
}
