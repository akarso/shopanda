package pluginsdk

import (
	"context"
	"database/sql"
	"io/fs"

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
type MigrationSpec struct {
	FS      fs.FS
	Dir     string
	Options MigrationFSOptions
}

// Migrator is an optional pluginsdk.Plugin interface. Implement it to declare
// plugin-owned SQL without Init side effects. pluginsdk.Adapt forwards it to
// the core plugin.Migrator contract so `app migrate` / InitAll pick it up.
type Migrator interface {
	Migration() MigrationSpec
}

func toInternalMigrationSpec(spec MigrationSpec) plugin.MigrationSpec {
	return plugin.MigrationSpec{FS: spec.FS, Dir: spec.Dir, Options: spec.Options}
}

// ApplyMigrationFS applies spec's SQL against db (integration tests only —
// production apply happens via InitAllContext / `app migrate`).
func ApplyMigrationFS(ctx context.Context, db *sql.DB, pluginName string, spec MigrationSpec) (MigrationResult, error) {
	return migrate.RunPluginFSOptions(ctx, db, pluginName, spec.FS, spec.Dir, spec.Options)
}
