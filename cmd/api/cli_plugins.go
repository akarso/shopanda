package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// pluginCLIRuntime keeps the Init App so RequiresDB commands can bind ports
// (including CapCatalogSeed) on the same App Host closures captured at Init.
type pluginCLIRuntime struct {
	Registry *plugin.Registry
	App      *plugin.App
}

// Injectable for tests (open DB / bind ports without a live Postgres).
var (
	pluginCLIOpenDB = func(cfg *config.Config) (*sql.DB, error) {
		return db.Open(config.DatabaseDSN(cfg))
	}
	pluginCLIBindPorts = bindPluginCLIPorts
)

func bootstrapPluginCLIRegistry(cfg *config.Config, log logger.Logger) (*pluginCLIRuntime, error) {
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	app := &plugin.App{
		Logger: log,
		Config: cfg,
	}
	preparePermissionRegistry(app)
	summary := registry.InitAll(app)
	freezePermissionRegistry(app) // CLI: freeze only (no BindRuntime; avoid multi-App collisions)
	if err := plugin.InitSummaryError(summary); err != nil {
		return nil, err
	}
	return &pluginCLIRuntime{Registry: registry, App: app}, nil
}

func runPluginCLICommand(cfg *config.Config, log logger.Logger, registryFn func() (*pluginCLIRuntime, error), name string, args []string) (bool, error) {
	rt, err := registryFn()
	if err != nil {
		return false, err
	}
	if rt == nil || rt.Registry == nil {
		return false, nil
	}
	cmdRegistry := rt.Registry.CLIRegistry()
	if cmdRegistry == nil {
		return false, nil
	}
	cmd, ok := cmdRegistry.Get(name)
	if !ok {
		return false, nil
	}

	// Seed plugins that Need CapCatalogSeed must use RequiresDB so the root
	// can bind CatalogSeedPort before Run.
	if cmd.Plugin != "" && rt.App != nil &&
		rt.App.CapabilityDeclaredBy(plugin.CapCatalogSeed, cmd.Plugin) && !cmd.RequiresDB {
		return true, fmt.Errorf("plugin CLI: command %q (plugin %q) Needs CapCatalogSeed but RequiresDB is false", name, cmd.Plugin)
	}

	ctx := cli.Context{
		Ctx:    context.Background(),
		Config: cfg,
		Logger: log,
	}
	if cmd.RequiresDB {
		conn, err := pluginCLIOpenDB(cfg)
		if err != nil {
			return true, fmt.Errorf("database: %w", err)
		}
		defer conn.Close()
		if err := pluginCLIBindPorts(rt.App, conn, cmd); err != nil {
			return true, err
		}
	}
	return true, cmdRegistry.Run(name, ctx, args)
}

// bindPluginCLIPorts wires product ports on the Init App after DB open.
// CapCatalogSeed is bound only when the running command's owning plugin
// declared that Need — not for every RequiresDB command when a peer needs seed.
func bindPluginCLIPorts(app *plugin.App, conn *sql.DB, cmd cli.Command) error {
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		return err
	}
	return bindPluginCLIPortsFromBoot(app, boot, cmd)
}

func bindPluginCLIPortsFromBoot(app *plugin.App, boot *plugin.Bootstrap, cmd cli.Command) error {
	if app == nil {
		return fmt.Errorf("plugin CLI: app is nil")
	}
	if boot == nil {
		return fmt.Errorf("plugin CLI: bootstrap is required")
	}
	app.Bootstrap = boot
	if err := bindProductPorts(app, boot); err != nil {
		return err
	}

	bindSeed := cmd.Plugin != "" && app.CapabilityDeclaredBy(plugin.CapCatalogSeed, cmd.Plugin)
	if bindSeed {
		if err := bindCatalogSeedPort(app, boot); err != nil {
			return err
		}
	}

	// Plugin CLI does not wire CapCart (serve-only) or CapIntegration*
	// (jobs/serve wire those before Init). Failed Init of warehousedemo still
	// leaves CapIntegration* in required — skip them here like CapCart.
	skip := []plugin.Capability{
		plugin.CapCart,
		plugin.CapIntegrationOrder,
		plugin.CapIntegrationShipment,
		plugin.CapIntegrationStock,
	}
	if !bindSeed {
		skip = append(skip, plugin.CapCatalogSeed)
	}
	if err := app.AssertNeedsMetExcept(skip...); err != nil {
		return fmt.Errorf("plugin capabilities: %w", err)
	}
	return nil
}

func pluginCLIHelpLines(registryFn func() (*pluginCLIRuntime, error)) []string {
	rt, err := registryFn()
	if err != nil || rt == nil || rt.Registry == nil || rt.Registry.CLIRegistry() == nil {
		return nil
	}
	cmds := rt.Registry.CLIRegistry().List()
	if len(cmds) == 0 {
		return nil
	}
	lines := make([]string, 0, len(cmds)+1)
	lines = append(lines, "Plugin commands:")
	for _, cmd := range cmds {
		lines = append(lines, fmt.Sprintf("  %-20s %s", cmd.Name, cmd.Description))
	}
	return lines
}

func appendPluginCLIHelp(registryFn func() (*pluginCLIRuntime, error), base string) string {
	lines := pluginCLIHelpLines(registryFn)
	if len(lines) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	if !strings.HasSuffix(base, "\n") {
		b.WriteByte('\n')
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
