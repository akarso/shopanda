package b2b

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func runPluginMigrations(ctx context.Context, db *sql.DB, pluginName string) error {
	if db == nil {
		return fmt.Errorf("b2b plugin: database not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// AdoptBareVersions remaps legacy RunFS bare-basename rows to
	// plugin:{name}:{basename} under the same per-file lock as apply so
	// existing shops do not re-exec DDL (PR-1085).
	if _, err := migrate.RunPluginFSOptions(ctx, db, pluginName, migrationFiles, "migrations", migrate.PluginFSOptions{
		AdoptBareVersions: true,
	}); err != nil {
		return fmt.Errorf("b2b plugin: migrate: %w", err)
	}
	return nil
}

// RunMigrationsForTest applies embedded B2B migrations (integration tests only).
func RunMigrationsForTest(db *sql.DB) error {
	return runPluginMigrations(context.Background(), db, "shopanda/b2b")
}
