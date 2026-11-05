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

// MigrationSpec is one SQL filesystem owned by a plugin.
type MigrationSpec struct {
	FS      fs.FS
	Dir     string
	Options migrate.PluginFSOptions
}

// Migrator is an optional Plugin interface. Implement it to declare plugin-owned
// SQL without Init side effects. app migrate / setup collect Migrator specs
// without calling Init; InitAll applies them (when Bootstrap.DB is set) before
// Init so tables exist for repo wiring.
type Migrator interface {
	Migrations() []MigrationSpec
}

// MigrationRegistration is a named MigrationSpec ready to apply.
type MigrationRegistration struct {
	PluginName string
	FS         fs.FS
	Dir        string
	Options    migrate.PluginFSOptions
}

// PluginMigrateResult aggregates RunPluginFS outcomes across plugins.
type PluginMigrateResult struct {
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

// orderedEntries returns registry entries in InitAll order. When depends_on is
// unset (or sorting fails), the returned slice shares the registry backing
// array so InitAll state updates stick on r.entries.
func (r *Registry) orderedEntries(app *App) []Entry {
	entries := r.entries
	var depends map[string][]string
	if app != nil && app.Config != nil {
		depends = app.Config.Plugins.DependsOn
	}
	if len(depends) == 0 {
		return entries
	}
	sorted, err := SortEntriesByDependsOn(entries, depends)
	if err != nil {
		if r != nil && r.log != nil {
			r.log.Error("plugin.depends_on.sort_failed", err, nil)
		}
		return entries
	}
	return sorted
}

// CollectMigrations returns validated registrations for every Migrator plugin
// in InitAll order. Does not call Init. Invalid or empty specs error.
func (r *Registry) CollectMigrations(app *App) ([]MigrationRegistration, error) {
	if r == nil {
		return nil, fmt.Errorf("plugin: registry is required")
	}
	entries := r.orderedEntries(app)
	var out []MigrationRegistration
	for _, e := range entries {
		m, ok := e.Plugin.(Migrator)
		if !ok {
			continue
		}
		specs := m.Migrations()
		if len(specs) == 0 {
			return nil, fmt.Errorf("plugin: %s: Migrator returned no migration specs", e.Name)
		}
		for _, spec := range specs {
			reg, err := migrationRegistration(e.Name, spec)
			if err != nil {
				return nil, err
			}
			out = append(out, reg)
		}
	}
	return out, nil
}

// ApplyMigrationRegistrations runs RunPluginFSOptions for each registration.
func ApplyMigrationRegistrations(ctx context.Context, db *sql.DB, log logger.Logger, regs []MigrationRegistration) (PluginMigrateResult, error) {
	var totals PluginMigrateResult
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

// applyPluginMigrator runs Migrator specs for one plugin before Init.
func applyPluginMigrator(ctx context.Context, app *App, name string, p Plugin) error {
	m, ok := p.(Migrator)
	if !ok {
		return nil
	}
	if app == nil || app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return nil
	}
	specs := m.Migrations()
	if len(specs) == 0 {
		return fmt.Errorf("plugin: %s: Migrator returned no migration specs", name)
	}
	var regs []MigrationRegistration
	for _, spec := range specs {
		reg, err := migrationRegistration(name, spec)
		if err != nil {
			return err
		}
		regs = append(regs, reg)
	}
	_, err := ApplyMigrationRegistrations(ctx, app.Bootstrap.DB, app.Logger, regs)
	return err
}
