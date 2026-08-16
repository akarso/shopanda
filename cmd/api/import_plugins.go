package main

import (
	importctxApp "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// bootstrapImportRegistry loads plugins and returns the shared import row hook registry.
func bootstrapImportRegistry(cfg *config.Config, log logger.Logger) *importctxApp.Registry {
	importRegistry := importctxApp.NewRegistry(log)
	pluginRegistry := plugin.NewRegistry(log)
	registerPlugins(pluginRegistry, cfg)
	app := &plugin.App{
		Logger: log,
		Config: cfg,
	}
	app.SetImportRegistry(importRegistry)
	preparePermissionRegistry(app)
	summary := pluginRegistry.InitAll(app)
	freezePermissionRegistry(app) // import CLI: freeze only (no BindRuntime)
	if summary.Failed > 0 {
		log.Warn("import.plugins.init", map[string]interface{}{
			"registered":  summary.Registered,
			"initialized": summary.Initialized,
			"failed":      summary.Failed,
		})
	}
	return importRegistry
}
