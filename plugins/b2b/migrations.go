package b2b

import (
	"context"
	"database/sql"
	"embed"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
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

// initMigrateLockWait bounds how long plugin migrate waits per file for the
// advisory lock. Matches DefaultPluginLockWait so a peer running a long
// VALIDATE CONSTRAINT does not crash-loop waiters while the holder is healthy.
const initMigrateLockWait = migrate.DefaultPluginLockWait

func migrationOptions() migrate.PluginFSOptions {
	return migrate.PluginFSOptions{
		LockWait:           initMigrateLockWait,
		AdoptBareBasenames: legacyBareMigrationBasenames,
	}
}

var _ plugin.Migrator = (*Plugin)(nil)

// Migrations implements plugin.Migrator. Declared once for migrate CLI and
// InitAll (applied before Init when Bootstrap.DB is set).
func (p *Plugin) Migrations() []plugin.MigrationSpec {
	return []plugin.MigrationSpec{{
		FS:      migrationFiles,
		Dir:     "migrations",
		Options: migrationOptions(),
	}}
}

// RunMigrationsForTest applies embedded B2B migrations (integration tests only).
func RunMigrationsForTest(db *sql.DB) (migrate.PluginFSResult, error) {
	return migrate.RunPluginFSOptions(context.Background(), db, pluginName, migrationFiles, "migrations", migrationOptions())
}
