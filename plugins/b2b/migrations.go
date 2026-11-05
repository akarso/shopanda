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

// LegacyBareMigrationBasenames is the frozen set of files that existed when
// B2B still used RunFS. Only these may be adopted from bare keys; later files
// never consult bare rows (avoids colliding with core or other plugins).
var LegacyBareMigrationBasenames = []string{
	"001_create_customer_groups.sql",
	"002_create_customer_group_prices.sql",
	"003_allow_zero_group_price_amounts.sql",
	"004_validate_zero_group_price_amount_check.sql",
}

// initMigrateLockWait bounds how long Init waits per file for the plugin
// migrate advisory lock. Keep well below typical deploy health-check budgets.
const initMigrateLockWait = 30 * time.Second

// initMigrateTimeout bounds the whole B2B migrate pass during Init.
const initMigrateTimeout = 2 * time.Minute

func runPluginMigrations(ctx context.Context, db *sql.DB) (migrate.PluginFSResult, error) {
	var zero migrate.PluginFSResult
	if db == nil {
		return zero, fmt.Errorf("b2b plugin: database not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, initMigrateTimeout)
	defer cancel()

	res, err := migrate.RunPluginFSOptions(ctx, db, pluginName, migrationFiles, "migrations", migrate.PluginFSOptions{
		LockWait:           initMigrateLockWait,
		AdoptBareBasenames: LegacyBareMigrationBasenames,
	})
	if err != nil {
		return res, fmt.Errorf("b2b plugin: migrate: %w", err)
	}
	return res, nil
}

// RunMigrationsForTest applies embedded B2B migrations (integration tests only).
func RunMigrationsForTest(db *sql.DB) error {
	_, err := runPluginMigrations(context.Background(), db)
	return err
}
