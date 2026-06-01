package main

import (
	"path/filepath"
	"testing"

	"appie-insights/backend/schema"
	"appie-insights/backend/store"
)

// TestMigrateStampsFreshDatabase verifies a fresh database (built from schema.DDL,
// user_version 0) is stamped at the current baseline so later migrations know it
// already reflects the full schema.
func TestMigrateStampsFreshDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(schema.DDL); err != nil {
		t.Fatalf("exec DDL: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != baselineVersion {
		t.Errorf("user_version = %d, want %d", version, baselineVersion)
	}
}
