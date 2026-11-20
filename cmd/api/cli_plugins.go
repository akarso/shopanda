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

// pluginCLIDeps opens the DB and binds ports for RequiresDB commands.
// Tests pass a stub deps into runPluginCLICommandWithDeps (no package globals).
type pluginCLIDeps struct {
	OpenDB func(*config.Config) (*sql.DB, error)
	Bind   func(*plugin.App, *sql.DB, cli.Command) error
}

func defaultPluginCLIDeps() pluginCLIDeps {
	return pluginCLIDeps{
		OpenDB: func(cfg *config.Config) (*sql.DB, error) {
			return db.Open(config.DatabaseDSN(cfg))
		},
		Bind: bindPluginCLIPorts,
	}
}

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
	return runPluginCLICommandWithDeps(cfg, log, registryFn, name, args, defaultPluginCLIDeps())
}

func runPluginCLICommandWithDeps(cfg *config.Config, log logger.Logger, registryFn func() (*pluginCLIRuntime, error), name string, args []string, deps pluginCLIDeps) (bool, error) {
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

	if err := validateCLICommandNeeds(cmd.Needs); err != nil {
		return true, fmt.Errorf("plugin CLI: command %q: %w", name, err)
	}
	// Per-command CapCatalogSeed (CLICommand.Needs) requires RequiresDB so the
	// root can bind CatalogSeedPort. Plugin-level Need(CapCatalogSeed) alone
	// must not block sibling RequiresDB:false commands (e.g. metaldogs:eval).
	if cliCommandNeedsCapability(cmd, plugin.CapCatalogSeed) && !cmd.RequiresDB {
		return true, fmt.Errorf("plugin CLI: command %q Needs CapCatalogSeed but RequiresDB is false", name)
	}

	ctx := cli.Context{
		Ctx:    context.Background(),
		Config: cfg,
		Logger: log,
	}
	if cmd.RequiresDB {
		// Any RequiresDB opens DB and binds product read ports. CapCatalogSeed
		// bind is owner-scoped (see bindPluginCLIPortsFromBoot).
		if cmd.Plugin == "" && rt.App != nil &&
			(cliCommandNeedsCapability(cmd, plugin.CapCatalogSeed) || rt.App.CapabilityNeeded(plugin.CapCatalogSeed)) {
			return true, fmt.Errorf("plugin CLI: command %q RequiresDB but has empty Plugin owner while CapCatalogSeed is Needed", name)
		}
		if deps.OpenDB == nil || deps.Bind == nil {
			return true, fmt.Errorf("plugin CLI: open/bind deps not configured")
		}
		conn, err := deps.OpenDB(cfg)
		if err != nil {
			return true, fmt.Errorf("database: %w", err)
		}
		defer conn.Close()
		if err := deps.Bind(rt.App, conn, cmd); err != nil {
			return true, err
		}
	}
	return true, cmdRegistry.Run(name, ctx, args)
}

func cliCommandNeedsCapability(cmd cli.Command, cap plugin.Capability) bool {
	want := string(cap)
	for _, n := range cmd.Needs {
		if n == want {
			return true
		}
	}
	return false
}

// validateCLICommandNeeds rejects unknown command Needs. Only CapCatalogSeed
// is supported on CLICommand.Needs / cli.Command.Needs (not a general Needs API).
func validateCLICommandNeeds(needs []string) error {
	for _, n := range needs {
		if n == "" {
			continue
		}
		if n != string(plugin.CapCatalogSeed) {
			return fmt.Errorf("unsupported command Need %q (only CapCatalogSeed is supported)", n)
		}
	}
	return nil
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
	if err := validateCLICommandNeeds(cmd.Needs); err != nil {
		return err
	}
	app.Bootstrap = boot
	if err := bindProductPorts(app, boot); err != nil {
		return err
	}

	// CapCatalogSeed bind requires the owning plugin to have declared the cap
	// (Host.CatalogSeed gates on CapabilityDeclaredBy). Then:
	// - command Needs includes CapCatalogSeed → bind
	// - command Needs empty (compat) → bind for every RequiresDB of that plugin
	// - command Needs non-empty without CapCatalogSeed → do not bind
	bindSeed := false
	if cmd.Plugin != "" && app.CapabilityDeclaredBy(plugin.CapCatalogSeed, cmd.Plugin) {
		if cliCommandNeedsCapability(cmd, plugin.CapCatalogSeed) {
			bindSeed = true
		} else if len(cmd.Needs) == 0 {
			bindSeed = true
		}
	}
	if bindSeed {
		if err := bindCatalogSeedPort(app, boot); err != nil {
			return err
		}
	}
	// Always skip CapCart + CapIntegration* on plugin CLI; CapCatalogSeed only
	// when this command did not bind it (peer Needs must not force the port).
	return assertPluginCLINeedsMet(app, bindSeed)
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
