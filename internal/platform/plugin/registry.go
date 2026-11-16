package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// State represents the lifecycle state of a plugin.
type State string

const (
	StateLoaded State = "loaded"
	StateActive State = "active"
	StateFailed State = "failed"
)

// InitSummary reports the outcome of InitAll / InitAllContext.
type InitSummary struct {
	Registered    int
	Initialized   int
	Failed        int
	MigrateFailed int // Migrator apply/verify failures; always counted in Failed
	// Canceled is how many Loaded plugins were marked Failed because the boot
	// context was canceled before their Init ran (not ordinary Init errors).
	Canceled     int
	DependsOnErr error // non-nil when depends_on sort failed and a Migrator is registered
}

// Entry tracks a registered plugin and its lifecycle state.
type Entry struct {
	Plugin Plugin
	Name   string
	State  State
	Err    error // non-nil when State == StateFailed
}

// Registry manages plugin loading and initialization.
//
// Plugins register at compile time (see cmd/api/register_plugins.go).
// Runtime .so loading was evaluated and deferred — see
// PLUGINS.md deferred capabilities (PR-544).
type Registry struct {
	entries        []Entry
	log            logger.Logger
	configRegistry *ConfigRegistry
	cliRegistry    *cli.Registry
}

// NewRegistry creates a Registry.
func NewRegistry(log logger.Logger) *Registry {
	if log == nil {
		panic("plugin: logger must not be nil")
	}
	return &Registry{
		log:            log,
		configRegistry: NewConfigRegistry(),
		cliRegistry:    cli.NewRegistry(),
	}
}

// ConfigRegistry returns plugin-registered admin config fields.
func (r *Registry) ConfigRegistry() *ConfigRegistry {
	if r == nil {
		return nil
	}
	return r.configRegistry
}

// Register adds a plugin to the registry in the loaded state.
// Panics if the plugin is nil or has an empty name.
// Panics if a plugin with the same name is already registered.
func (r *Registry) Register(p Plugin) {
	if p == nil {
		panic("plugin: plugin must not be nil")
	}
	name := p.Name()
	if name == "" {
		panic("plugin: plugin name must not be empty")
	}
	for _, e := range r.entries {
		if e.Name == name {
			panic(fmt.Sprintf("plugin: duplicate plugin name: %q", name))
		}
	}
	r.entries = append(r.entries, Entry{
		Plugin: p,
		Name:   name,
		State:  StateLoaded,
	})
	r.log.Info("plugin.registered", map[string]interface{}{
		"plugin": name,
	})
}

// CLIRegistry returns plugin-registered CLI commands.
func (r *Registry) CLIRegistry() *cli.Registry {
	if r == nil {
		return nil
	}
	return r.cliRegistry
}

// InitAll is InitAllContext with context.Background().
func (r *Registry) InitAll(app *App) InitSummary {
	return r.InitAllContext(context.Background(), app)
}

