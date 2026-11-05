package b2b

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/platform/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// pluginName is the stable Name() / schema_migrations namespace.
const pluginName = "shopanda/b2b"

// legacyBareMigrationBasenames is the frozen set of files that existed when
// B2B still used RunFS. Only these may be adopted from bare keys; later files
// never consult bare rows. On fresh apply these four basenames are also
// dual-written into the global schema_migrations namespace (reserved names —
// do not reuse them in core or other plugins).
var legacyBareMigrationBasenames = []string{
	"001_create_customer_groups.sql",
	"002_create_customer_group_prices.sql",
	"003_allow_zero_group_price_amounts.sql",
	"004_validate_zero_group_price_amount_check.sql",
}

// initMigrateLockWait bounds how long Init waits per file for the plugin
// migrate advisory lock. DDL itself uses context.Background() semantics
// (no overall deadline) so large VALIDATE CONSTRAINT scans are not cancelled.
const initMigrateLockWait = 30 * time.Second

func runPluginMigrations(ctx context.Context, db *sql.DB) (migrate.PluginFSResult, error) {
	var zero migrate.PluginFSResult
	if db == nil {
		return zero, fmt.Errorf("b2b plugin: database not configured")
	}

	res, err := migrate.RunPluginFSOptions(ctx, db, pluginName, migrationFiles, "migrations", migrate.PluginFSOptions{
		LockWait:           initMigrateLockWait,
		AdoptBareBasenames: legacyBareMigrationBasenames,
	})
	if err != nil {
		return res, fmt.Errorf("b2b plugin: migrate: %w", err)
	}
	return res, nil
}

// RunMigrationsForTest applies embedded B2B migrations (integration tests only).
func RunMigrationsForTest(db *sql.DB) (migrate.PluginFSResult, error) {
	return runPluginMigrations(context.Background(), db)
}
