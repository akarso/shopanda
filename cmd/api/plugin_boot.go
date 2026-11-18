package main

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Process-kind AssertNeedsMetExcept lists — single source of truth for serve,
// worker, scheduler, search reindex, and non-seed CLIs. Products and tests
// call bootServePlugins / bootWorkerPlugins / assertSeedCLINeedsMet instead of
// copying skip lists from main.go.

func serveNeedsSkip() []plugin.Capability {
	// CapCart is bound on serve; CapCatalogSeed is CLI-seed-only.
	return []plugin.Capability{plugin.CapCatalogSeed}
}

func workerNeedsSkip() []plugin.Capability {
	// CapCart is serve-only; CapCatalogSeed is CLI-seed-only.
	return []plugin.Capability{plugin.CapCart, plugin.CapCatalogSeed}
}

// schedulerNeedsSkip matches worker (no CartPort, no CatalogSeedPort).
func schedulerNeedsSkip() []plugin.Capability { return workerNeedsSkip() }

func assertServeNeedsMet(app *plugin.App) error {
	if err := app.AssertNeedsMetExcept(serveNeedsSkip()...); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}

func assertWorkerNeedsMet(app *plugin.App) error {
	if err := app.AssertNeedsMetExcept(workerNeedsSkip()...); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}

func assertSchedulerNeedsMet(app *plugin.App) error {
	if err := app.AssertNeedsMetExcept(schedulerNeedsSkip()...); err != nil {
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
// assertServeNeedsMet. Callable from tests without wireServeRuntime.
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
	freezePermissionRegistry(app)
	if err := plugin.InitSummaryError(summary); err != nil {
		return err
	}
	if cartPort == nil {
		return fmt.Errorf("bootServePlugins: cartPort is required (serve binds CapCart)")
	}
	if err := app.BindCartPort(cartPort); err != nil {
		return err
	}
	return assertServeNeedsMet(app)
}

// bootWorkerPlugins binds product ports, InitAll, then assertWorkerNeedsMet.
// Same skip list as scheduler / search reindex / non-seed CLIs.
func bootWorkerPlugins(reg *plugin.Registry, app *plugin.App, boot *plugin.Bootstrap) error {
	if reg == nil || app == nil {
		return fmt.Errorf("bootWorkerPlugins: registry and app are required")
	}
	if err := bindProductPorts(app, boot); err != nil {
		return err
	}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	freezePermissionRegistry(app)
	if err := plugin.InitSummaryError(summary); err != nil {
		return err
	}
	return assertWorkerNeedsMet(app)
}

// bootSchedulerPlugins is the scheduler process kind (same skips as worker).
func bootSchedulerPlugins(reg *plugin.Registry, app *plugin.App, boot *plugin.Bootstrap) error {
	if reg == nil || app == nil {
		return fmt.Errorf("bootSchedulerPlugins: registry and app are required")
	}
	if err := bindProductPorts(app, boot); err != nil {
		return err
	}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	freezePermissionRegistry(app)
	if err := plugin.InitSummaryError(summary); err != nil {
		return err
	}
	return assertSchedulerNeedsMet(app)
}
