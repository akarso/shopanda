package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func bootstrapPluginCLIRegistry(cfg *config.Config, log logger.Logger) *plugin.Registry {
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	app := &plugin.App{
		Logger: log,
		Config: cfg,
	}
	preparePermissionRegistry(app)
	registry.InitAll(app)
	freezePermissionRegistry(app) // CLI: freeze only (no BindRuntime; avoid multi-App collisions)
	return registry
}

func runPluginCLICommand(cfg *config.Config, log logger.Logger, registryFn func() *plugin.Registry, name string, args []string) (bool, error) {
	registry := registryFn()
	cmdRegistry := registry.CLIRegistry()
	if cmdRegistry == nil {
		return false, nil
	}
	cmd, ok := cmdRegistry.Get(name)
	if !ok {
		return false, nil
	}

	ctx := cli.Context{
		Ctx:    context.Background(),
		Config: cfg,
		Logger: log,
	}
	if cmd.RequiresDB {
		dsn := config.DatabaseDSN(cfg)
		conn, err := db.Open(dsn)
		if err != nil {
			return true, fmt.Errorf("database: %w", err)
		}
		defer conn.Close()
		ctx.DB = conn
	}
	return true, cmdRegistry.Run(name, ctx, args)
}

func pluginCLIHelpLines(registryFn func() *plugin.Registry) []string {
	cmds := registryFn().CLIRegistry().List()
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

func appendPluginCLIHelp(registryFn func() *plugin.Registry, base string) string {
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
