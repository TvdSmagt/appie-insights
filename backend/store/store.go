// Package store provides a shared SQLite connection factory for the backend.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open returns a write-capable SQLite connection with WAL mode, a 30 s busy
// timeout, and foreign-key enforcement. The single connection cap prevents
// write-write contention; WAL still allows concurrent readers.
func Open(path string) (*sql.DB, error) {
	return open(path, 1, true)
}

// OpenReader returns a read-optimised SQLite connection with WAL mode, a 30 s
// busy timeout, and up to 4 concurrent connections. Foreign-key enforcement is
// omitted because readers never mutate data.
func OpenReader(path string) (*sql.DB, error) {
	return open(path, 4, false)
}

func open(path string, maxConns int, foreignKeys bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=30000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if foreignKeys {
		if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable FK: %w", err)
		}
	}
	return db, nil
}
