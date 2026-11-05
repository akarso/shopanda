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

func bootstrapPluginCLIRegistry(cfg *config.Config, log logger.Logger) (*plugin.Registry, error) {
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
	return registry, nil
}

func runPluginCLICommand(cfg *config.Config, log logger.Logger, registryFn func() (*plugin.Registry, error), name string, args []string) (bool, error) {
	registry, err := registryFn()
	if err != nil {
		return false, err
	}
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

func pluginCLIHelpLines(registryFn func() (*plugin.Registry, error)) []string {
	registry, err := registryFn()
	if err != nil || registry == nil || registry.CLIRegistry() == nil {
		return nil
	}
	cmds := registry.CLIRegistry().List()
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

func appendPluginCLIHelp(registryFn func() (*plugin.Registry, error), base string) string {
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
