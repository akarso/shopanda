package main

import (
	exportctxApp "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// bootstrapExportRegistry loads plugins and returns the shared export row hook registry.
func bootstrapExportRegistry(cfg *config.Config, log logger.Logger) *exportctxApp.Registry {
	exportRegistry := exportctxApp.NewRegistry(log)
	pluginRegistry := plugin.NewRegistry(log)
	registerPlugins(pluginRegistry, cfg)
	app := &plugin.App{
		Logger: log,
		Config: cfg,
	}
	app.SetExportRegistry(exportRegistry)
	preparePermissionRegistry(app)
	summary := pluginRegistry.InitAll(app)
	freezePermissionRegistry(app) // export CLI: freeze only (no BindRuntime)
	if summary.Failed > 0 {
		log.Warn("export.plugins.init", map[string]interface{}{
			"registered":  summary.Registered,
			"initialized": summary.Initialized,
			"failed":      summary.Failed,
		})
	}
	return exportRegistry
}
