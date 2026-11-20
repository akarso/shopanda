package pluginsdk

import (
	"context"
	"database/sql"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// MigrationFSOptions configures how a Migrator's SQL migrations are applied.
type MigrationFSOptions = migrate.PluginFSOptions

// MigrationResult aggregates applied/adopted migration counts.
type MigrationResult = migrate.PluginFSResult

// DefaultMigrationLockWait matches the core per-file advisory lock timeout
// used when MigrationFSOptions.LockWait is zero and ctx has no deadline.
const DefaultMigrationLockWait = migrate.DefaultPluginLockWait

// MigrationSpec is the SQL filesystem owned by a Migrator plugin. One spec
// per plugin — basenames share the plugin:{name}: namespace.
//
// Type alias of plugin.MigrationSpec so a plugin.App that implements only
// pluginsdk.Migrator also satisfies plugin.Migrator (CollectMigrations /
// InitAll apply without requiring pluginsdk.Adapt).
type MigrationSpec = plugin.MigrationSpec

// Migrator is an optional pluginsdk.Plugin interface. Implement it to declare
// plugin-owned SQL without Init side effects. Because MigrationSpec aliases
// plugin.MigrationSpec, the same type also implements plugin.Migrator — both
// Adapt bridges and legacy plugin.App registrations are collected.
type Migrator interface {
	Migration() MigrationSpec
}

// ApplyMigrationFS applies spec's SQL against db (integration tests only —
// production apply happens via InitAllContext / `app migrate`).
func ApplyMigrationFS(ctx context.Context, db *sql.DB, pluginName string, spec MigrationSpec) (MigrationResult, error) {
	return migrate.RunPluginFSOptions(ctx, db, pluginName, spec.FS, spec.Dir, spec.Options)
}
