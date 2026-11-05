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
// migrate advisory lock. Matches DefaultPluginLockWait so a peer running a
// long VALIDATE CONSTRAINT (or similar) does not cause crash-loops while the
// lock holder is healthy. No overall DDL deadline is applied here; the
// caller's ctx governs statement cancellation.
const initMigrateLockWait = migrate.DefaultPluginLockWait

func migrationOptions() migrate.PluginFSOptions {
	return migrate.PluginFSOptions{
		LockWait:           initMigrateLockWait,
		AdoptBareBasenames: legacyBareMigrationBasenames,
	}
}

func runPluginMigrations(ctx context.Context, db *sql.DB) (migrate.PluginFSResult, error) {
	var zero migrate.PluginFSResult
	if db == nil {
		return zero, fmt.Errorf("b2b plugin: database not configured")
	}

	res, err := migrate.RunPluginFSOptions(ctx, db, pluginName, migrationFiles, "migrations", migrationOptions())
	if err != nil {
		return res, fmt.Errorf("b2b plugin: migrate: %w", err)
	}
	return res, nil
}

// RunMigrationsForTest applies embedded B2B migrations (integration tests only).
func RunMigrationsForTest(db *sql.DB) (migrate.PluginFSResult, error) {
	return runPluginMigrations(context.Background(), db)
}
