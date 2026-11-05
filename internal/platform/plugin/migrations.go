package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"strings"

	"github.com/akarso/shopanda/internal/platform/migrate"
)

// MigrationRegistration is a plugin-owned SQL FS applied by app migrate and
// as an Init safety net via migrate.RunPluginFSOptions.
type MigrationRegistration struct {
	PluginName string
	FS         fs.FS
	Dir        string
	Options    migrate.PluginFSOptions
}

// RegisterMigrations records an embedded (or other) FS of plugin SQL.
// Call from Plugin.Init with the plugin's Name(). When Init runs via
// Registry.InitAll, pluginName must match the plugin being initialized.
// opts may be zero; set AdoptBareBasenames when remapping legacy RunFS rows.
func (a *App) RegisterMigrations(pluginName string, fsys fs.FS, dir string, opts migrate.PluginFSOptions) error {
	if a == nil {
		return fmt.Errorf("plugin: app not configured")
	}
	name := strings.TrimSpace(pluginName)
	if name == "" {
		return fmt.Errorf("plugin: RegisterMigrations: plugin name is required")
	}
	if initName := strings.TrimSpace(a.initPluginName); initName != "" && initName != name {
		return fmt.Errorf("plugin: RegisterMigrations: name %q does not match Init plugin %q", name, initName)
	}
	if fsys == nil {
		return fmt.Errorf("plugin: %s: RegisterMigrations: fs is required", name)
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("plugin: %s: RegisterMigrations: dir is required", name)
	}
	for _, existing := range a.migrations {
		if existing.PluginName == name {
			return fmt.Errorf("plugin: %s: migrations already registered", name)
		}
	}
	a.migrations = append(a.migrations, MigrationRegistration{
		PluginName: name,
		FS:         fsys,
		Dir:        dir,
		Options:    opts,
	})
	return nil
}

// Migrations returns a copy of registered plugin migration FS entries.
func (a *App) Migrations() []MigrationRegistration {
	if a == nil || len(a.migrations) == 0 {
		return nil
	}
	out := make([]MigrationRegistration, len(a.migrations))
	copy(out, a.migrations)
	return out
}

// ApplyMigrations runs migrate.RunPluginFSOptions for every registration on db.
// Idempotent with Init safety-net applies (same lock and version keys).
func (a *App) ApplyMigrations(ctx context.Context, db *sql.DB) error {
	if a == nil {
		return fmt.Errorf("plugin: app not configured")
	}
	if db == nil {
		return fmt.Errorf("plugin: ApplyMigrations: database is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, reg := range a.migrations {
		res, err := migrate.RunPluginFSOptions(ctx, db, reg.PluginName, reg.FS, reg.Dir, reg.Options)
		if err != nil {
			return fmt.Errorf("plugin: migrate %s: %w", reg.PluginName, err)
		}
		if a.Logger != nil && (res.Applied > 0 || res.Adopted > 0) {
			a.Logger.Info("plugin.migrate", map[string]interface{}{
				"plugin":  reg.PluginName,
				"applied": res.Applied,
				"adopted": res.Adopted,
			})
		}
	}
	return nil
}

func (a *App) setInitPluginName(name string) {
	if a == nil {
		return
	}
	a.initPluginName = name
}
