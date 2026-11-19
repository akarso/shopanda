package harness

import (
	"database/sql"
	"fmt"
	"io"

	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Options configures minimal composition-root wiring for AdaptInit.
type Options struct {
	// CartPort binds when CapCart is satisfied; omit for MarkCapabilitySatisfied only.
	CartPort pluginsdk.CartPort
	// Satisfy lists capabilities to mark or bind before Init (typically CapCart).
	Satisfy []pluginsdk.Capability
	// DB sets App.Bootstrap.DB so Host.BootstrapDB() works when CapDB is
	// declared (e.g. from OpenIsolated). Nil leaves Bootstrap unset.
	DB *sql.DB
}

// Result is the public surface from AdaptInit. It deliberately does not expose
// *plugin.App so product plugin tests cannot reach core types through pkg/.
type Result struct {
	Host pluginsdk.Host
}

// AdaptInit boots pluginsdk.Adapt(p) against a minimal in-memory composition root
// and returns a Host scoped to the plugin name.
func AdaptInit(p pluginsdk.Plugin, opts Options) (Result, error) {
	if p == nil {
		return Result{}, fmt.Errorf("pluginsdk/harness: plugin is nil")
	}
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
	}
	if opts.DB != nil {
		app.Bootstrap = &plugin.Bootstrap{DB: opts.DB}
	}
	for _, cap := range opts.Satisfy {
		switch cap {
		case pluginsdk.CapCart:
			if opts.CartPort != nil {
				if err := app.BindCartPort(opts.CartPort); err != nil {
					return Result{}, err
				}
			} else {
				app.MarkCapabilitySatisfied(cap)
			}
		case pluginsdk.CapCatalog, pluginsdk.CapCatalogSeed, pluginsdk.CapOrders, pluginsdk.CapDB:
			app.MarkCapabilitySatisfied(cap)
		default:
			app.MarkCapabilitySatisfied(cap)
		}
	}
	bridged := pluginsdk.Adapt(p)
	if err := bridged.Init(app); err != nil {
		return Result{}, fmt.Errorf("pluginsdk/harness: Init: %w", err)
	}
	if err := app.AssertNeedsMet(); err != nil {
		return Result{}, err
	}
	return Result{Host: pluginsdk.AsHostFor(app, p.Name())}, nil
}
