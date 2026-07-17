package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	importctxapp "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/application/pluginreport"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func bootstrapPluginReport(cfg *config.Config, log logger.Logger) (*plugin.Registry, *plugin.App) {
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	app.SetHookRegistry(hooksapp.NewRegistry(log))
	app.SetImportRegistry(importctxapp.NewRegistry(log))
	app.SetExtensionRegistry(extensionApp.NewRegistry())
	registry.InitAll(app)
	return registry, app
}

func runPluginsReport(w io.Writer, cfg *config.Config, log logger.Logger, args []string) error {
	if w == nil {
		w = os.Stdout
	}
	jsonOut := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOut = true
		case "--help", "-h":
			fmt.Print(`Usage: app plugins report [--json]

Print registered plugin extension points, infrastructure ports, and routes.
Uses the same compile-time plugin set as the running application (no database required).
`)
			return nil
		default:
			return fmt.Errorf("plugins report: unknown flag %q", arg)
		}
	}

	registry, app := bootstrapPluginReport(cfg, log)
	report := pluginreport.Build(registry, app, cfg)
	if jsonOut {
		data, err := pluginreport.FormatJSON(report)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		if err == nil && !strings.HasSuffix(string(data), "\n") {
			_, err = w.Write([]byte("\n"))
		}
		return err
	}
	_, err := fmt.Fprint(w, pluginreport.FormatText(report))
	return err
}
