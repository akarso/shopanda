package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"strings"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

// MigrationSpec is the SQL filesystem owned by a Migrator plugin.
// One spec per plugin — basenames share the plugin:{name}: namespace.
type MigrationSpec struct {
	FS      fs.FS
	Dir     string
	Options migrate.PluginFSOptions
}

// Migrator is an optional Plugin interface. Implement it to declare plugin-owned
// SQL without Init side effects. app migrate / setup collect Migrator specs
// without calling Init; InitAll applies them (when Bootstrap.DB is set and
// SkipPluginMigrations is false) before Init so tables exist for repo wiring.
type Migrator interface {
	Migration() MigrationSpec
}

// MigrationRegistration is a named MigrationSpec ready to apply.
type MigrationRegistration struct {
	PluginName string
	FS         fs.FS
	Dir        string
	Options    migrate.PluginFSOptions
}

// MigrateResult aggregates RunPluginFS outcomes across plugins.
type MigrateResult struct {
	Applied int
	Adopted int
}

func migrationRegistration(pluginName string, spec MigrationSpec) (MigrationRegistration, error) {
	name := strings.TrimSpace(pluginName)
	if name == "" {
		return MigrationRegistration{}, fmt.Errorf("plugin: migration: plugin name is required")
	}
	dir := strings.TrimSpace(spec.Dir)
	if spec.FS == nil {
		return MigrationRegistration{}, fmt.Errorf("plugin: %s: migration FS is required", name)
	}
	if dir == "" {
		return MigrationRegistration{}, fmt.Errorf("plugin: %s: migration dir is required", name)
	}
	opts := spec.Options
	if n := len(opts.AdoptBareBasenames); n > 0 {
		copied := make([]string, n)
		copy(copied, opts.AdoptBareBasenames)
		opts.AdoptBareBasenames = copied
	}
	return MigrationRegistration{
		PluginName: name,
		FS:         spec.FS,
		Dir:        dir,
		Options:    opts,
	}, nil
}

// migratorRegistration calls Migrator.Migration under recover and validates the
// spec. ok is false when p is not a Migrator.
func migratorRegistration(name string, p Plugin) (reg MigrationRegistration, ok bool, err error) {
	m, isMigrator := p.(Migrator)
	if !isMigrator {
		return MigrationRegistration{}, false, nil
	}
	ok = true
	defer func() {
		if rv := recover(); rv != nil {
			err = &panicError{value: rv}
		}
	}()
	reg, err = migrationRegistration(name, m.Migration())
	return reg, ok, err
}

// orderedEntries returns registry entries in depends_on order.
// When dependsOn is empty, the returned slice shares r.entries.
// A sort error is returned to the caller (migrate must not silently reorder).
func (r *Registry) orderedEntries(dependsOn map[string][]string) ([]Entry, error) {
	entries := r.entries
	if len(dependsOn) == 0 {
		return entries, nil
	}
	return SortEntriesByDependsOn(entries, dependsOn)
}

// CollectMigrations returns validated registrations for every Migrator plugin
// in depends_on order. Does not call Init. Panics inside Migration() are
// recovered and returned as errors naming the plugin.
func (r *Registry) CollectMigrations(dependsOn map[string][]string) ([]MigrationRegistration, error) {
	if r == nil {
		return nil, fmt.Errorf("plugin: registry is required")
	}
	entries, err := r.orderedEntries(dependsOn)
	if err != nil {
		return nil, fmt.Errorf("plugin: depends_on: %w", err)
	}
	var out []MigrationRegistration
	for _, e := range entries {
		reg, ok, regErr := migratorRegistration(e.Name, e.Plugin)
		if !ok {
			continue
		}
		if regErr != nil {
			return nil, fmt.Errorf("plugin: %s: %w", e.Name, regErr)
		}
		out = append(out, reg)
	}
	return out, nil
}

// ApplyMigrationRegistrations runs RunPluginFSOptions for each registration.
func ApplyMigrationRegistrations(ctx context.Context, db *sql.DB, log logger.Logger, regs []MigrationRegistration) (MigrateResult, error) {
	var totals MigrateResult
	if db == nil {
		return totals, fmt.Errorf("plugin: ApplyMigrations: database is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, reg := range regs {
		res, err := migrate.RunPluginFSOptions(ctx, db, reg.PluginName, reg.FS, reg.Dir, reg.Options)
		if err != nil {
			return totals, fmt.Errorf("plugin: migrate %s: %w", reg.PluginName, err)
		}
		totals.Applied += res.Applied
		totals.Adopted += res.Adopted
		if log != nil && (res.Applied > 0 || res.Adopted > 0) {
			log.Info("plugin.migrate", map[string]interface{}{
				"plugin":  reg.PluginName,
				"applied": res.Applied,
				"adopted": res.Adopted,
			})
		}
	}
	return totals, nil
}

// applyPluginMigrator runs a plugin's Migrator SQL before Init.
func applyPluginMigrator(ctx context.Context, app *App, name string, p Plugin) error {
	if app != nil && app.SkipPluginMigrations {
		return nil
	}
	if app == nil || app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return nil
	}
	reg, ok, err := migratorRegistration(name, p)
	if !ok {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = ApplyMigrationRegistrations(ctx, app.Bootstrap.DB, app.Logger, []MigrationRegistration{reg})
	return err
}
