package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func initPluginCLIRegistry(cfg *config.Config, log logger.Logger) *plugin.Registry {
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	app := &plugin.App{
		Logger: log,
		Config: cfg,
	}
	registry.InitAll(app)
	return registry
}

func runPluginCLICommand(cfg *config.Config, log logger.Logger, name string, args []string) (bool, error) {
	registry := initPluginCLIRegistry(cfg, log)
	cmdRegistry := registry.CLIRegistry()
	if cmdRegistry == nil {
		return false, nil
	}
	if _, ok := cmdRegistry.Get(name); !ok {
		return false, nil
	}

	dsn := config.DatabaseDSN(cfg)
	conn, err := db.Open(dsn)
	if err != nil {
		return true, fmt.Errorf("database: %w", err)
	}
	defer conn.Close()

	ctx := cli.Context{
		Ctx:    context.Background(),
		Config: cfg,
		Logger: log,
		DB:     conn,
	}
	return true, cmdRegistry.Run(name, ctx, args)
}

func pluginCLIHelpLines(cfg *config.Config, log logger.Logger) []string {
	registry := initPluginCLIRegistry(cfg, log)
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

func appendPluginCLIHelp(cfg *config.Config, log logger.Logger, base string) string {
	lines := pluginCLIHelpLines(cfg, log)
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
