package main

import (
	"database/sql"

	"appie-insights/backend/schema"
)

func initDB(db *sql.DB) error {
	if _, err := db.Exec(schema.DDL); err != nil {
		return err
	}
	return runMigrations(db)
}
