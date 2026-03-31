package store

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB with convenience methods.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the SQLite database at the given path.
// Pass ":memory:" for an in-process test database.
func Open(path string) (*DB, error) {
	if path == "" {
		var err error
		path, err = defaultDBPath()
		if err != nil {
			return nil, fmt.Errorf("store: resolve db path: %w", err)
		}
	}

	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("store: create db dir: %w", err)
		}
	}

	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}

	db := &DB{sql: sqlDB}

	if err := db.configurePragmas(); err != nil {
		sqlDB.Close()
		return nil, err
	}

	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.sql.Close()
}

func (db *DB) configurePragmas() error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -8000",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.sql.Exec(p); err != nil {
			return fmt.Errorf("store: pragma %q: %w", p, err)
		}
	}
	return nil
}

func (db *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS session (
			id         TEXT    PRIMARY KEY,
			project_id TEXT    NOT NULL,
			title      TEXT    DEFAULT '',
			model      TEXT    DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			summary    TEXT    DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS message (
			id         TEXT    PRIMARY KEY,
			session_id TEXT    NOT NULL REFERENCES session(id),
			role       TEXT    NOT NULL,
			content    BLOB    NOT NULL,
			model      TEXT    DEFAULT '',
			tokens_in  INTEGER DEFAULT 0,
			tokens_out INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_message_session ON message(session_id)`,
		`CREATE TABLE IF NOT EXISTS permission_rule (
			id         TEXT    PRIMARY KEY,
			source     TEXT    NOT NULL,
			tool       TEXT    NOT NULL,
			pattern    TEXT    NOT NULL,
			action     TEXT    NOT NULL,
			created_at INTEGER NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.sql.Exec(s); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	return nil
}

// newID returns a new ULID string suitable for use as a primary key.
func newID() string {
	entropy := ulid.Monotonic(rand.Reader, 0)
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// defaultDBPath returns the XDG/platform-appropriate path for the database.
func defaultDBPath() (string, error) {
	var base string
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support", "altcode")
	} else {
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			xdg = filepath.Join(home, ".local", "share")
		}
		base = filepath.Join(xdg, "altcode")
	}
	return filepath.Join(base, "altcode.db"), nil
}
