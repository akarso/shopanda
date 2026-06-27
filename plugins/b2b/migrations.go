package b2b

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func runPluginMigrations(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("b2b plugin: database not configured")
	}
	if _, err := migrate.RunFS(db, migrationFiles, "migrations"); err != nil {
		return fmt.Errorf("b2b plugin: migrate: %w", err)
	}
	return nil
}

// RunMigrationsForTest applies embedded B2B migrations (integration tests only).
func RunMigrationsForTest(db *sql.DB) error {
	return runPluginMigrations(db)
}