// InitAllContext initializes all loaded plugins by calling Init(app).
// When Bootstrap.DB is set, each Migrator's SQL is applied (PluginMigrateApply)
// or verified (PluginMigrateVerify) before Init. ctx cancels Migrator work.
// Panics in Migration() / apply are recovered. A depends_on sort error with any
// Migrator registered sets DependsOnErr and skips all inits (same as migrate).
// The first Migrator failure stops further plugins (fail-fast).
func (r *Registry) InitAllContext(ctx context.Context, app *App) InitSummary {
	if app == nil {
		panic("plugin: app must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	app.configRegistry = r.configRegistry
	var depends map[string][]string
	if app.Config != nil {
		depends = app.Config.Plugins.DependsOn
	}
	summary := InitSummary{Registered: len(r.entries)}
	entries, sortErr := r.orderedEntries(depends)
	if sortErr != nil {
		r.log.Error("plugin.depends_on.sort_failed", sortErr, nil)
		if hasMigrator(r.entries) {
			// Fail closed: do not Init anyone. Failed matches Registered so
			// callers that only check Failed still refuse to continue.
			summary.DependsOnErr = sortErr
			summary.Failed = summary.Registered
			return summary
		}
		entries = r.entries
	}
	for i := range entries {
		if err := ctx.Err(); err != nil {
			// Deadline/cancel: mark every remaining Loaded plugin failed so callers
			// that care about a full boot can refuse. Ordinary Init errors are not
			// counted in Canceled. Plugins that already finished Init stay Active.
			skipped := 0
			for j := i; j < len(entries); j++ {
				live := r.entryByName(entries[j].Name)
				if live == nil || live.State != StateLoaded {
					continue
				}
				live.State = StateFailed
				live.Err = err
				summary.Failed++
				summary.Canceled++
				skipped++
			}
			r.log.Error("plugin.init.canceled", err, map[string]interface{}{
				"remaining": skipped,
			})
			break
		}
		e := &entries[i]
		// Resolve the live registry entry so State updates persist under depends_on sorts.
		live := r.entryByName(e.Name)
		if live == nil || live.State != StateLoaded {
			continue
		}
		name := live.Name
		app.setCurrentPluginName(name)
		r.log.Info("plugin.init.start", map[string]interface{}{
			"plugin": name,
		})
		if nd, ok := live.Plugin.(NeedsDeclarer); ok {
			app.RecordNeeds(nd.Needs()...)
		}
		if err := applyPluginMigrator(ctx, app, name, live.Plugin); err != nil {
			live.State = StateFailed
			live.Err = err
			summary.Failed++
			summary.MigrateFailed++
			cause := "migrate"
			if _, ok := err.(*panicError); ok {
				cause = "migrate_panic"
			}
			r.log.Error("plugin.init.failed", err, map[string]interface{}{
				"plugin": name,
				"cause":  cause,
			})
			// Fail-fast: do not Init remaining plugins after a schema failure.
			break
		}
		stagingCLI := cli.NewRegistry()
		app.cliRegistry = stagingCLI
		extBefore := app.extensionCodeSnapshot()
		err := app.settleExtensionFields(name, extBefore, r.safeInit(ctx, live, app))
		if err != nil {
			live.State = StateFailed
			live.Err = err
			summary.Failed++
			// Only boot-context cancel counts as Canceled. A plugin-local
			// WithTimeout (e.g. SQS) returns DeadlineExceeded while ctx is live —
			// that stays an ordinary Failed so serve can continue.
			if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				summary.Canceled++
			}
			cause := "error"
			if _, ok := err.(*panicError); ok {
				cause = "panic"
			}
			r.log.Error("plugin.init.failed", err, map[string]interface{}{
				"plugin": name,
				"cause":  cause,
			})
			continue
		}
		r.cliRegistry.Merge(stagingCLI)
		live.State = StateActive
		summary.Initialized++
		r.log.Info("plugin.init.complete", map[string]interface{}{
			"plugin": name,
		})
	}
	app.cliRegistry = r.cliRegistry
	app.WarnUnownedPluginOnlyFields()
	app.setCurrentPluginName("")
	app.FreezePluginNeeds()
	return summary
}

func (r *Registry) entryByName(name string) *Entry {
	for i := range r.entries {
		if r.entries[i].Name == name {
			return &r.entries[i]
		}
	}
	return nil
}

// panicError wraps a recovered panic value.
type panicError struct {
	value interface{}
}

func (e *panicError) Error() string {
	return fmt.Sprintf("panic: %v", e.value)
}

// safeInit calls plugin.Init under the boot context and recovers from panics.
// Plugins observe cancellation via App.Context(); a blocked Init that ignores
// that context still runs until it returns (between-plugin checks cover the rest).
func (r *Registry) safeInit(ctx context.Context, e *Entry, app *App) (err error) {
	defer func() {
		if rv := recover(); rv != nil {
			err = &panicError{value: rv}
		}
		app.setInitContext(nil)
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	app.setInitContext(ctx)
	return e.Plugin.Init(app)
}

// Entries returns a copy of all plugin entries.
func (r *Registry) Entries() []Entry {
	cp := make([]Entry, len(r.entries))
	copy(cp, r.entries)
	return cp
}

// Len returns the number of registered plugins.
func (r *Registry) Len() int {
	return len(r.entries)
}
