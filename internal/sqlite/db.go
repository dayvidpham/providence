// Package sqlite encapsulates all SQLite operations for Providence.
// It uses zombiezen.com/go/sqlite (NOT database/sql) for explicit
// connection management. All queries are coordinated through a single
// *sqlite.Conn protected by a sync.Mutex for safe concurrent use.
package sqlite

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	zs "zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// DB wraps a zombiezen SQLite connection with a mutex for safe
// concurrent access. All exported functions in this package accept
// *DB and acquire the lock before touching the connection.
//
// A single-writer model: all writes go through the same connection,
// while WAL mode allows readers to operate concurrently at the OS level.
// This is sufficient for Providence's scale (< 10,000 tasks, single process).
type DB struct {
	mu   sync.Mutex
	conn *zs.Conn
}

// Open opens (or creates) a SQLite database at dbPath.
// Parent directories are created if they do not exist.
// The schema is applied via ensureSchema on every Open.
//
// WAL mode, foreign keys, and a 5-second busy timeout are configured.
// Returns an error if the database cannot be opened or the schema cannot be applied.
func Open(dbPath string) (*DB, error) {
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, fmt.Errorf(
				"sqlite.Open: failed to create parent directory for %q: %w — "+
					"ensure the path is writable and the filesystem is mounted",
				dbPath, err,
			)
		}
	}

	conn, err := zs.OpenConn(dbPath, zs.OpenReadWrite|zs.OpenCreate|zs.OpenWAL|zs.OpenURI)
	if err != nil {
		return nil, fmt.Errorf(
			"sqlite.Open: failed to open connection to %q: %w — "+
				"check that the path is accessible and not locked by another process",
			dbPath, err,
		)
	}

	db := &DB{conn: conn}

	if err := db.applyPragmas(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf(
			"sqlite.Open: failed to apply pragmas on %q: %w — "+
				"this is unexpected; check SQLite library version compatibility",
			dbPath, err,
		)
	}

	if err := db.ensureSchema(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf(
			"sqlite.Open: failed to apply schema to %q: %w — "+
				"this may indicate a corrupted database or incompatible schema version",
			dbPath, err,
		)
	}

	return db, nil
}

// OpenMemory opens an in-memory SQLite database.
// Equivalent to Open(":memory:"). Useful for tests and ephemeral sessions.
func OpenMemory() (*DB, error) {
	return Open(":memory:")
}

// Close releases the database connection.
// It is safe to call Close multiple times; subsequent calls are no-ops.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.conn == nil {
		return nil
	}
	err := db.conn.Close()
	db.conn = nil
	if err != nil {
		return fmt.Errorf(
			"sqlite.DB.Close: failed to close connection: %w — "+
				"this may indicate uncommitted transactions; check for deferred work",
			err,
		)
	}
	return nil
}

// Conn returns the underlying *zs.Conn.
// The caller MUST hold db.mu while using the returned connection.
// This is an internal escape hatch for packages that need direct access.
func (db *DB) Conn() *zs.Conn {
	return db.conn
}

// Lock acquires the DB mutex and returns an unlock function.
// Used by callers that need to run multiple statements atomically.
func (db *DB) Lock() (unlock func()) {
	db.mu.Lock()
	return db.mu.Unlock
}

// timeFromNano converts a Unix nanosecond timestamp to a UTC time.Time.
func timeFromNano(ns int64) time.Time {
	return time.Unix(0, ns).UTC()
}

// applyPragmas sets WAL mode, foreign keys, and busy timeout.
func (db *DB) applyPragmas() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, p := range pragmas {
		if err := sqlitex.ExecuteTransient(db.conn, p, nil); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	return nil
}
