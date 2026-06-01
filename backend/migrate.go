package main

import (
	"database/sql"
	"fmt"
)

// baselineVersion is the schema version that a freshly-created database is
// stamped with. It must always equal the highest migration version. A fresh
// database is built from the current schema.DDL, so it already reflects the
// full schema; stamping it at the baseline marks it as up to date. Bump this
// whenever a new migration is added.
//
// This is the 1.0 baseline: the pre-1.0 migration chain (v1–v10) was collapsed
// into schema.DDL, since no released database predates it.
const baselineVersion = 1

// runMigrations applies schema changes needed to bring an existing database up
// to the current version. Add new migrations at the bottom; never remove or
// reorder existing entries.
func runMigrations(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	if version == 0 {
		// Fresh database — schema is already current via initDB, just stamp it.
		_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", baselineVersion))
		return err
	}

	type step struct {
		ver int
		run func(*sql.DB) error
	}
	// No post-baseline migrations yet. Append new ones here as the schema evolves.
	var steps []step

	for _, s := range steps {
		if version >= s.ver {
			continue
		}
		if err := s.run(db); err != nil {
			return fmt.Errorf("migration %d: %w", s.ver, err)
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", s.ver)); err != nil {
			return fmt.Errorf("set user_version=%d: %w", s.ver, err)
		}
	}

	return nil
}
