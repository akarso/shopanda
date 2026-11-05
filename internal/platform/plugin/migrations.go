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

// PluginMigrateMode controls how InitAll handles Migrator plugins.
type PluginMigrateMode int

const (
	// PluginMigrateApply runs RunPluginFS before Init (default for serve/worker/scheduler).
	PluginMigrateApply PluginMigrateMode = iota
	// PluginMigrateVerify checks schema_migrations for pending plugin versions
	// and fails with "run app migrate" without taking the advisory lock.
	PluginMigrateVerify
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
// without calling Init; InitAllContext applies or verifies them before Init.
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

func migrationRegistration(pluginName string, spec MigrationSpec) (reg MigrationRegistration, err error) {
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
	// Catch typed-nil FS and missing dirs early (also recovers panics from Open/ReadDir).
	defer func() {
		if rv := recover(); rv != nil {
			err = &panicError{value: rv}
		}
	}()
	if _, readErr := fs.ReadDir(spec.FS, dir); readErr != nil {
		return MigrationRegistration{}, fmt.Errorf("plugin: %s: read migration dir %q: %w", name, dir, readErr)
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

func hasMigrator(entries []Entry) bool {
	for _, e := range entries {
		if _, ok := e.Plugin.(Migrator); ok {
			return true
		}
	}
	return false
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

func listSQLBasenames(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

func appliedVersions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

// verifyPluginMigration fails if any SQL basename for the registration is not
// yet recorded under plugin:{name}:{basename}.
func verifyPluginMigration(db *sql.DB, reg MigrationRegistration) error {
	names, err := listSQLBasenames(reg.FS, reg.Dir)
	if err != nil {
		return fmt.Errorf("plugin: %s: list migrations: %w", reg.PluginName, err)
	}
	applied, err := appliedVersions(db)
	if err != nil {
		return fmt.Errorf("plugin: %s: query schema_migrations: %w", reg.PluginName, err)
	}
	for _, base := range names {
		version := "plugin:" + reg.PluginName + ":" + base
		if !applied[version] {
			return fmt.Errorf("plugin: %s: pending migration %s — run app migrate", reg.PluginName, base)
		}
	}
	return nil
}

// applyPluginMigrator runs or verifies a plugin's Migrator SQL before Init.
func applyPluginMigrator(ctx context.Context, app *App, name string, p Plugin) (err error) {
	if app == nil || app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return nil
	}
	reg, ok, regErr := migratorRegistration(name, p)
	if !ok {
		return nil
	}
	if regErr != nil {
		return regErr
	}
	defer func() {
		if rv := recover(); rv != nil {
			err = &panicError{value: rv}
		}
	}()
	mode := app.PluginMigrateMode
	switch mode {
	case PluginMigrateVerify:
		return verifyPluginMigration(app.Bootstrap.DB, reg)
	default:
		_, err = ApplyMigrationRegistrations(ctx, app.Bootstrap.DB, app.Logger, []MigrationRegistration{reg})
		return err
	}
}

// InitSummaryError returns a non-nil error when InitAll/InitAllContext left the
// process in a state that must not continue (depends_on or Migrator failure).
func InitSummaryError(summary InitSummary) error {
	if summary.DependsOnErr != nil {
		return fmt.Errorf("plugin depends_on: %w", summary.DependsOnErr)
	}
	if summary.MigrateFailed > 0 {
		return fmt.Errorf("plugin migrate failed: %d plugin(s) failed schema apply (see plugin.init.failed logs)", summary.MigrateFailed)
	}
	return nil
}
