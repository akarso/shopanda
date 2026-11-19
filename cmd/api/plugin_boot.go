package main

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Process-kind AssertNeedsMetExcept lists — single source of truth for serve,
// worker / scheduler / search reindex / non-seed CLIs, and plugin CLI.
// Production composition roots call assert* after their own bind+Init.
// Tests call bootServePlugins / bootWorkerPlugins to lock the same skip lists
// (thinner than full wireServeRuntime / runWorker — skip-list coverage, not a
// substitute for production Init wiring).

func serveNeedsSkip() []plugin.Capability {
	// CapCart is bound on serve; CapCatalogSeed is CLI-seed-only.
	return []plugin.Capability{plugin.CapCatalogSeed}
}

func workerNeedsSkip() []plugin.Capability {
	// CapCart is serve-only; CapCatalogSeed is CLI-seed-only.
	// Use after CapJobs / CapMedia are bound (worker, scheduler, search-reindex,
	// jobs CLI, schedule CLI). CapJobs needs a Reader-backed queue; CapMedia
	// needs BindMediaStorage.
	return []plugin.Capability{plugin.CapCart, plugin.CapCatalogSeed}
}

func workerLiteNeedsSkip() []plugin.Capability {
	// Same as workerNeedsSkip plus CapJobs / CapMedia for short-lived CLIs that
	// never resolveAndBindJobQueue / resolveAndBindMediaStorage (cache,
	// extension-field, discovery facet sync).
	return []plugin.Capability{
		plugin.CapCart,
		plugin.CapCatalogSeed,
		plugin.CapJobs,
		plugin.CapMedia,
	}
}

func assertServeNeedsMet(app *plugin.App) error {
	if err := app.AssertNeedsMetExcept(serveNeedsSkip()...); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}

// assertWorkerNeedsMet checks Needs after CapJobs / CapMedia bind (bind then
// assert — same order as serve's BindCartPort then assertServeNeedsMet).
func assertWorkerNeedsMet(app *plugin.App) error {
	if err := app.AssertNeedsMetExcept(workerNeedsSkip()...); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}

// assertWorkerLiteNeedsMet is for CLIs that Init plugins but do not bind
// CapJobs / CapMedia (cache / extension-field / discovery).
func assertWorkerLiteNeedsMet(app *plugin.App) error {
	if err := app.AssertNeedsMetExcept(workerLiteNeedsSkip()...); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}

// seedCLINeedsSkip is CapCart + CapIntegration* (not wired on plugin CLI).
// CapCatalogSeed must already be bound for seed commands.
func seedCLINeedsSkip() []plugin.Capability {
	return []plugin.Capability{
		plugin.CapCart,
		plugin.CapIntegrationOrder,
		plugin.CapIntegrationShipment,
		plugin.CapIntegrationStock,
	}
}

func assertSeedCLINeedsMet(app *plugin.App) error {
	return assertPluginCLINeedsMet(app, true)
}

// pluginCLINeedsSkip is CapCart + CapIntegration* always; CapCatalogSeed only
// when the owning command did not bind CatalogSeedPort.
func pluginCLINeedsSkip(bindSeed bool) []plugin.Capability {
	skip := seedCLINeedsSkip()
	if !bindSeed {
		skip = append(skip, plugin.CapCatalogSeed)
	}
	return skip
}

func assertPluginCLINeedsMet(app *plugin.App, bindSeed bool) error {
	if err := app.AssertNeedsMetExcept(pluginCLINeedsSkip(bindSeed)...); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}

// bootServePlugins binds product ports, InitAll, late-binds cartPort, then
// assertServeNeedsMet. Test helper to lock serveNeedsSkip — not the full
// wireServeRuntime path (no integration writers).
// cartPort may be any non-nil value accepted by BindCartPort (e.g. struct{}{}).
func bootServePlugins(reg *plugin.Registry, app *plugin.App, boot *plugin.Bootstrap, cartPort any) error {
	if reg == nil || app == nil {
		return fmt.Errorf("bootServePlugins: registry and app are required")
	}
	if err := bindProductPorts(app, boot); err != nil {
		return err
	}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	if err := plugin.InitSummaryError(summary); err != nil {
		return err
	}
	if cartPort == nil {
		return fmt.Errorf("bootServePlugins: cartPort is required (serve binds CapCart)")
	}
	if err := app.BindCartPort(cartPort); err != nil {
		return err
	}
	if err := assertServeNeedsMet(app); err != nil {
		return err
	}
	freezePermissionRegistry(app)
	return nil
}

// bootWorkerPlugins binds product ports, InitAll, then assertWorkerLiteNeedsMet.
// Skip-list coverage only — does not bind CapJobs / CapMedia (production worker
// binds those before assertWorkerNeedsMet).
func bootWorkerPlugins(reg *plugin.Registry, app *plugin.App, boot *plugin.Bootstrap) error {
	if reg == nil || app == nil {
		return fmt.Errorf("bootWorkerPlugins: registry and app are required")
	}
	if err := bindProductPorts(app, boot); err != nil {
		return err
	}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	if err := plugin.InitSummaryError(summary); err != nil {
		return err
	}
	if err := assertWorkerLiteNeedsMet(app); err != nil {
		return err
	}
	freezePermissionRegistry(app)
	return nil
}
